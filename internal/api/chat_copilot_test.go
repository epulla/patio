package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestCopilotACPDriver(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	connectorID := createCopilotTestConnector(t, database)
	if err := keyring.Set(connectorService, connectorSecret(connectorID), "copilot-test-secret"); err != nil {
		t.Fatal(err)
	}
	chatID := createDriverTestChat(t, database, connectorID, root)
	processes := []*pipeChatProcess{newPipeChatProcess(), newPipeChatProcess()}
	launch := 0
	methods := make(chan string, 8)
	permissions := make(chan string, 2)
	events := make(chan any, 32)
	server := New(database, "local", false, "dev", root, Options{
		DBPath:      filepath.Join(root, "test.db"),
		findCopilot: func() (string, error) { return "copilot-test", nil },
		processFactory: func(path string, args, env []string, cwd string) (chatProcess, error) {
			if path != "copilot-test" || strings.Join(args, " ") != "--acp --stdio --reasoning-effort low" || cwd != filepath.Join(root, "chats", chatID) {
				t.Errorf("Copilot process launch = %q %v %q", path, args, cwd)
			}
			if !hasEnv(env, "COPILOT_GITHUB_TOKEN", "copilot-test-secret") {
				t.Error("Copilot credential was not passed through connector environment")
			}
			launch++
			return processes[launch-1], nil
		},
		Emit: func(_ string, event any) { events <- event },
	})
	addChatMCPTestServers(t, server)
	if err := keyring.Set(connectorService, connectorSecret(connectorID), "copilot-test-secret"); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.chats.approvalPollInterval = time.Millisecond
	for i, process := range processes {
		go serveCopilotFake(process, func(message acpMessage) {
			if message.Method != "" {
				methods <- message.Method
			}
		}, i, permissions)
	}

	if code, body := requestRaw(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "first"}); code != http.StatusAccepted {
		t.Fatalf("first prompt status = %d: %s", code, body)
	}
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	if err := server.chats.CloseChat(chatID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "allow"}, nil); code != http.StatusAccepted {
		t.Fatalf("allow prompt status = %d", code)
	}
	answerACPApproval(t, server, database, chatID, true, permissions)
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "deny"}, nil); code != http.StatusAccepted {
		t.Fatalf("deny prompt status = %d", code)
	}
	answerACPApproval(t, server, database, chatID, false, permissions)
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "cancel"}, nil); code != http.StatusAccepted {
		t.Fatalf("cancel prompt status = %d", code)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/stop", nil, nil); code != http.StatusNoContent {
		t.Fatalf("stop status = %d", code)
	}

	var sessionID string
	if err := database.QueryRow(`SELECT session_id FROM chat WHERE id=?`, chatID).Scan(&sessionID); err != nil || sessionID != "session-copilot" {
		t.Fatalf("session id = %q, %v", sessionID, err)
	}
	wantMethods := []string{"initialize", "session/new", "session/prompt", "initialize", "session/load", "session/prompt", "session/prompt", "session/prompt", "session/cancel"}
	for _, want := range wantMethods {
		select {
		case got := <-methods:
			if got != want {
				t.Fatalf("ACP method = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing ACP method %q", want)
		}
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
	var assistant []string
	for _, message := range messages {
		if message.Role == "assistant" {
			assistant = append(assistant, message.Content)
		}
	}
	if strings.Join(assistant, ",") != "hello world,hello world,hello world" {
		t.Fatalf("assistant messages = %v", assistant)
	}
	if !hasChatEvent(events, "delta") || !hasChatEvent(events, "tool") || !hasChatEvent(events, "done") {
		t.Fatal("expected delta, tool, and done events")
	}
	var approvals int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_approval WHERE chat_id=? AND status IN ('allowed','denied')`, chatID).Scan(&approvals); err != nil || approvals != 2 {
		t.Fatalf("answered approvals = %d, %v", approvals, err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCopilotAuthError(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	connectorID := createCopilotTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	process := newPipeChatProcess()
	server := New(database, "local", false, "dev", root, Options{
		findCopilot:    func() (string, error) { return "copilot-test", nil },
		processFactory: func(string, []string, []string, string) (chatProcess, error) { return process, nil },
	})
	defer server.Close()
	go func() {
		for line := range process.input {
			var request acpMessage
			if json.Unmarshal([]byte(line), &request) != nil {
				continue
			}
			switch request.Method {
			case "initialize":
				writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{}})
			case "session/new":
				writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "not signed in"}})
			}
		}
	}()
	if code, body := requestRaw(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "hello"}); code != http.StatusAccepted {
		t.Fatalf("send status = %d: %s", code, body)
	}
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	var role, content string
	if err := database.QueryRow(`SELECT role,content FROM chat_message WHERE chat_id=? AND role='error'`, chatID).Scan(&role, &content); err != nil || content != copilotAuthError {
		t.Fatalf("auth message = (%q, %q), %v", role, content, err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCopilotThinkingChangeRestartsAndLoadsSession(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	connectorID := createCopilotTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	processes := []*pipeChatProcess{newPipeChatProcess(), newPipeChatProcess()}
	loaded := make(chan string, 2)
	launch := 0
	server := New(database, "local", false, "dev", root, Options{
		DBPath:      filepath.Join(root, "test.db"),
		findCopilot: func() (string, error) { return "copilot-test", nil },
		processFactory: func(_ string, args, _ []string, _ string) (chatProcess, error) {
			want := []string{"--acp", "--stdio", "--reasoning-effort", "low"}
			if launch == 1 {
				want = []string{"--acp", "--stdio"}
			}
			if !reflect.DeepEqual(args, want) {
				t.Errorf("Copilot args = %v, want %v", args, want)
			}
			process := processes[launch]
			go serveThinkingCopilot(process, launch, loaded)
			launch++
			return process, nil
		},
	})
	defer server.Close()
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
	case got := <-loaded:
		if got != "session/new:" {
			t.Fatalf("first Copilot session = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("Copilot did not create initial session")
	}
	select {
	case got := <-loaded:
		if got != "session/load:session-thinking-copilot" {
			t.Fatalf("resumed Copilot session = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("Copilot did not resume session")
	}
	select {
	case <-processes[0].done:
	case <-time.After(time.Second):
		t.Fatal("Copilot process was not restarted")
	}
}

func serveThinkingCopilot(process *pipeChatProcess, index int, loaded chan<- string) {
	for line := range process.input {
		var request acpMessage
		if json.Unmarshal([]byte(line), &request) != nil {
			return
		}
		switch request.Method {
		case "initialize":
			writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{}})
		case "session/new", "session/load":
			var params map[string]any
			_ = json.Unmarshal(request.Params, &params)
			if index == 0 && request.Method != "session/new" || index == 1 && (request.Method != "session/load" || params["sessionId"] != "session-thinking-copilot") {
				return
			}
			sessionID, _ := params["sessionId"].(string)
			loaded <- request.Method + ":" + sessionID
			writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"sessionId": "session-thinking-copilot"}})
		case "session/prompt":
			writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "session-thinking-copilot", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"text": "OK"}}}})
			writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"stopReason": "end_turn"}})
		}
	}
}

func createCopilotTestConnector(t *testing.T, database *sql.DB) string {
	t.Helper()
	id := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'copilot','Copilot',?)`, id, now()); err != nil {
		t.Fatal(err)
	}
	return id
}

