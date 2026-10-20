package api

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

	connectorcli "patio/internal/connector"
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

func TestEmitThinkingPersistsBeforeToolOnlyWhenEnabled(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET show_thinking=1 WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	var events []map[string]any
	manager := newChatManager(database, Options{Emit: func(_ string, event any) { events = append(events, event.(map[string]any)) }})
	rt := manager.runtime(chatID)
	rt.showThinking = true
	manager.emitThinking(chatID, "reason")
	if err := manager.emitTool(chatID, "tool call"); err != nil {
		t.Fatal(err)
	}
	if err := manager.finishTurn(chatID, "answer", nil); err != nil {
		t.Fatal(err)
	}
	var roles []string
	rows, err := database.Query(`SELECT role FROM chat_message WHERE chat_id=? ORDER BY rowid`, chatID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			t.Fatal(err)
		}
		roles = append(roles, role)
	}
	if !reflect.DeepEqual(roles, []string{"thinking", "tool", "assistant"}) {
		t.Fatalf("roles = %v", roles)
	}
	if len(events) != 4 || events[0]["type"] != "thinking_delta" || events[1]["type"] != "thinking" || events[2]["type"] != "tool" || events[3]["type"] != "done" {
		t.Fatalf("events = %#v", events)
	}
	if _, err := database.Exec(`DELETE FROM chat_message WHERE chat_id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	rt.showThinking = false
	manager.emitThinking(chatID, "private")
	if err := manager.finishTurn(chatID, "answer", nil); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='thinking'`, chatID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("thinking count = %d, %v", count, err)
	}
}

// Send holds rt.mu while a driver starts a turn; a driver goroutine emitting deltas must not wait on it.
func TestStreamEmitsDoNotWaitOnRuntimeLock(t *testing.T) {
	database, _, root := chatAPI(t)
	chatID := createDriverTestChat(t, database, createTestConnector(t, database), root)
	manager := newChatManager(database, Options{Emit: func(string, any) {}})
	rt := manager.runtime(chatID)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	done := make(chan struct{})
	go func() {
		manager.emitThinking(chatID, "reason")
		manager.markToolAfterText(chatID)
		manager.emitDelta(chatID, "text")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream emit blocked on rt.mu")
	}
}

func TestDriverThinkingOnOff(t *testing.T) {
	for _, driverName := range []string{"claude", "codex", "copilot"} {
		for _, enabled := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/enabled=%t", driverName, enabled), func(t *testing.T) {
				database, _, root := chatAPI(t)
				connectorID := createTestConnector(t, database)
				chatID := createDriverTestChat(t, database, connectorID, root)
				manager := newChatManager(database, Options{})
				rt := manager.runtime(chatID)
				rt.showThinking = enabled
				switch driverName {
				case "claude":
					d := &claudeRuntime{manager: manager, chatID: chatID, runtime: rt}
					fixture, err := os.ReadFile(filepath.Join("testdata", "claude-thinking.jsonl"))
					if err != nil {
						t.Fatal(err)
					}
					if err := d.parseLine(fixture[:len(fixture)-1]); err != nil {
						t.Fatal(err)
					}
				case "codex":
					d := &codexRuntime{manager: manager, chatID: chatID, runtime: rt, items: map[string]json.RawMessage{}}
					if err := d.handleNotification(connectorcli.Notification{Method: "item/reasoning/summaryTextDelta", Params: json.RawMessage(`{"delta":"reason"}`)}); err != nil {
						t.Fatal(err)
					}
				case "copilot":
					d := &copilotRuntime{manager: manager, chatID: chatID}
					d.handleUpdate(json.RawMessage(`{"update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"reason"}}}`))
				}
				if err := manager.finishTurn(chatID, "answer", nil); err != nil {
					t.Fatal(err)
				}
				var roles []string
				rows, err := database.Query(`SELECT role FROM chat_message WHERE chat_id=? ORDER BY rowid`, chatID)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				for rows.Next() {
					var role string
					if err := rows.Scan(&role); err != nil {
						t.Fatal(err)
					}
					roles = append(roles, role)
				}
				want := []string{"assistant"}
				if enabled {
					want = []string{"thinking", "assistant"}
				}
				if !reflect.DeepEqual(roles, want) {
					t.Fatalf("roles = %v, want %v", roles, want)
				}
			})
		}
	}
}

