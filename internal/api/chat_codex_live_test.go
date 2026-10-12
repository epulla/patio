package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	connectorcli "patio/internal/connector"
)

func TestCodexLive(t *testing.T) {
	if os.Getenv("PATIO_CODEX_LIVE") != "1" {
		t.Skip("set PATIO_CODEX_LIVE=1 for logged-in Codex checks")
	}
	binary := os.Getenv("PATIO_BINARY")
	if binary == "" {
		t.Fatal("PATIO_BINARY is required")
	}
	codexPath, err := connectorcli.Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	database, _, root := chatAPI(t)
	connectorID := createCodexTestConnector(t, database)
	fixturePath := os.Getenv("PATIO_CODEX_FIXTURE")
	var fixture *os.File
	if fixturePath != "" {
		fixture, err = os.Create(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		defer fixture.Close()
	}
	var fixtureMu sync.Mutex
	var fallbackCalls int
	events := make(chan map[string]any, 64)
	server := New(database, "local", false, "live", root, Options{
		DBPath:        filepath.Join(root, "test.db"),
		Emit:          func(_ string, data any) { events <- data.(map[string]any) },
		findCodex:     func() (string, error) { return codexPath, nil },
		mcpExecutable: binary,
		codexStart: func(ctx context.Context, path string, env []string, isolated bool, version, cwd string, overrides ...string) (codexClient, error) {
			if len(overrides) != 0 {
				fallbackCalls++
			}
			client, err := connectorcli.StartCodexInDir(ctx, path, env, isolated, version, cwd, overrides...)
			if err != nil {
				return nil, err
			}
			return &recordingCodexClient{Client: client, fixture: fixture, fixtureMu: &fixtureMu}, nil
		},
	})
	server.chats.approvalPollInterval = 100 * time.Millisecond
	defer server.Close()

	if os.Getenv("PATIO_CODEX_SKIP_REPLY") != "1" {
		firstChat := liveCodexChat(t, database, connectorID, root)
		liveCodexPrompt(t, server, firstChat, "Reply with just OK", nil, events)
		var session string
		if err := database.QueryRow(`SELECT session_id FROM chat WHERE id=?`, firstChat).Scan(&session); err != nil || session == "" {
			t.Fatalf("session id = %q, %v", session, err)
		}
	}

	allowedChat := liveCodexChat(t, database, connectorID, root)
	allowedPath := filepath.Join(root, "chats", allowedChat, "hello.txt")
	allowedApproval := liveCodexPrompt(t, server, allowedChat, "Create a file hello.txt containing hi", boolPointer(true), events)
	t.Logf("per-thread MCP config fallback launches: %d; allowed prompt requested approval: %t", fallbackCalls, allowedApproval)
	if err := server.chats.CloseChat(allowedChat); err != nil {
		t.Fatal(err)
	}
	resumed := loadLiveChat(t, database, allowedChat)
	runtime := server.chats.runtime(allowedChat)
	runtime.connectorID = connectorID
	resumed.Workdir, err = server.chats.workdir(allowedChat)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := startCodex(server.chats, resumed, "", runtime)
	if err != nil {
		t.Fatalf("resume Codex thread without prompt: %v", err)
	}
	if err := driver.close(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(allowedPath); err != nil || !strings.Contains(string(data), "hi") {
		t.Fatalf("allowed file content = %q, %v", data, err)
	}

	deniedChat := liveCodexChat(t, database, connectorID, root)
	deniedPath := filepath.Join(root, "chats", deniedChat, "hello.txt")
	deniedApproval := liveCodexPrompt(t, server, deniedChat, "Create a file hello.txt containing hi", boolPointer(false), events)
	if !deniedApproval {
		t.Fatal("Codex completed workspace file write without requesting approval")
	}
	if _, err := os.Stat(deniedPath); !os.IsNotExist(err) {
		t.Fatalf("denied file exists or stat failed: %v", err)
	}
	if fixture != nil {
		if err := fixture.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("per-thread MCP config fallback launches: %d", fallbackCalls)
}

type recordingCodexClient struct {
	*connectorcli.Client
	fixture   *os.File
	fixtureMu *sync.Mutex
}

func (c *recordingCodexClient) Notifications() <-chan connectorcli.Notification {
	out := make(chan connectorcli.Notification, 16)
	go func() {
		defer close(out)
		for notification := range c.Client.Notifications() {
			c.record(map[string]any{"method": notification.Method, "params": redactCodexFixture(notification.Params)})
			out <- notification
		}
	}()
	return out
}

func (c *recordingCodexClient) Requests() <-chan connectorcli.Request {
	out := make(chan connectorcli.Request, 16)
	go func() {
		defer close(out)
		for request := range c.Client.Requests() {
			c.record(map[string]any{"id": "request-id", "method": request.Method, "params": redactCodexFixture(request.Params)})
			out <- request
		}
	}()
	return out
}

func (c *recordingCodexClient) record(value any) {
	if c.fixture == nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	c.fixtureMu.Lock()
	_, _ = c.fixture.Write(append(data, '\n'))
	c.fixtureMu.Unlock()
}

func redactCodexFixture(raw json.RawMessage) any {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return redactCodexValue(value)
}

func redactCodexValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "email") {
				value[key] = "[redacted]"
			} else if lower == "cwd" || lower == "path" || strings.Contains(lower, "workspace") || lower == "servername" {
				value[key] = "[workdir]"
			} else if lower == "id" || strings.HasSuffix(lower, "id") {
				value[key] = "[id]"
			} else {
				value[key] = redactCodexValue(item)
			}
		}
		return value
	case []any:
		for index := range value {
			value[index] = redactCodexValue(value[index])
		}
		return value
	default:
		return value
	}
}

func liveCodexChat(t *testing.T, database *sql.DB, connectorID, root string) string {
	t.Helper()
	return createDriverTestChat(t, database, connectorID, root)
}

func loadLiveChat(t *testing.T, database *sql.DB, id string) chat {
	t.Helper()
	var c chat
	if err := database.QueryRow(`SELECT id,connector_id,title,model,folder,session_id,created_at,updated_at FROM chat WHERE id=?`, id).Scan(&c.ID, &c.ConnectorID, &c.Title, &c.Model, &c.Folder, &c.SessionID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	return c
}

func liveCodexPrompt(t *testing.T, server *Server, chatID, text string, allow *bool, events <-chan map[string]any) bool {
	t.Helper()
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": text}, nil); code != http.StatusAccepted {
		t.Fatalf("send %q status = %d", text, code)
	}
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	requestedApproval := false
	for {
		select {
		case event := <-events:
			switch event["type"] {
			case "approval":
				requestedApproval = true
				if allow == nil {
					t.Fatalf("unexpected Codex approval for prompt %q", text)
				}
				var approvalID string
				waitFor(t, func() bool {
					return server.db.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&approvalID) == nil
				})
				if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": *allow}, nil); code != http.StatusOK {
					t.Fatalf("answer Codex approval = %d", code)
				}
			case "done":
				return requestedApproval
			}
		case <-deadline.C:
			t.Fatalf("Codex prompt timed out: %q", text)
		}
	}
}

func boolPointer(value bool) *bool { return &value }