func hasEnv(env []string, name, value string) bool {
	for _, entry := range env {
		if entry == name+"="+value {
			return true
		}
	}
	return false
}

func serveCopilotFake(process *pipeChatProcess, method func(acpMessage), processIndex int, permissions chan<- string) {
	promptCount := 0
	for line := range process.input {
		var request acpMessage
		if json.Unmarshal([]byte(line), &request) != nil {
			continue
		}
		if request.Method != "" {
			method(request)
		}
		switch request.Method {
		case "initialize":
			var params struct {
				ProtocolVersion    int `json:"protocolVersion"`
				ClientCapabilities struct {
					FS struct {
						ReadTextFile  bool `json:"readTextFile"`
						WriteTextFile bool `json:"writeTextFile"`
					} `json:"fs"`
					Terminal bool `json:"terminal"`
				} `json:"clientCapabilities"`
			}
			if json.Unmarshal(request.Params, &params) != nil || params.ProtocolVersion != 1 || params.ClientCapabilities.FS.ReadTextFile || params.ClientCapabilities.FS.WriteTextFile || params.ClientCapabilities.Terminal {
				return
			}
			writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{}})
		case "session/new", "session/load":
			if processIndex == 0 && request.Method != "session/new" || processIndex == 1 && request.Method != "session/load" {
				return
			}
			var params map[string]any
			_ = json.Unmarshal(request.Params, &params)
			servers, ok := params["mcpServers"].([]any)
			if !ok || len(servers) != 3 || servers[0].(map[string]any)["name"] != "patio" || len(servers[0].(map[string]any)["env"].([]any)) != 0 {
				return
			}
			stdio := servers[1].(map[string]any)
			stdioEnv := stdio["env"].([]any)
			if stdio["name"] != "stdio-test" || stdioEnv[0].(map[string]any)["name"] != "TOKEN" || stdioEnv[0].(map[string]any)["value"] != "stdio-secret" {
				return
			}
			httpServer := servers[2].(map[string]any)
			headers := httpServer["headers"].([]any)
			if httpServer["type"] != "http" || httpServer["name"] != "http-test" || headers[0].(map[string]any)["name"] != "Authorization" || headers[0].(map[string]any)["value"] != "Bearer http-secret" {
				return
			}
			if request.Method == "session/load" && params["sessionId"] != "session-copilot" {
				return
			}
			if request.Method == "session/load" {
				writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "session-copilot", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "replayed"}}}})
			}
			writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"sessionId": "session-copilot"}})
		case "session/prompt":
			promptCount++
			writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "session-copilot", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "hello "}}}})
			writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "session-copilot", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "world"}}}})
			if processIndex == 1 && promptCount <= 2 {
				writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": 900 + promptCount, "method": "some/unknown", "params": map[string]any{}})
				var unknown acpMessage
				if !readACPResponse(process, &unknown) || unknown.Error == nil || unknown.Error.Code != -32601 {
					return
				}
				writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "session-copilot", "update": map[string]any{"sessionUpdate": "tool_call", "title": "Write file"}}})
				writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": 100 + promptCount, "method": "session/request_permission", "params": map[string]any{"toolCall": map[string]any{"title": "Write file", "kind": "edit"}, "options": []any{map[string]any{"optionId": "allow-custom", "name": "Allow once", "kind": "allow_once"}, map[string]any{"optionId": "deny-custom", "name": "Deny once", "kind": "reject_once"}}}})
				var answer acpMessage
				if !readACPResponse(process, &answer) || answer.Error != nil {
					return
				}
				var outcome struct {
					Outcome struct {
						OptionID string `json:"optionId"`
					} `json:"outcome"`
				}
				_ = json.Unmarshal(answer.Result, &outcome)
				permissions <- outcome.Outcome.OptionID
			}
			if processIndex == 1 && promptCount == 3 {
				continue
			}
			writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"stopReason": "end_turn"}})
		case "session/cancel":
			return
		}
	}
}

func answerACPApproval(t *testing.T, server *Server, database *sql.DB, chatID string, allow bool, permissions <-chan string) {
	t.Helper()
	var id string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&id)
		if err == nil {
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if id == "" {
		t.Fatal("Copilot approval was not created")
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+id, map[string]bool{"allow": allow}, nil); code != http.StatusOK {
		t.Fatalf("answer approval status = %d", code)
	}
	want := "deny-custom"
	if allow {
		want = "allow-custom"
	}
	select {
	case got := <-permissions:
		if got != want {
			t.Fatalf("selected option = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("permission response was not sent")
	}
}

func readACPResponse(process *pipeChatProcess, response *acpMessage) bool {
	select {
	case line := <-process.input:
		return json.Unmarshal([]byte(line), response) == nil
	case <-time.After(time.Second):
		return false
	}
}

func writeACPTestMessage(process *pipeChatProcess, message any) {
	data, _ := json.Marshal(message)
	_, _ = io.WriteString(process.stdoutW, string(data)+"\n")
}

func hasChatEvent(events <-chan any, kind string) bool {
	for len(events) > 0 {
		event := (<-events).(map[string]any)
		if event["type"] == kind {
			return true
		}
	}
	return false
}
