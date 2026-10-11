package api

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	runtime := &claudeRuntime{manager: manager, chatID: chatID, runtime: manager.runtime(chatID)}
	runtime.runtime.connectorID = connectorID
	data, err := os.ReadFile(filepath.Join("testdata", "claude-stream.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		if err := runtime.parseLine(scanner.Bytes()); err != nil {
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

func TestRequestApproval(t *testing.T) {
	for _, test := range []struct {
		name      string
		allow     bool
		answer    bool
		cancel    bool
		wantState string
	}{
		{name: "allow", allow: true, answer: true, wantState: "allowed"},
		{name: "deny", answer: true, wantState: "denied"},
		{name: "context cancel", cancel: true, wantState: "expired"},
		{name: "timeout", wantState: "expired"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, _, root := chatAPI(t)
			connectorID := createTestConnector(t, database)
			chatID := createDriverTestChat(t, database, connectorID, root)
			events := make(chan any, 1)
			server := New(database, "local", false, "dev", root, Options{Emit: func(name string, event any) {
				if name == "chat:"+chatID {
					events <- event
				}
			}})
			server.chats.approvalPollInterval = time.Millisecond
			server.chats.approvalTimeout = 30 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan struct {
				allowed bool
				err     error
			}, 1)
			go func() {
				allowed, err := server.chats.requestApproval(ctx, chatID, "write_file", json.RawMessage(`{"path":"x"}`))
				result <- struct {
					allowed bool
					err     error
				}{allowed, err}
			}()
			var approvalID string
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&approvalID)
				if err == nil {
					break
				}
				if !errors.Is(err, sql.ErrNoRows) {
					t.Fatal(err)
				}
				time.Sleep(time.Millisecond)
			}
			if approvalID == "" {
				t.Fatal("approval row was not inserted")
			}
			select {
			case event := <-events:
				if event.(map[string]any)["type"] != "approval" {
					t.Fatalf("approval event = %#v", event)
				}
			case <-time.After(time.Second):
				t.Fatal("approval event was not emitted")
			}
			if test.cancel {
				cancel()
			}
			if test.answer {
				if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": test.allow}, nil); code != http.StatusOK {
					t.Fatalf("answer status = %d", code)
				}
			}
			select {
			case got := <-result:
				if got.err != nil || got.allowed != test.allow {
					t.Fatalf("requestApproval = (%v, %v), want (%v, nil)", got.allowed, got.err, test.allow)
				}
			case <-time.After(time.Second):
				t.Fatal("approval waiter did not return")
			}
			var status string
			if err := database.QueryRow(`SELECT status FROM chat_approval WHERE id=?`, approvalID).Scan(&status); err != nil || status != test.wantState {
				t.Fatalf("approval status = %q, %v; want %q", status, err, test.wantState)
			}
			if err := server.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRequestApprovalAutoAllowed(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET auto_allow=1 WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	events := make(chan any, 1)
	server := New(database, "local", false, "dev", root, Options{Emit: func(_ string, event any) { events <- event }})
	defer server.Close()
	server.chats.approvalTimeout = time.Second
	allowed, err := server.chats.requestApproval(context.Background(), chatID, "write_file", json.RawMessage(`{"path":"x"}`))
	if err != nil || !allowed {
		t.Fatalf("requestApproval = (%v, %v), want (true, nil)", allowed, err)
	}
	var status string
	var auto bool
	var answeredAt sql.NullString
	if err := database.QueryRow(`SELECT status,auto,answered_at FROM chat_approval WHERE chat_id=?`, chatID).Scan(&status, &auto, &answeredAt); err != nil {
		t.Fatal(err)
	}
	if status != "allowed" || !auto || !answeredAt.Valid {
		t.Fatalf("approval = (%q, %v, %v), want allowed, auto, answered", status, auto, answeredAt)
	}
	select {
	case event := <-events:
		t.Fatalf("auto-allowed request emitted event %#v", event)
	default:
	}
}

func TestAutoAllowResolvesPendingApprovalWhileRunning(t *testing.T) {
	database, handler, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	server := handler.(*Server)
	server.chats.approvalPollInterval = time.Millisecond
	server.chats.approvalTimeout = time.Second
	runtime := server.chats.runtime(chatID)
	runtime.mu.Lock()
	runtime.busy = true
	runtime.mu.Unlock()
	defer func() {
		runtime.mu.Lock()
		runtime.busy = false
		runtime.mu.Unlock()
	}()
	result := make(chan struct {
		allowed bool
		err     error
	}, 1)
	go func() {
		allowed, err := server.chats.requestApproval(context.Background(), chatID, "write_file", json.RawMessage(`{"path":"x"}`))
		result <- struct {
			allowed bool
			err     error
		}{allowed, err}
	}()
	var approvalID string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending'`, chatID).Scan(&approvalID)
		if err == nil {
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if approvalID == "" {
		t.Fatal("pending approval was not inserted")
	}
	var updated chat
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+chatID, map[string]bool{"auto_allow": true}, &updated); code != http.StatusOK || !updated.AutoAllow {
		t.Fatalf("enable auto_allow while running = %d, %#v", code, updated)
	}
	select {
	case got := <-result:
		if got.err != nil || !got.allowed {
			t.Fatalf("approval waiter = (%v, %v), want (true, nil)", got.allowed, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending approval waiter did not return")
	}
	var status string
	var auto bool
	if err := database.QueryRow(`SELECT status,auto FROM chat_approval WHERE id=?`, approvalID).Scan(&status, &auto); err != nil || status != "allowed" || !auto {
		t.Fatalf("resolved approval = (%q, %v), %v", status, auto, err)
	}
	var detail struct {
		AutoApprovals []chatApproval `json:"auto_approvals"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats/"+chatID, nil, &detail); code != http.StatusOK || len(detail.AutoApprovals) != 1 || !detail.AutoApprovals[0].Auto {
		t.Fatalf("auto approvals API = %d, %#v", code, detail)
	}
}

func TestBuildClaudeArgs(t *testing.T) {
	configPath, err := writeClaudeMCPConfig(t.TempDir(), map[string]any{"my-tickets": map[string]any{
		"type": "stdio", "command": "/app/mt", "args": []string{"mcp", "-d", "/data/board.db", "-approve-chat", "chat-id"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	args := buildClaudeArgsWithConfig("sonnet", "session-id", configPath)
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
	if index < 0 || index+1 == len(args) || args[index+1] != configPath {
		t.Fatalf("missing MCP config args: %v", args)
	}
	if strings.Contains(joined, "approve-chat") || strings.Contains(joined, "/data/board.db") {
		t.Fatalf("Claude config leaked in argv: %q", joined)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
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
	if code, body := requestRaw(t, server, http.MethodPut, "/api/chats/"+chatID, map[string]string{"model": "opus"}); code != http.StatusConflict || !strings.Contains(body, "Stop the reply before changing the model") {
		t.Fatalf("busy model change = %d %s", code, body)
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

func TestChatModelChangeClosesIdleProcessAndRestartsWithNewModel(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET model='sonnet' WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	processes := []*pipeChatProcess{newPipeChatProcess(), newPipeChatProcess()}
	var models []string
	launch := 0
	server := New(database, "local", false, "dev", root, Options{
		findClaude:     func() (string, error) { return "claude-test", nil },
		commandBuilder: func(_, _, _, model, _ string) ([]string, error) { return []string{model}, nil },
		processFactory: func(_ string, args, _ []string, _ string) (chatProcess, error) {
			models = append(models, args[0])
			p := processes[launch]
			launch++
			return p, nil
		},
	})
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "first"}, nil); code != http.StatusAccepted {
		t.Fatalf("first send = %d", code)
	}
	select {
	case <-processes[0].input:
	case <-time.After(time.Second):
		t.Fatal("first process did not receive message")
	}
	if _, err := io.WriteString(processes[0].stdoutW, `{"type":"result","result":"done"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	if code := requestJSON(t, server, http.MethodPut, "/api/chats/"+chatID, map[string]string{"model": "opus"}, nil); code != http.StatusOK {
		t.Fatalf("idle model change = %d", code)
	}
	select {
	case <-processes[0].done:
	case <-time.After(time.Second):
		t.Fatal("idle Claude process was not closed")
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "second"}, nil); code != http.StatusAccepted {
		t.Fatalf("second send = %d", code)
	}
	select {
	case <-processes[1].input:
	case <-time.After(time.Second):
		t.Fatal("restarted process did not receive message")
	}
	if !reflect.DeepEqual(models, []string{"sonnet", "opus"}) {
		t.Fatalf("process models = %v", models)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestChatSendRejectsUnsupportedConnector(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'local','Local',?)`, connectorID, now()); err != nil {
		t.Fatal(err)
	}
	chatID := createDriverTestChat(t, database, connectorID, root)
	start, existed := runtimes["local"]
	delete(runtimes, "local")
	defer func() {
		if existed {
			runtimes["local"] = start
		}
	}()
	server := New(database, "local", false, "dev", root)
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "hello"}, nil); code != http.StatusBadRequest {
		t.Fatalf("unsupported driver send status = %d", code)
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
