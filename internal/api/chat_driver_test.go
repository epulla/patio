package api

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClaudeStreamFixture(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	events := []map[string]any{}
	manager := newChatManager(database, Options{Emit: func(_ string, event any) { events = append(events, event.(map[string]any)) }})
	runtime := &chatRuntime{}
	data, err := os.ReadFile(filepath.Join("testdata", "claude-stream.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		if err := manager.parseLine(chatID, runtime, scanner.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	var messages []ChatMessage
	rows, err := database.Query(`SELECT id,chat_id,role,content,created_at FROM chat_message WHERE chat_id=? ORDER BY rowid`, chatID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var message ChatMessage
		if err := rows.Scan(&message.ID, &message.ChatID, &message.Role, &message.Content, &message.CreatedAt); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 || messages[0].Role != "tool" || messages[0].Content != `Write {"file_path":"hello.txt","content":"hi"}` || messages[1].Role != "assistant" || messages[1].Content != "Hello" || messages[2].Role != "error" || messages[2].Content != "denied" {
		t.Fatalf("messages = %#v", messages)
	}
	var session string
	if err := database.QueryRow(`SELECT session_id FROM chat WHERE id=?`, chatID).Scan(&session); err != nil || session != "session-fixture" {
		t.Fatalf("session = %q, %v", session, err)
	}
	var quotaJSON string
	if err := database.QueryRow(`SELECT quota_json FROM connector WHERE id=?`, connectorID).Scan(&quotaJSON); err != nil {
		t.Fatal(err)
	}
	var quota struct {
		Kind    string `json:"kind"`
		Windows []struct {
			Label       string   `json:"label"`
			UsedPercent *float64 `json:"used_percent"`
			ResetsAt    *string  `json:"resets_at"`
		} `json:"windows"`
	}
	if err := json.Unmarshal([]byte(quotaJSON), &quota); err != nil || quota.Kind != "claude" || len(quota.Windows) != 2 || quota.Windows[0].Label != "5h" || quota.Windows[0].UsedPercent == nil || math.Abs(*quota.Windows[0].UsedPercent-14) > 0.001 || quota.Windows[0].ResetsAt == nil || *quota.Windows[0].ResetsAt != "2026-10-09T17:50:00Z" || quota.Windows[1].Label != "weekly" || quota.Windows[1].UsedPercent == nil || *quota.Windows[1].UsedPercent != 9 || quota.Windows[1].ResetsAt == nil || *quota.Windows[1].ResetsAt != "2026-10-10T19:00:00Z" {
		t.Fatalf("quota = %s, %v", quotaJSON, err)
	}
	if len(events) != 5 || events[0]["type"] != "delta" || events[1]["type"] != "delta" || events[2]["type"] != "tool" || events[3]["type"] != "done" || events[4]["type"] != "done" {
		t.Fatalf("events = %#v", events)
	}
}

func TestBuildClaudeArgs(t *testing.T) {
	args, err := buildClaudeArgs("/app/mt", "/data/board.db", "chat-id", "sonnet", "session-id")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-p", "--append-system-prompt", "/tickets/<ticket id>", "--input-format stream-json", "--output-format stream-json", "--verbose", "--include-partial-messages", "--model sonnet", "--strict-mcp-config", "--permission-prompt-tool mcp__my-tickets__approve", "--resume session-id"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Claude args %q missing %q", joined, want)
		}
	}
	promptIndex := slices.Index(args, "--append-system-prompt")
	if promptIndex < 0 || promptIndex+1 == len(args) || !strings.Contains(args[promptIndex+1], "/tickets/<ticket id>") {
		t.Fatalf("system prompt args = %v", args)
	}
	index := slices.Index(args, "--mcp-config")
	if index < 0 || index+1 == len(args) {
		t.Fatalf("missing MCP config args: %v", args)
	}
	var config struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(args[index+1]), &config); err != nil {
		t.Fatal(err)
	}
	server := config.Servers["my-tickets"]
	if server.Command != "/app/mt" || !reflect.DeepEqual(server.Args, []string{"mcp", "-d", "/data/board.db", "-approve-chat", "chat-id"}) {
		t.Fatalf("MCP server config = %#v", server)
	}
}

