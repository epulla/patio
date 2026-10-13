package api

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenCodeChatServerAndEvents(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	if _, err := database.Exec(`UPDATE connector SET kind='opencode' WHERE id=?`, connectorID); err != nil {
		t.Fatal(err)
	}
	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET show_thinking=1 WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE chat SET model='openai/gpt-4.1-nano' WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	reusedID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET session_id='resume-session' WHERE id=?`, reusedID); err != nil {
		t.Fatal(err)
	}
	fake := newOpenCodeFake(t)
	var versionChecks atomic.Int32
	var emittedMu sync.Mutex
	var emitted []map[string]any
	server := New(database, "local", false, "dev", root, Options{
		DBPath: filepath.Join(root, "test.db"),
		Emit: func(_ string, event any) {
			emittedMu.Lock()
			emitted = append(emitted, event.(map[string]any))
			emittedMu.Unlock()
		},
		findOpencode: func() (string, error) { return "opencode-test", nil },
		opencodeMajor: func(context.Context, string, []string) (int, error) {
			versionChecks.Add(1)
			return 1, nil
		},
		opencodeStarter: func(_ string, args, env []string) (opencodeProcess, error) {
			return fake.start(args, env)
		},
	})
	addChatMCPTestServers(t, server)
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "first prompt"}, nil); code != http.StatusAccepted {
		t.Fatalf("new session send status = %d", code)
	}
	firstSession := fake.waitSession(t, 0)
	if firstSession != "session-1" {
		t.Fatalf("created session = %q", firstSession)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+reusedID+"/messages", map[string]string{"text": "resume prompt"}, nil); code != http.StatusAccepted {
		t.Fatalf("reused session send status = %d", code)
	}
	fake.waitEvents(t)
	fake.mu.Lock()
	if fake.sessions != 1 || fake.serverStarts != 1 || fake.eventStreams != 1 {
		t.Fatalf("sessions=%d server starts=%d event streams=%d", fake.sessions, fake.serverStarts, fake.eventStreams)
	}
	if got := versionChecks.Load(); got != 1 {
		t.Fatalf("version checks = %d; want one per server start", got)
	}
	if !slices.Contains(fake.promptSessions, "resume-session") {
		t.Fatalf("reused session was not prompted: %v", fake.promptSessions)
	}
	if len(fake.directories) < 3 {
		t.Fatalf("directory query missing from session calls: %v", fake.directories)
	}
	for _, directory := range fake.directories {
		if directory == "" {
			t.Fatalf("empty directory query: %v", fake.directories)
		}
	}
	if fake.promptModels[0] != "openai/gpt-4.1-nano" {
		t.Fatalf("prompt model = %#v", fake.promptModels)
	}
	fake.mu.Unlock()
	var savedSession string
	if err := database.QueryRow(`SELECT session_id FROM chat WHERE id=?`, chatID).Scan(&savedSession); err != nil || savedSession != firstSession {
		t.Fatalf("saved session = %q, %v", savedSession, err)
	}

	fake.publish(t, map[string]any{"type": "message.updated", "properties": map[string]any{"sessionID": firstSession, "info": map[string]any{"id": "assistant-1", "role": "assistant"}}})
	fake.publish(t, map[string]any{"type": "message.part.updated", "properties": map[string]any{"part": map[string]any{"id": "text-1", "sessionID": firstSession, "messageID": "assistant-1", "type": "text", "text": "Checking:", "time": map[string]any{"start": 1}}, "delta": "Checking:"}})
	fake.publish(t, map[string]any{"type": "message.part.updated", "properties": map[string]any{"part": map[string]any{"id": "reasoning-1", "sessionID": firstSession, "messageID": "assistant-1", "type": "reasoning", "text": "hidden reasoning", "time": map[string]any{"start": 1}}, "delta": "hidden reasoning"}})
	fake.publish(t, map[string]any{"type": "message.updated", "properties": map[string]any{"sessionID": firstSession, "info": map[string]any{"id": "user-1", "role": "user"}}})
	fake.publish(t, map[string]any{"type": "message.part.updated", "properties": map[string]any{"sessionID": firstSession, "part": map[string]any{"id": "user-text", "messageID": "user-1", "type": "text"}}})
	fake.publish(t, map[string]any{"type": "message.part.delta", "properties": map[string]any{"sessionID": firstSession, "messageID": "user-1", "partID": "user-text", "field": "text", "delta": "echo must not appear"}})
	fake.publish(t, map[string]any{"type": "message.part.updated", "properties": map[string]any{
		"sessionID": firstSession, "part": map[string]any{"id": "tool-1", "messageID": "assistant-1", "type": "tool", "tool": "write", "state": map[string]any{"status": "completed", "input": map[string]any{"path": "hello.txt"}}},
	}})
	fake.publish(t, map[string]any{"type": "message.part.updated", "properties": map[string]any{"part": map[string]any{"id": "text-2", "sessionID": firstSession, "messageID": "assistant-1", "type": "text", "text": "Done.", "time": map[string]any{"start": 2}}, "delta": "Done."}})
	fake.publish(t, map[string]any{"type": "session.idle", "properties": map[string]any{"sessionID": firstSession}})
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	var assistant, tool string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='assistant' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&assistant); err != nil || assistant != "Checking:\n\nDone." {
		t.Fatalf("assistant stored as %q, %v", assistant, err)
	}
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='tool' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&tool); err != nil || tool != `write {"path":"hello.txt"}` {
		t.Fatalf("tool summary = %q, %v", tool, err)
	}
	var thinking string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='thinking' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&thinking); err != nil || thinking != "hidden reasoning" {
		t.Fatalf("thinking = %q, %v", thinking, err)
	}
	emittedMu.Lock()
	var deltaCount, toolCount, doneCount int
	var streamed strings.Builder
	for _, event := range emitted {
		switch event["type"] {
		case "delta":
			deltaCount++
			streamed.WriteString(event["text"].(string))
		case "tool":
			toolCount++
		case "done":
			doneCount++
		}
	}
	emittedMu.Unlock()
	if deltaCount != 2 || toolCount != 1 || doneCount != 1 {
		t.Fatalf("stream events delta=%d tool=%d done=%d", deltaCount, toolCount, doneCount)
	}
	if got := streamed.String(); got != "Checking:\n\nDone." {
		t.Fatalf("streamed assistant = %q", got)
	}
	fake.publish(t, map[string]any{"type": "session.status", "properties": map[string]any{"sessionID": firstSession, "status": map[string]string{"type": "busy"}}})
	waitFor(t, func() bool { return server.chats.Running(chatID) })
	fake.publish(t, map[string]any{"type": "message.updated", "properties": map[string]any{"sessionID": firstSession, "info": map[string]any{"id": "late-assistant", "role": "assistant"}}})
	fake.publish(t, map[string]any{"type": "message.part.updated", "properties": map[string]any{"sessionID": firstSession, "part": map[string]any{"id": "late-text", "messageID": "late-assistant", "type": "text"}}})
	fake.publish(t, map[string]any{"type": "message.part.delta", "properties": map[string]any{"sessionID": firstSession, "messageID": "late-assistant", "partID": "late-text", "field": "text", "delta": "late reply"}})
	fake.publish(t, map[string]any{"type": "session.idle", "properties": map[string]any{"sessionID": firstSession}})
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='assistant' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&assistant); err != nil || assistant != "late reply" {
		t.Fatalf("autonomous v1 assistant = %q, %v", assistant, err)
	}
	fake.mu.Lock()
	for _, prompt := range fake.prompts {
		if strings.Contains(prompt, "echo must not appear") {
			t.Fatal("user prompt echoed as assistant delta")
		}
	}
	fake.mu.Unlock()

	for _, answer := range []struct {
		id       string
		allow    bool
		response string
	}{{"per-once", true, "once"}, {"per-reject", false, "reject"}} {
		if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "permission test"}, nil); code != http.StatusAccepted {
			t.Fatalf("permission turn send status = %d", code)
		}
		fake.publish(t, map[string]any{"type": "permission.asked", "properties": map[string]any{
			"sessionID": firstSession, "id": answer.id, "permission": "edit", "patterns": []string{"hello.txt"}, "metadata": map[string]any{"filepath": "hello.txt"}, "always": []string{"*"}, "tool": map[string]any{"messageID": "assistant-1", "callID": "call-1"},
		}})
		approvalID := fake.waitApproval(t, database, chatID)
		if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": answer.allow}, nil); code != http.StatusOK {
			t.Fatalf("answer approval status = %d", code)
		}
		fake.waitPermission(t, answer.id, answer.response)
		var toolName, approvalInput string
		if err := database.QueryRow(`SELECT tool_name,input_json FROM chat_approval WHERE id=?`, approvalID).Scan(&toolName, &approvalInput); err != nil || toolName != "edit" || !strings.Contains(approvalInput, "hello.txt") {
			t.Fatalf("stored approval = (%q, %q), %v", toolName, approvalInput, err)
		}
		fake.publish(t, map[string]any{"type": "session.status", "properties": map[string]any{"sessionID": firstSession, "status": map[string]any{"type": "idle"}}})
		waitFor(t, func() bool { return !server.chats.Running(chatID) })
	}

	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "abort me"}, nil); code != http.StatusAccepted {
		t.Fatalf("abort turn send status = %d", code)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/stop", nil, nil); code != http.StatusNoContent {
		t.Fatalf("stop status = %d", code)
	}
	fake.mu.Lock()
	if fake.aborts != 1 {
		t.Fatalf("abort calls = %d", fake.aborts)
	}
	fake.mu.Unlock()
	if err := server.chats.CloseChat(reusedID); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	if fake.kills != 1 {
		t.Fatalf("shared server process kills = %d; want 1 after last chat closes", fake.kills)
	}
	fake.mu.Unlock()
}

func TestOpenCodeV1ParentLookupRetriesAfterFailure(t *testing.T) {
	var requests atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"id":"child","parentID":"parent"}`))
	}))
	defer httpServer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	parent := &opencodeRuntime{chatID: "chat", directory: "."}
	s := &opencodeServer{major: 1, baseURL: httpServer.URL, client: httpServer.Client(), ctx: ctx, chats: map[string]*opencodeRuntime{"parent": parent}, ancestors: make(map[string]string)}
	if driver, _ := s.driverForSession("child"); driver != nil {
		t.Fatal("failed lookup unexpectedly routed")
	}
	driver, child := s.driverForSession("child")
	if driver != parent || !child {
		t.Fatalf("driver=%p child=%v", driver, child)
	}
}

