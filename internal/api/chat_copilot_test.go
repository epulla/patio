package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
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
	var configPaths []string
	methods := make(chan string, 8)
	permissions := make(chan string, 2)
	events := make(chan any, 32)
	server := New(database, "local", false, "dev", root, Options{
		DBPath:      filepath.Join(root, "test.db"),
		findCopilot: func() (string, error) { return "copilot-test", nil },
		processFactory: func(path string, args, env []string, cwd string) (chatProcess, error) {
			if path != "copilot-test" || len(args) < 6 || args[0] != "--acp" || args[1] != "--stdio" || args[2] != "--additional-mcp-config" || !strings.HasPrefix(args[3], "@") || cwd != filepath.Join(root, "chats", chatID) {
				t.Errorf("Copilot process launch = %q %v %q", path, args, cwd)
			}
			if !hasEnv(env, "COPILOT_GITHUB_TOKEN", "copilot-test-secret") {
				t.Error("Copilot credential was not passed through connector environment")
			}
			configPath := strings.TrimPrefix(args[3], "@")
			info, err := os.Stat(configPath)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Errorf("Copilot config mode/stat = %v, %v", info, err)
			}
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				Servers map[string]map[string]any `json:"mcpServers"`
			}
			if err := json.Unmarshal(data, &config); err != nil {
				t.Fatal(err)
			}
			if config.Servers["patio"]["type"] != "stdio" || config.Servers["stdio-test"]["type"] != "stdio" || config.Servers["stdio-test"]["env"].(map[string]any)["TOKEN"] != "stdio-secret" {
				t.Errorf("Copilot stdio config = %#v", config.Servers)
			}
			configPaths = append(configPaths, configPath)
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
	if _, err := os.Stat(configPaths[0]); !os.IsNotExist(err) {
		t.Errorf("Copilot config still exists after close: %v", err)
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
	if _, err := os.Stat(configPaths[1]); !os.IsNotExist(err) {
		t.Errorf("Copilot config still exists after stop: %v", err)
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

func TestCopilotSteersRunningPrompt(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	connectorID := createCopilotTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	process := newPipeChatProcess()
	events := make(chan any, 32)
	server := New(database, "local", false, "test", root, Options{
		DBPath:         filepath.Join(root, "test.db"),
		findCopilot:    func() (string, error) { return "copilot-test", nil },
		processFactory: func(string, []string, []string, string) (chatProcess, error) { return process, nil },
		Emit:           func(_ string, event any) { events <- event },
	})
	defer server.Close()
	requests := make(chan acpMessage, 8)
	go func() {
		for line := range process.input {
			var message acpMessage
			if json.Unmarshal([]byte(line), &message) == nil {
				requests <- message
			}
		}
	}()
	readRequest := func(method string) acpMessage {
		t.Helper()
		select {
		case got := <-requests:
			if got.Method != method {
				t.Fatalf("ACP method=%q want %q", got.Method, method)
			}
			return got
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %s", method)
		}
		return acpMessage{}
	}
	reply := func(id json.RawMessage, result any) {
		t.Helper()
		writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	initialResponse := make(chan int, 1)
	go func() {
		code, _ := requestRaw(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "first"})
		initialResponse <- code
	}()
	initialize := readRequest("initialize")
	reply(initialize.ID, map[string]any{"agentCapabilities": map[string]any{"promptCapabilities": map[string]any{"image": false}}})
	newSession := readRequest("session/new")
	reply(newSession.ID, map[string]any{"sessionId": "steer-session"})
	if code := <-initialResponse; code != http.StatusAccepted {
		t.Fatalf("initial prompt=%d", code)
	}
	first := readRequest("session/prompt")
	if _, err := database.Exec(`INSERT INTO chat_queue(id,chat_id,text,attachments,created_at) VALUES('steer-q',?,'second','[]','1')`, chatID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/queue/send", nil, nil); code != http.StatusAccepted {
		t.Fatalf("queue send=%d", code)
	}
	second := readRequest("session/prompt")
	writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "steer-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"text": "before "}}}})
	reply(first.ID, map[string]any{"stopReason": "cancelled"})
	if !server.chats.Running(chatID) {
		t.Fatal("turn stopped after superseded prompt returned cancelled")
	}
	for _, event := range drainEvents(events) {
		if isDoneChatEvent(event) {
			t.Fatal("superseded prompt emitted done")
		}
	}
	writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "steer-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"text": "after"}}}})
	reply(second.ID, map[string]any{"stopReason": "end_turn"})
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	var count int
	var content string
	if err := database.QueryRow(`SELECT COUNT(*),MAX(content) FROM chat_message WHERE chat_id=? AND role='assistant'`, chatID).Scan(&count, &content); err != nil || count != 1 || content != "before after" {
		t.Fatalf("assistant count=%d content=%q err=%v", count, content, err)
	}
	done := 0
	for _, event := range drainEvents(events) {
		if isDoneChatEvent(event) {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("done events=%d want 1", done)
	}
}

func drainEvents(events <-chan any) []any {
	var result []any
	for {
		select {
		case event := <-events:
			result = append(result, event)
		default:
			return result
		}
	}
}

func isDoneChatEvent(event any) bool {
	data, _ := json.Marshal(event)
	var value map[string]any
	_ = json.Unmarshal(data, &value)
	return value["type"] == "done" || value["event"] == "done"
}

func TestCopilotPermissionAllowsOnlyPatioMCP(t *testing.T) {
	tests := []struct {
		name      string
		params    string
		toolLabel string
		want      bool
	}{
		{"captured Patio MCP request", `{"sessionId":"6e391eff-802a-4348-afc1-67828d9b4b35","toolCall":{"toolCallId":"call_M7k0BA0y7VhvSu1JutXncGCW","title":"list_boards","kind":"other","status":"pending","rawInput":{}},"options":[{"optionId":"allow_once","kind":"allow_once","name":"Allow once"},{"optionId":"allow_always","kind":"allow_always","name":"Always allow"},{"optionId":"reject_once","kind":"reject_once","name":"Deny"}]}`, "patio-list_boards", true},
		{"payload claims Patio without row label", `{"toolCall":{"title":"list_boards","kind":"mcp","serverName":"patio","rawInput":{"serverName":"patio"}}}`, "", false},
		{"bash command mentions Patio", `{"toolCall":{"title":"Run shell command","kind":"execute","rawInput":{"command":"echo patio-list_boards"}}}`, "", false},
		{"similarly named MCP server", `{"toolCall":{"title":"list_boards","kind":"other","rawInput":{}}}`, "my-patio-clone-list_boards", false},
		{"spoofed unknown tool", `{"toolCall":{"title":"unknown","kind":"other"}}`, "patio-not_registered", false},
		{"uppercase label", `{"toolCall":{"title":"list_boards","kind":"other"}}`, "PATIO-list_boards", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var params struct {
				ToolCall struct {
					Kind       string          `json:"kind"`
					Server     string          `json:"server"`
					ServerName string          `json:"serverName"`
					RawInput   json.RawMessage `json:"rawInput"`
					Meta       json.RawMessage `json:"_meta"`
				} `json:"toolCall"`
			}
			if err := json.Unmarshal([]byte(test.params), &params); err != nil {
				t.Fatal(err)
			}
			got := isPatioMCPToolLabel(test.toolLabel, []string{"list_boards"})
			if got != test.want {
				t.Fatalf("isPatioMCPToolLabel = %t, want %t", got, test.want)
			}
		})
	}
}

func TestCopilotPermissionApprovalUsesKnownCorrelatedTool(t *testing.T) {
	tests := []struct {
		name         string
		toolLabel    string
		toolCall     map[string]any
		wantApproval bool
	}{
		{"known Patio tool", "patio-list_boards", map[string]any{"toolCallId": "call", "title": "list_boards", "kind": "other"}, false},
		{"spoofed unknown tool", "patio-not_registered", map[string]any{"toolCallId": "call", "title": "not_registered", "kind": "other"}, true},
		{"uppercase label", "PATIO-list_boards", map[string]any{"toolCallId": "call", "title": "list_boards", "kind": "other"}, true},
		{"payload claims Patio without correlated label", "", map[string]any{"toolCallId": "call", "title": "list_boards", "kind": "mcp", "serverName": "patio", "rawInput": map[string]any{"serverName": "patio"}}, true},
		{"bash command mentions Patio", "Run shell command", map[string]any{"toolCallId": "call", "title": "Run shell command", "kind": "execute", "rawInput": map[string]any{"command": "echo patio-list_boards"}}, true},
		{"other MCP server", "my-patio-clone-list_boards", map[string]any{"toolCallId": "call", "title": "list_boards", "kind": "other"}, true},
	}
	database, _, root := chatAPI(t)
	connectorID := createCopilotTestConnector(t, database)
	server := New(database, "local", false, "test", root, Options{PatioMCPTools: []string{"list_boards"}})
	defer server.Close()
	server.chats.approvalPollInterval = time.Millisecond
	server.chats.approvalTimeout = 2 * time.Second
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			chatID := createDriverTestChat(t, database, connectorID, root)
			var response bytes.Buffer
			d := &copilotRuntime{manager: server.chats, chatID: chatID, ctx: context.Background(), client: &acpClient{stdin: &response}, toolNames: map[string]string{"call": test.toolLabel}}
			params, err := json.Marshal(map[string]any{"toolCall": test.toolCall, "options": []any{map[string]string{"optionId": "allow_once", "kind": "allow_once"}, map[string]string{"optionId": "reject_once", "kind": "reject_once"}}})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				d.handlePermission(acpMessage{JSONRPC: "2.0", ID: json.RawMessage("1"), Params: params})
				close(done)
			}()
			var approvalID string
			if test.wantApproval {
				waitFor(t, func() bool {
					return database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending'`, chatID).Scan(&approvalID) == nil
				})
				if _, err := database.Exec(`UPDATE chat_approval SET status='allowed' WHERE id=?`, approvalID); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("permission request did not complete")
			}
			var approvals int
			if err := database.QueryRow(`SELECT COUNT(*) FROM chat_approval WHERE chat_id=?`, chatID).Scan(&approvals); err != nil {
				t.Fatal(err)
			}
			if (approvals > 0) != test.wantApproval {
				t.Fatalf("approval rows = %d, want approval=%t", approvals, test.wantApproval)
			}
		})
	}
}

func TestCopilotStartFailureRemovesConfigDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	keyring.MockInit()
	database, _, root := chatAPI(t)
	connectorID := createCopilotTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	server := New(database, "local", false, "dev", root, Options{
		DBPath:         filepath.Join(root, "test.db"),
		findCopilot:    func() (string, error) { return "copilot-test", nil },
		processFactory: func(string, []string, []string, string) (chatProcess, error) { return nil, errors.New("launch failed") },
	})
	defer server.Close()
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "start"}, nil); code != http.StatusInternalServerError {
		t.Fatalf("send = %d", code)
	}
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	parent, err := copilotMCPConfigParent()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("config dirs remain after start failure: %v", entries)
	}
}

func TestCleanupStaleCopilotMCPConfigs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	parent, err := copilotMCPConfigParent()
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(parent, "chat-stale")
	if err := os.Mkdir(stale, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "mcp.json"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CleanupStaleCopilotMCPConfigs(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale config dir remains: %v", err)
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
	prompts := make(chan string, 2)
	launch := 0
	server := New(database, "local", false, "dev", root, Options{
		DBPath:      filepath.Join(root, "test.db"),
		findCopilot: func() (string, error) { return "copilot-test", nil },
		processFactory: func(_ string, args, _ []string, _ string) (chatProcess, error) {
			want := []string{"--acp", "--stdio", "--additional-mcp-config"}
			if launch == 1 {
				want = []string{"--acp", "--stdio", "--additional-mcp-config"}
			}
			if len(args) < 4 || !reflect.DeepEqual(args[:3], want) || !strings.HasPrefix(args[3], "@") || (launch == 0 && !reflect.DeepEqual(args[4:], []string{"--reasoning-effort", "low"})) || (launch == 1 && len(args) != 4) {
				t.Errorf("Copilot args = %v, want %v", args, want)
			}
			process := processes[launch]
			go serveThinkingCopilot(process, launch, loaded, prompts)
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
	for i := range 2 {
		select {
		case got := <-prompts:
			if i == 0 && !strings.Contains(got, "You are Martin") {
				t.Fatalf("new session prompt missing persona: %q", got)
			}
			if i == 1 && strings.Contains(got, "You are Martin") {
				t.Fatalf("resumed session prompt contains persona: %q", got)
			}
		case <-time.After(time.Second):
			t.Fatal("Copilot prompt was not received")
		}
	}
	select {
	case <-processes[0].done:
	case <-time.After(time.Second):
		t.Fatal("Copilot process was not restarted")
	}
}

func serveThinkingCopilot(process *pipeChatProcess, index int, loaded chan<- string, prompts chan<- string) {
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
			var params struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			_ = json.Unmarshal(request.Params, &params)
			if len(params.Prompt) > 0 {
				prompts <- params.Prompt[0].Text
			}
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
			if !ok || len(servers) != 1 {
				return
			}
			httpServer := servers[0].(map[string]any)
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
				writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "session-copilot", "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call_true", "title": "my-patio-clone-list_boards", "kind": "other", "status": "pending"}}})
				writeACPTestMessage(process, map[string]any{"jsonrpc": "2.0", "id": 0, "method": "session/request_permission", "params": map[string]any{
					"sessionId": "session-copilot",
					"toolCall":  map[string]any{"toolCallId": "call_true", "title": "list_boards", "kind": "other", "status": "pending", "rawInput": map[string]any{}},
					"options":   []any{map[string]any{"optionId": "allow_once", "kind": "allow_once", "name": "Allow once"}, map[string]any{"optionId": "allow_always", "kind": "allow_always", "name": "Always allow"}, map[string]any{"optionId": "reject_once", "kind": "reject_once", "name": "Deny"}},
				}})
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
	want := "reject_once"
	if allow {
		want = "allow_once"
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