func TestDriverToolResults(t *testing.T) {
	big := strings.Repeat("x", 5000)
	for _, driverName := range []string{"claude", "codex", "copilot"} {
		t.Run(driverName, func(t *testing.T) {
			database, _, root := chatAPI(t)
			connectorID := createTestConnector(t, database)
			chatID := createDriverTestChat(t, database, connectorID, root)
			manager := newChatManager(database, Options{})
			rt := manager.runtime(chatID)
			switch driverName {
			case "claude":
				d := &claudeRuntime{manager: manager, chatID: chatID, runtime: rt}
				for _, line := range []string{
					`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"get_board","input":{}}]}}`,
					`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"` + big + `"}]}]}}`,
					`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{}}]}}`,
					`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"boom","is_error":true}]}}`,
					`{"type":"user","message":{"content":"plain text is not a tool result"}}`,
				} {
					if err := d.parseLine([]byte(line)); err != nil {
						t.Fatal(err)
					}
				}
			case "codex":
				d := &codexRuntime{manager: manager, chatID: chatID, runtime: rt, items: map[string]json.RawMessage{}}
				for _, item := range []string{
					`{"id":"i1","type":"mcpToolCall","server":"patio","toolName":"get_board","status":"completed","result":{"content":[{"type":"text","text":"` + big + `"}]}}`,
					`{"id":"i2","type":"commandExecution","command":"false","status":"failed","aggregatedOutput":"boom","exitCode":1}`,
				} {
					if err := d.handleNotification(connectorcli.Notification{Method: "item/completed", Params: json.RawMessage(`{"item":` + item + `}`)}); err != nil {
						t.Fatal(err)
					}
				}
			case "copilot":
				d := &copilotRuntime{manager: manager, chatID: chatID}
				for _, update := range []string{
					`{"sessionUpdate":"tool_call","toolCallId":"c1","title":"get_board"}`,
					`{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"` + big + `"}}]}`,
					`{"sessionUpdate":"tool_call","toolCallId":"c2","title":"run"}`,
					`{"sessionUpdate":"tool_call_update","toolCallId":"c2","status":"failed","content":[{"type":"content","content":{"type":"text","text":"boom"}}]}`,
				} {
					d.handleUpdate(json.RawMessage(`{"update":` + update + `}`))
				}
			}
			rows, err := database.Query(`SELECT tool_result,tool_result_size,tool_error FROM chat_message WHERE chat_id=? AND role='tool' ORDER BY rowid`, chatID)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			type row struct {
				preview string
				size    int
				failed  bool
			}
			var got []row
			for rows.Next() {
				var r row
				if err := rows.Scan(&r.preview, &r.size, &r.failed); err != nil {
					t.Fatal(err)
				}
				got = append(got, r)
			}
			if len(got) != 2 || got[0].size != 5000 || got[0].failed || len(got[0].preview) > storedToolResultMax ||
				got[1].size != 4 || !got[1].failed || got[1].preview != "boom" {
				t.Fatalf("tool rows = %+v", got)
			}
		})
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
			nativeCalls := 0
			server := New(database, "local", false, "dev", root, Options{Emit: func(name string, event any) {
				if name == "chat:"+chatID {
					events <- event
				}
			}, Notify: func(string, string, string) error { nativeCalls++; return nil }})
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
			deadline = time.Now().Add(time.Second)
			for nativeCalls == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			var notificationCount int
			if err := database.QueryRow(`SELECT count(*) FROM notification WHERE kind='approval' AND body='' AND link=?`, "/ai/chats/"+chatID).Scan(&notificationCount); err != nil {
				t.Fatal(err)
			}
			if notificationCount != 1 || nativeCalls != 1 {
				t.Fatalf("approval notifications=%d native calls=%d, want 1 each", notificationCount, nativeCalls)
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
	configPath, err := writeClaudeMCPConfig(t.TempDir(), map[string]any{"patio": map[string]any{
		"type": "stdio", "command": "/app/mt", "args": []string{"mcp", "-d", "/data/board.db", "-approve-chat", "chat-id", "-in-app"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	args := buildClaudeArgsWithConfig("sonnet", "session-id", configPath, ChatSystemPrompt(true, "chat-id", nil))
	joined := strings.Join(args, " ")
	for _, want := range []string{"-p", "--disallowedTools AskUserQuestion", "--append-system-prompt", "/tickets/<ticket id>", "To ask the user multiple-choice questions", "--input-format stream-json", "--output-format stream-json", "--verbose", "--include-partial-messages", "--model sonnet", "--strict-mcp-config", "--permission-prompt-tool mcp__patio__approve", "--resume session-id"} {
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
	server := config.Servers["patio"]
	if server.Command != "/app/mt" || !reflect.DeepEqual(server.Args, []string{"mcp", "-d", "/data/board.db", "-approve-chat", "chat-id", "-in-app"}) {
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
			if path != "claude-test" || len(args) != 3 || args[1] != "--append-system-prompt" || !strings.Contains(args[2], "/tickets/<ticket id>") || !strings.Contains(args[2], "/ai/chats/<chat id>") || !strings.Contains(args[2], "This chat's ID is "+chatID) || cwd != filepath.Join(root, "chats", chatID) {
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
	var queued map[string]any
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "again"}, &queued); code != http.StatusAccepted || queued["queued"] == nil {
		t.Fatalf("concurrent send status/data = %d %#v", code, queued)
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

func TestChatQueueCancel(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	server := New(database, "local", false, "dev", root)
	defer server.Close()
	if _, err := database.Exec(`INSERT INTO chat_queue(id,chat_id,text,created_at) VALUES('queued',?,'hello','now')`, chatID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, server, http.MethodDelete, "/api/chats/"+chatID+"/queue/queued", nil, nil); code != http.StatusNoContent {
		t.Fatalf("cancel = %d", code)
	}
	if code := requestJSON(t, server, http.MethodDelete, "/api/chats/"+chatID+"/queue/queued", nil, nil); code != http.StatusNotFound {
		t.Fatalf("missing cancel = %d", code)
	}
}

func TestChatQueueSendNow(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	fake := newPipeChatProcess()
	server := New(database, "local", false, "dev", root, Options{findClaude: func() (string, error) { return "claude-test", nil }, commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil }, processFactory: func(string, []string, []string, string) (chatProcess, error) { return fake, nil }})
	defer server.Close()
	if _, err := database.Exec(`INSERT INTO chat_queue(id,chat_id,text,created_at) VALUES('queued',?,'from queue','now')`, chatID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/queue/queued/send", nil, nil); code != http.StatusAccepted {
		t.Fatalf("send-now = %d", code)
	}
	select {
	case <-fake.input:
	case <-time.After(time.Second):
		t.Fatal("queued input not received")
	}
	var queued, messages int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_queue WHERE id='queued'`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='user' AND content='from queue'`, chatID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if queued != 0 || messages != 1 {
		t.Fatalf("queued=%d user messages=%d", queued, messages)
	}
	if _, err := io.WriteString(fake.stdoutW, `{"type":"result","result":"done"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	if _, err := database.Exec(`INSERT INTO chat_queue(id,chat_id,text,created_at) VALUES('busy',?,'busy','now')`, chatID); err != nil {
		t.Fatal(err)
	}
	// Start another turn, then verify queue row survives a busy send-now.
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "active"}, nil); code != http.StatusAccepted {
		t.Fatalf("active send = %d", code)
	}
	select {
	case <-fake.input:
	case <-time.After(time.Second):
		t.Fatal("active input not received")
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/queue/busy/send", nil, nil); code != http.StatusConflict {
		t.Fatalf("busy send-now = %d", code)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_queue WHERE id='busy'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("busy queue row count=%d err=%v", queued, err)
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

func TestOpenCodePicksUpMCPChangesOnNextSend(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	if _, err := database.Exec(`UPDATE connector SET kind='opencode' WHERE id=?`, connectorID); err != nil {
		t.Fatal(err)
	}
	chatID := createDriverTestChat(t, database, connectorID, root)
	fake := newOpenCodeFake(t)
	var configs []map[string]json.RawMessage
	server := New(database, "local", false, "dev", root, Options{
		DBPath: filepath.Join(root, "test.db"), findOpencode: func() (string, error) { return "opencode-test", nil },
		opencodeMajor: func(context.Context, string, []string) (int, error) { return 1, nil },
		opencodeStarter: func(path string, args, env []string) (opencodeProcess, error) {
			for i, item := range env {
				if strings.HasPrefix(item, "OPENCODE_CONFIG_CONTENT=") {
					var config map[string]json.RawMessage
					if err := json.Unmarshal([]byte(strings.TrimPrefix(item, "OPENCODE_CONFIG_CONTENT=")), &config); err != nil {
						return nil, err
					}
					var mcp map[string]json.RawMessage
					if err := json.Unmarshal(config["mcp"], &mcp); err != nil {
						return nil, err
					}
					original, _ := json.Marshal(mcp)
					var captured map[string]json.RawMessage
					_ = json.Unmarshal(original, &captured)
					configs = append(configs, captured)
					delete(mcp, "new-test")
					config["mcp"], _ = json.Marshal(mcp)
					content, _ := json.Marshal(config)
					env[i] = "OPENCODE_CONFIG_CONTENT=" + string(content)
				}
			}
			return fake.start(args, env)
		},
	})
	addChatMCPTestServers(t, server)
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	send := func(text string) {
		t.Helper()
		if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": text}, nil); code != http.StatusAccepted {
			t.Fatalf("send %q = %d", text, code)
		}
		fake.waitEvents(t)
		var sessionID string
		if err := database.QueryRow(`SELECT session_id FROM chat WHERE id=?`, chatID).Scan(&sessionID); err != nil {
			t.Fatal(err)
		}
		fake.publish(t, map[string]any{"type": "session.idle", "properties": map[string]any{"sessionID": sessionID}})
		waitFor(t, func() bool { return !server.chats.Running(chatID) })
	}
	send("first")
	send("unchanged")
	if len(configs) != 1 {
		t.Fatalf("unchanged MCP config restarted runtime: %d starts", len(configs))
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/mcp-servers", map[string]any{"name": "new-test", "kind": "http", "url": "https://new.example.test/mcp", "enabled": true}, nil); code != http.StatusCreated {
		t.Fatalf("add MCP server = %d", code)
	}
	send("after change")
	if len(configs) != 2 || configs[1]["new-test"] == nil {
		t.Fatalf("new runtime MCP config = %d starts, %#v", len(configs), configs)
	}
	var notes int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND content='MCP servers changed — reconnected.'`, chatID).Scan(&notes); err != nil || notes != 1 {
		t.Fatalf("reconnect notes = %d, %v", notes, err)
	}
}

func TestChatThinkingChangeRestartsClaudeWithResume(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	processes := []*pipeChatProcess{newPipeChatProcess(), newPipeChatProcess()}
	var launches [][]string
	launch := 0
	server := New(database, "local", false, "dev", root, Options{
		findClaude: func() (string, error) { return "claude-test", nil },
		commandBuilder: func(_, _, _, model, session string) ([]string, error) {
			args := []string{"--model", model}
			if session != "" {
				args = append(args, "--resume", session)
			}
			return args, nil
		},
		processFactory: func(_ string, args, env []string, _ string) (chatProcess, error) {
			launches = append(launches, append([]string(nil), args...))
			if launch == 0 && !hasEnv(env, "MAX_THINKING_TOKENS", "0") {
				t.Errorf("thinking-off Claude env missing MAX_THINKING_TOKENS=0: %v", env)
			}
			if launch == 1 && !reflect.DeepEqual(env, connectorEnv("claude", "")) {
				t.Errorf("thinking-on Claude env changed defaults: %v", env)
			}
			process := processes[launch]
			launch++
			return process, nil
		},
	})
	defer server.Close()
	for i, process := range processes {
		go func(i int, process *pipeChatProcess) {
			for range process.input {
				if i == 0 {
					_, _ = io.WriteString(process.stdoutW, `{"type":"system","subtype":"init","session_id":"session-thinking"}`+"\n")
				}
				_, _ = io.WriteString(process.stdoutW, `{"type":"result","result":"OK"}`+"\n")
			}
		}(i, process)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "first"}, nil); code != http.StatusAccepted {
		t.Fatalf("first send = %d", code)
	}
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	if code := requestJSON(t, server, http.MethodPut, "/api/chats/"+chatID, map[string]bool{"thinking": true}, nil); code != http.StatusOK {
		t.Fatalf("thinking update = %d", code)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "second"}, nil); code != http.StatusAccepted {
		t.Fatalf("second send = %d", code)
	}
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	select {
	case <-processes[0].done:
	case <-time.After(time.Second):
		t.Fatal("Claude process was not restarted")
	}
	if len(launches) != 2 || !reflect.DeepEqual(launches[0][:2], []string{"--model", ""}) || !reflect.DeepEqual(launches[1][:4], []string{"--model", "", "--resume", "session-thinking"}) || !strings.Contains(strings.Join(launches[0], " "), "/tickets/<ticket id>") {
		t.Fatalf("Claude launches = %#v", launches)
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
	deadline := time.Now().Add(5 * time.Second)
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