func TestOpenCodeV1SessionCreatedMapsChildToParent(t *testing.T) {
	parent := &opencodeRuntime{chatID: "chat"}
	s := &opencodeServer{major: 1, chats: map[string]*opencodeRuntime{"parent": parent}, ancestors: make(map[string]string)}
	event, err := json.Marshal(map[string]any{"type": "session.created", "properties": map[string]any{"info": map[string]string{"id": "child", "parentID": "parent"}}})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(event, &raw); err != nil {
		t.Fatal(err)
	}
	if err := s.dispatch(raw); err != nil {
		t.Fatal(err)
	}
	driver, child := s.driverForSession("child")
	if driver != parent || !child {
		t.Fatalf("driver=%p child=%v", driver, child)
	}
}

type openCodeFake struct {
	t              *testing.T
	mu             sync.Mutex
	events         chan []byte
	password       string
	sessions       int
	serverStarts   int
	eventStreams   int
	kills          int
	aborts         int
	directories    []string
	prompts        []string
	promptSessions []string
	promptModels   []string
	permissions    map[string]string
	permissionDone chan string
}

func newOpenCodeFake(t *testing.T) *openCodeFake {
	return &openCodeFake{t: t, events: make(chan []byte, 64), permissions: make(map[string]string), permissionDone: make(chan string, 4)}
}