func TestChatSendRunningConflictAndStop(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	fake := newPipeChatProcess()
	server := New(database, "local", false, "dev", root, Options{
		DBPath:         filepath.Join(root, "test.db"),
		findClaude:     func() (string, error) { return "claude-test", nil },
		commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil },
		processFactory: func(path string, args, env []string, cwd string) (chatProcess, error) {
			if path != "claude-test" || len(args) != 1 || cwd != filepath.Join(root, "chats", chatID) {
				t.Errorf("process launch = %q %#v %q", path, args, cwd)
			}
			return fake, nil
		},
	})
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "hello"}, nil); code != http.StatusAccepted {
		t.Fatalf("send status = %d", code)
	}
	var line string
	select {
	case line = <-fake.input:
	case <-time.After(time.Second):
		t.Fatal("process did not receive user input")
	}
	var input struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		ParentToolUseID any `json:"parent_tool_use_id"`
	}
	if err := json.Unmarshal([]byte(line), &input); err != nil || input.Type != "user" || input.Message.Role != "user" || input.Message.Content != "hello" || input.ParentToolUseID != nil {
		t.Fatalf("stdin line = %s, %v", line, err)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "again"}, nil); code != http.StatusConflict {
		t.Fatalf("concurrent send status = %d", code)
	}
	var state struct {
		Chat struct {
			Running bool `json:"running"`
		} `json:"chat"`
	}
	if code := requestJSON(t, server, http.MethodGet, "/api/chats/"+chatID, nil, &state); code != http.StatusOK || !state.Chat.Running {
		t.Fatalf("running status/data = %d %#v", code, state)
	}
	if _, err := io.WriteString(fake.stdoutW, `{"type":"result","is_error":false,"result":"OK"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "stop me"}, nil); code != http.StatusAccepted {
		t.Fatalf("second turn status = %d", code)
	}
	select {
	case <-fake.input:
	case <-time.After(time.Second):
		t.Fatal("second input not received")
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/stop", nil, nil); code != http.StatusNoContent {
		t.Fatalf("stop status = %d", code)
	}
	if server.chats.Running(chatID) {
		t.Fatal("chat remains running after stop")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestChatSendRejectsNonClaudeConnector(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'codex','Codex',?)`, connectorID, now()); err != nil {
		t.Fatal(err)
	}
	chatID := createDriverTestChat(t, database, connectorID, root)
	server := New(database, "local", false, "dev", root)
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "hello"}, nil); code != http.StatusBadRequest {
		t.Fatalf("non-Claude send status = %d", code)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=?`, chatID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("messages after unsupported send = %d, %v", count, err)
	}
	if _, err := database.Exec(`UPDATE chat SET connector_id=NULL WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "hello"}, nil); code != http.StatusBadRequest {
		t.Fatalf("missing connector send status = %d", code)
	}
}

func createDriverTestChat(t *testing.T, database *sql.DB, connectorID, root string) string {
	t.Helper()
	id := uid()
	if _, err := database.Exec(`INSERT INTO chat(id,connector_id,title,model,created_at,updated_at) VALUES(?,?, 'New chat','',?,?)`, id, connectorID, now(), now()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "chats", id), 0o700); err != nil {
		t.Fatal(err)
	}
	return id
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met")
}

type pipeChatProcess struct {
	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	stderrR *io.PipeReader
	stderrW *io.PipeWriter
	input   chan string
	done    chan struct{}
	once    sync.Once
}

func newPipeChatProcess() *pipeChatProcess {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	p := &pipeChatProcess{stdinR: stdinR, stdinW: stdinW, stdoutR: stdoutR, stdoutW: stdoutW, stderrR: stderrR, stderrW: stderrW, input: make(chan string, 4), done: make(chan struct{})}
	go func() {
		scanner := bufio.NewScanner(stdinR)
		for scanner.Scan() {
			p.input <- scanner.Text()
		}
	}()
	return p
}

func (p *pipeChatProcess) Stdin() io.WriteCloser { return p.stdinW }
func (p *pipeChatProcess) Stdout() io.ReadCloser { return p.stdoutR }
func (p *pipeChatProcess) Stderr() io.ReadCloser { return p.stderrR }
func (p *pipeChatProcess) Interrupt() error      { return p.finish() }
func (p *pipeChatProcess) Kill() error           { return p.finish() }
func (p *pipeChatProcess) Wait() error           { <-p.done; return nil }
func (p *pipeChatProcess) finish() error {
	p.once.Do(func() {
		_ = p.stdoutW.Close()
		_ = p.stderrW.Close()
		_ = p.stdinW.Close()
		_ = p.stdinR.Close()
		close(p.done)
	})
	return nil
}