func (f *openCodeFake) start(args, env []string) (opencodeProcess, error) {
	var port string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--port" {
			port = args[i+1]
		}
	}
	var configRaw string
	for _, item := range env {
		if strings.HasPrefix(item, "OPENCODE_SERVER_PASSWORD=") {
			f.password = strings.TrimPrefix(item, "OPENCODE_SERVER_PASSWORD=")
		}
		if strings.HasPrefix(item, "OPENCODE_CONFIG_CONTENT=") {
			configRaw = strings.TrimPrefix(item, "OPENCODE_CONFIG_CONTENT=")
		}
	}
	secret, decodeErr := hex.DecodeString(f.password)
	var config struct {
		Permission map[string]string `json:"permission"`
		MCP        map[string]struct {
			Type        string            `json:"type"`
			Command     []string          `json:"command"`
			Environment map[string]string `json:"environment"`
			URL         string            `json:"url"`
			Headers     map[string]string `json:"headers"`
			Enabled     bool              `json:"enabled"`
		} `json:"mcp"`
	}
	if port == "" || decodeErr != nil || len(secret) != 32 || json.Unmarshal([]byte(configRaw), &config) != nil {
		return nil, fmt.Errorf("missing port or server password")
	}
	if config.Permission["*"] != "ask" || config.Permission["edit"] != "ask" || config.Permission["bash"] != "ask" || config.Permission["webfetch"] != "ask" {
		return nil, fmt.Errorf("unexpected permission config: %#v", config.Permission)
	}
	mcp := config.MCP["patio"]
	if len(config.MCP) != 3 || config.MCP["disabled-test"].Type != "" || mcp.Type != "local" || !mcp.Enabled || len(mcp.Command) != 4 || mcp.Command[1] != "mcp" || mcp.Command[2] != "-d" || mcp.Command[3] == "" {
		return nil, fmt.Errorf("unexpected ticket MCP config: %#v", mcp)
	}
	stdio := config.MCP["stdio-test"]
	if stdio.Type != "local" || !reflect.DeepEqual(stdio.Command[1:], []string{"--fixture"}) || stdio.Environment["TOKEN"] != "stdio-secret" {
		return nil, fmt.Errorf("unexpected external stdio MCP config: %#v", stdio)
	}
	httpServer := config.MCP["http-test"]
	if httpServer.Type != "remote" || httpServer.URL != "https://mcp.example.test/mcp" || httpServer.Headers["Authorization"] != "Bearer http-secret" {
		return nil, fmt.Errorf("unexpected external HTTP MCP config: %#v", httpServer)
	}
	f.mu.Lock()
	f.serverStarts++
	f.mu.Unlock()
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/doc", func(w http.ResponseWriter, r *http.Request) {
		if f.auth(w, r) {
			w.WriteHeader(http.StatusOK)
		}
	})
	mux.HandleFunc("/global/event", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		f.mu.Lock()
		f.eventStreams++
		f.mu.Unlock()
		flusher := w.(http.Flusher)
		flusher.Flush()
		for {
			select {
			case data := <-f.events:
				payload, err := json.Marshal(map[string]any{"directory": "/tmp", "payload": json.RawMessage(data)})
				if err != nil {
					f.t.Errorf("encode global event: %v", err)
					return
				}
				_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.recordDirectory(r)
		f.mu.Lock()
		f.sessions++
		id := fmt.Sprintf("session-%d", f.sessions)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
	})
	mux.HandleFunc("/session/", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.recordDirectory(r)
		path := strings.TrimPrefix(r.URL.Path, "/session/")
		if strings.HasSuffix(path, "/prompt_async") {
			var body struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
				Model struct {
					Provider string `json:"providerID"`
					Model    string `json:"modelID"`
				} `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				f.t.Errorf("decode prompt body: %v", err)
			}
			f.mu.Lock()
			f.promptSessions = append(f.promptSessions, strings.TrimSuffix(path, "/prompt_async"))
			if len(body.Parts) > 0 {
				f.prompts = append(f.prompts, body.Parts[0].Text)
			}
			if body.Model.Provider != "" {
				f.promptModels = append(f.promptModels, body.Model.Provider+"/"+body.Model.Model)
			}
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.HasSuffix(path, "/abort") {
			f.mu.Lock()
			f.aborts++
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.Contains(path, "/permissions/") {
			var body struct {
				Response string `json:"response"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				f.t.Errorf("decode permission response: %v", err)
			}
			permissionID := path[strings.LastIndex(path, "/")+1:]
			f.mu.Lock()
			f.permissions[permissionID] = body.Response
			f.mu.Unlock()
			f.permissionDone <- permissionID
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
	})}
	proc := &openCodeFakeProcess{server: srv, done: make(chan struct{}), fake: f}
	go func() {
		defer close(proc.done)
		_ = srv.Serve(listener)
	}()
	return proc, nil
}

func (f *openCodeFake) auth(w http.ResponseWriter, r *http.Request) bool {
	user, password, ok := r.BasicAuth()
	if !ok || user != "opencode" || password != f.password {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (f *openCodeFake) recordDirectory(r *http.Request) {
	f.mu.Lock()
	f.directories = append(f.directories, r.URL.Query().Get("directory"))
	f.mu.Unlock()
}

func (f *openCodeFake) publish(t *testing.T, event map[string]any) {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case f.events <- data:
	case <-time.After(time.Second):
		t.Fatal("SSE event not consumed")
	}
}

func (f *openCodeFake) waitSession(t *testing.T, count int) string {
	t.Helper()
	waitFor(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.sessions > count
	})
	f.mu.Lock()
	defer f.mu.Unlock()
	return fmt.Sprintf("session-%d", f.sessions)
}

func (f *openCodeFake) waitEvents(t *testing.T) {
	t.Helper()
	waitFor(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.eventStreams > 0
	})
}

func (f *openCodeFake) waitApproval(t *testing.T, database *sql.DB, chatID string) string {
	t.Helper()
	var id string
	waitFor(t, func() bool {
		return database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&id) == nil
	})
	return id
}

func (f *openCodeFake) waitPermission(t *testing.T, id, response string) {
	t.Helper()
	select {
	case got := <-f.permissionDone:
		if got != id {
			t.Fatalf("permission id = %q; want %q", got, id)
		}
	case <-time.After(time.Second):
		t.Fatal("permission response not received")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.permissions[id] != response {
		t.Fatalf("permission response = %q; want %q", f.permissions[id], response)
	}
}

type openCodeFakeProcess struct {
	server *http.Server
	done   chan struct{}
	fake   *openCodeFake
	once   sync.Once
}

func (p *openCodeFakeProcess) Kill() error {
	p.once.Do(func() {
		_ = p.server.Close()
		<-p.done
		p.fake.mu.Lock()
		p.fake.kills++
		p.fake.mu.Unlock()
	})
	return nil
}

func (p *openCodeFakeProcess) Wait() error { <-p.done; return nil }

func TestOpenCodeV2ChatAndMissingSessionFallback(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	if _, err := database.Exec(`UPDATE connector SET kind='opencode' WHERE id=?`, connectorID); err != nil {
		t.Fatal(err)
	}
	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET model='opencode/big-pickle' WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE chat SET show_thinking=1 WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	resumeID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET session_id='missing-session',model='opencode/big-pickle' WHERE id=?`, resumeID); err != nil {
		t.Fatal(err)
	}
	fake := newOpenCodeV2Fake(t)
	var versionChecks atomic.Int32
	server := New(database, "local", false, "dev", root, Options{
		DBPath:       filepath.Join(root, "test.db"),
		findOpencode: func() (string, error) { return "opencode-v2-test", nil },
		opencodeMajor: func(context.Context, string, []string) (int, error) {
			versionChecks.Add(1)
			return 2, nil
		},
		opencodeStarter: func(_ string, args, env []string) (opencodeProcess, error) {
			return fake.start(args, env)
		},
	})
	addChatMCPTestServers(t, server)
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})

	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "stream"}, nil); code != http.StatusAccepted {
		t.Fatalf("new v2 chat send status = %d", code)
	}
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	var assistant, tool string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='assistant' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&assistant); err != nil || assistant != "Hello there" {
		t.Fatalf("v2 assistant = %q, %v", assistant, err)
	}
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='tool' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&tool); err != nil || tool != `write {"path":"hello.txt"}` {
		t.Fatalf("v2 tool summary = %q, %v", tool, err)
	}
	var thinking string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='thinking' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&thinking); err != nil || thinking != "Reason briefly. " {
		t.Fatalf("v2 thinking = %q, %v", thinking, err)
	}

	for _, answer := range []struct {
		text  string
		allow bool
		want  string
	}{{"permission-once", true, "once"}, {"permission-reject", false, "reject"}} {
		if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": answer.text}, nil); code != http.StatusAccepted {
			t.Fatalf("v2 permission send status = %d", code)
		}
		approvalID := fake.waitApproval(t, database, chatID)
		if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": answer.allow}, nil); code != http.StatusOK {
			t.Fatalf("v2 approval answer status = %d", code)
		}
		fake.waitPermission(t, answer.want)
		waitFor(t, func() bool { return !server.chats.Running(chatID) })
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "interrupt"}, nil); code != http.StatusAccepted {
		t.Fatalf("subagent turn send status = %d", code)
	}
	fake.publish(map[string]any{"type": "permission.asked", "data": map[string]any{"sessionID": "child-session", "id": "child-permission", "action": "shell"}})
	approvalID := fake.waitApproval(t, database, chatID)
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": true}, nil); code != http.StatusOK {
		t.Fatalf("child approval status = %d", code)
	}
	fake.waitPermission(t, "once")
	fake.publish(map[string]any{"type": "session.text.delta", "data": map[string]any{"sessionID": "child-session", "delta": "child leak"}})
	fake.publish(map[string]any{"type": "session.execution.succeeded", "data": map[string]string{"sessionID": "child-session"}})
	time.Sleep(50 * time.Millisecond)
	if !server.chats.Running(chatID) {
		t.Fatal("child completion finished parent turn")
	}
	fake.mu.Lock()
	if fake.permissionSessions["child-permission"] != "child-session" {
		t.Fatalf("permission replied to %q", fake.permissionSessions["child-permission"])
	}
	fake.mu.Unlock()
	fake.publish(map[string]any{"type": "session.execution.succeeded", "data": map[string]string{"sessionID": "ses-v2-1"}})
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	var parentText string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='assistant' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&parentText); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(parentText, "child leak") {
		t.Fatalf("child text leaked into parent reply: %q", parentText)
	}

	// A background subagent can trigger a new parent execution after prior idle.
	fake.publish(map[string]any{"type": "session.execution.started", "data": map[string]string{"sessionID": "ses-v2-1"}})
	waitFor(t, func() bool { return server.chats.Running(chatID) })
	fake.publish(map[string]any{"type": "session.text.delta", "data": map[string]string{"sessionID": "ses-v2-1", "delta": "late reply"}})
	fake.publish(map[string]any{"type": "session.execution.succeeded", "data": map[string]string{"sessionID": "ses-v2-1"}})
	waitFor(t, func() bool { return !server.chats.Running(chatID) })
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='assistant' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&parentText); err != nil || parentText != "late reply" {
		t.Fatalf("autonomous v2 assistant = %q, %v", parentText, err)
	}

	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+resumeID+"/messages", map[string]string{"text": "resume prompt"}, nil); code != http.StatusAccepted {
		t.Fatalf("v2 resumed chat send status = %d", code)
	}
	waitFor(t, func() bool { return !server.chats.Running(resumeID) })
	var savedSession string
	if err := database.QueryRow(`SELECT session_id FROM chat WHERE id=?`, resumeID).Scan(&savedSession); err != nil || savedSession != "ses-v2-2" {
		t.Fatalf("fallback session = %q, %v", savedSession, err)
	}
	var noteCount int
	if err := database.QueryRow(`SELECT count(*) FROM chat_message WHERE chat_id=? AND role='error' AND content=?`, resumeID, "opencode was upgraded; earlier context was not carried over.").Scan(&noteCount); err != nil || noteCount != 1 {
		t.Fatalf("upgrade note count = %d, %v", noteCount, err)
	}
	var offThinkingCount int
	if err := database.QueryRow(`SELECT count(*) FROM chat_message WHERE chat_id=? AND role='thinking'`, resumeID).Scan(&offThinkingCount); err != nil || offThinkingCount != 0 {
		t.Fatalf("thinking rows while disabled = %d, %v", offThinkingCount, err)
	}

	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "interrupt"}, nil); code != http.StatusAccepted {
		t.Fatalf("v2 interrupt send status = %d", code)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/stop", nil, nil); code != http.StatusNoContent {
		t.Fatalf("v2 interrupt status = %d", code)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.creates != 2 || !slices.Contains(fake.promptSessions, "missing-session") || fake.interrupts != 1 {
		t.Fatalf("v2 creates=%d prompt sessions=%v interrupts=%d", fake.creates, fake.promptSessions, fake.interrupts)
	}
	if fake.model != "opencode/big-pickle" || fake.directory == "" || !fake.configParsed {
		t.Fatalf("v2 create model=%q directory=%q config parsed=%v", fake.model, fake.directory, fake.configParsed)
	}
	if got := versionChecks.Load(); got != 1 {
		t.Fatalf("version checks = %d; want one per server start", got)
	}
}

type openCodeV2Fake struct {
	t                  *testing.T
	mu                 sync.Mutex
	events             chan []byte
	password           string
	creates            int
	directory          string
	model              string
	configParsed       bool
	interrupts         int
	promptSessions     []string
	permissions        map[string]string
	permissionSessions map[string]string
	permissionDone     chan string
}

func newOpenCodeV2Fake(t *testing.T) *openCodeV2Fake {
	return &openCodeV2Fake{t: t, events: make(chan []byte, 64), permissions: make(map[string]string), permissionSessions: make(map[string]string), permissionDone: make(chan string, 4)}
}

func (f *openCodeV2Fake) start(args, env []string) (opencodeProcess, error) {
	var port, configRaw string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--port" {
			port = args[i+1]
		}
	}
	for _, value := range env {
		switch {
		case strings.HasPrefix(value, "OPENCODE_SERVER_PASSWORD="):
			f.password = strings.TrimPrefix(value, "OPENCODE_SERVER_PASSWORD=")
		case strings.HasPrefix(value, "OPENCODE_CONFIG_CONTENT="):
			configRaw = strings.TrimPrefix(value, "OPENCODE_CONFIG_CONTENT=")
		}
	}
	var config struct {
		Permissions []struct {
			Action   string `json:"action"`
			Resource string `json:"resource"`
			Effect   string `json:"effect"`
		} `json:"permissions"`
		MCP struct {
			Servers map[string]struct {
				Type     string   `json:"type"`
				Command  []string `json:"command"`
				Disabled *bool    `json:"disabled"`
			} `json:"servers"`
		} `json:"mcp"`
	}
	if port == "" || json.Unmarshal([]byte(configRaw), &config) != nil {
		return nil, fmt.Errorf("invalid v2 config or missing port")
	}
	wantPermissions := map[string]bool{"edit": false, "shell": false, "webfetch": false}
	for _, rule := range config.Permissions {
		if rule.Resource == "*" && rule.Effect == "ask" {
			if _, ok := wantPermissions[rule.Action]; ok {
				wantPermissions[rule.Action] = true
			}
		}
	}
	ticket := config.MCP.Servers["patio"]
	if len(config.MCP.Servers) != 3 || !wantPermissions["edit"] || !wantPermissions["shell"] || !wantPermissions["webfetch"] || ticket.Type != "local" || ticket.Disabled == nil || *ticket.Disabled || len(ticket.Command) != 4 {
		return nil, fmt.Errorf("unexpected v2 opencode config: %#v", config)
	}
	f.configParsed = true
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/info", func(w http.ResponseWriter, r *http.Request) {
		if f.auth(w, r) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"2.0.26"}`))
		}
	})
	mux.HandleFunc("/api/event", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		for {
			select {
			case event := <-f.events:
				_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		var body struct {
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
			Model struct {
				ProviderID string `json:"providerID"`
				ID         string `json:"id"`
			} `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("decode v2 create body: %v", err)
		}
		f.mu.Lock()
		f.creates++
		f.directory = body.Location.Directory
		f.model = body.Model.ProviderID + "/" + body.Model.ID
		id := fmt.Sprintf("ses-v2-%d", f.creates)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": id}})
	})
	mux.HandleFunc("/api/session/", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/session/"), "/")
		sid := parts[0]
		if r.Method == http.MethodGet && len(parts) == 1 {
			parent := ""
			if sid == "child-session" {
				parent = "ses-v2-1"
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": sid, "parentID": parent}})
			return
		}
		if len(parts) == 2 && parts[1] == "prompt" {
			var body struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				f.t.Errorf("decode v2 prompt body: %v", err)
			}
			f.mu.Lock()
			f.promptSessions = append(f.promptSessions, sid)
			f.mu.Unlock()
			if sid == "missing-session" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"_tag":"SessionNotFoundError","message":"Session not found"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"msg-test"}}`))
			fixture := "opencode-v2-chat.sse"
			switch body.Text {
			case "permission-once":
				fixture = "opencode-v2-permission-once.sse"
			case "permission-reject":
				fixture = "opencode-v2-permission-reject.sse"
			case "interrupt":
				return
			}
			f.publishFixture(fixture, sid)
			return
		}
		if len(parts) == 4 && parts[1] == "permission" && parts[3] == "reply" {
			var body struct {
				Decision string `json:"decision"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				f.t.Errorf("decode v2 permission reply: %v", err)
			}
			f.mu.Lock()
			f.permissions[parts[2]] = body.Decision
			f.permissionSessions[parts[2]] = sid
			f.mu.Unlock()
			f.permissionDone <- parts[2]
			typeName := "session.execution.succeeded"
			if body.Decision == "reject" {
				typeName = "session.execution.interrupted"
			}
			f.publish(map[string]any{"type": typeName, "data": map[string]string{"sessionID": sid}})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if len(parts) == 2 && parts[1] == "interrupt" {
			if r.URL.Query().Get("resume") != "false" {
				f.t.Errorf("v2 interrupt resume query = %q", r.URL.Query().Get("resume"))
			}
			f.mu.Lock()
			f.interrupts++
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})
	srv := &http.Server{Handler: mux}
	proc := &openCodeV2FakeProcess{server: srv, done: make(chan struct{}), fake: f}
	go func() {
		defer close(proc.done)
		_ = srv.Serve(listener)
	}()
	return proc, nil
}

func (f *openCodeV2Fake) auth(w http.ResponseWriter, r *http.Request) bool {
	user, password, ok := r.BasicAuth()
	if !ok || user != "opencode" || password != f.password {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (f *openCodeV2Fake) publish(event any) {
	data, err := json.Marshal(event)
	if err != nil {
		f.t.Errorf("encode v2 event: %v", err)
		return
	}
	f.events <- data
}

func (f *openCodeV2Fake) publishFixture(name, sessionID string) {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		f.t.Errorf("read v2 fixture %s: %v", name, err)
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		line = strings.ReplaceAll(strings.TrimPrefix(line, "data: "), "<SESSION>", sessionID)
		line = strings.ReplaceAll(line, "<MESSAGE>", "msg-fixture")
		line = strings.ReplaceAll(line, "<CALL>", "call-fixture")
		f.events <- []byte(line)
	}
}

func (f *openCodeV2Fake) waitApproval(t *testing.T, database *sql.DB, chatID string) string {
	t.Helper()
	var id string
	waitFor(t, func() bool {
		return database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&id) == nil
	})
	return id
}

func (f *openCodeV2Fake) waitPermission(t *testing.T, want string) {
	t.Helper()
	select {
	case id := <-f.permissionDone:
		f.mu.Lock()
		defer f.mu.Unlock()
		if got := f.permissions[id]; got != want {
			t.Fatalf("v2 permission decision = %q; want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("v2 permission reply not received")
	}
}

type openCodeV2FakeProcess struct {
	server *http.Server
	done   chan struct{}
	fake   *openCodeV2Fake
	once   sync.Once
}

func (p *openCodeV2FakeProcess) Kill() error {
	p.once.Do(func() {
		_ = p.server.Close()
		<-p.done
	})
	return nil
}

func (p *openCodeV2FakeProcess) Wait() error { <-p.done; return nil }
