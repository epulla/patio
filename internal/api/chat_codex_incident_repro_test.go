package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	connectorcli "patio/internal/connector"
)

// This is deliberately a failing regression test for the incident: the first
// Codex thread start fails after the user row is written; retry then gets an
// accepted turn/start response but emits no turn notifications.
func TestCodexStartFailureLeavesMessageAndRetryCanHang(t *testing.T) {
	db, _, root := chatAPI(t)
	connectorID := createCodexTestConnector(t, db)
	chatID := createDriverTestChat(t, db, connectorID, root)
	if _, err := db.Exec(`UPDATE chat SET thinking=1 WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	starts := 0
	server := New(db, "local", false, "test", root, Options{
		findCodex: func() (string, error) { return "codex-test", nil },
		codexStart: func(context.Context, string, []string, bool, string, string, ...string) (codexClient, error) {
			mu.Lock()
			starts++
			n := starts
			mu.Unlock()
			return &incidentCodexClient{n: n, notifications: make(chan connectorcli.Notification)}, nil
		},
	})
	defer server.Close()
	first := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "first"}, nil)
	var errorsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='error' AND content LIKE '%active writer%'`, chatID).Scan(&errorsCount); err != nil {
		t.Fatal(err)
	}
	if first != http.StatusInternalServerError {
		t.Fatalf("first send=%d, want startup error", first)
	}
	if errorsCount != 1 {
		t.Fatalf("startup error rows=%d, want visible error row", errorsCount)
	}
	second := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "second"}, nil)
	if second != http.StatusAccepted {
		t.Fatalf("retry returned %d, want accepted turn/start", second)
	}
	time.Sleep(50 * time.Millisecond)
	if server.chats.Running(chatID) {
		t.Log("fake Codex accepted turn/start but emitted no completion; runtime remains busy (watchdog out of scope)")
	}
}

func TestAgentMessageStartFailureQueuesWithoutDuplicateUserRow(t *testing.T) {
	db, _, root := chatAPI(t)
	connector := createCodexTestConnector(t, db)
	target := createDriverTestChat(t, db, connector, root)
	sender := uid()
	if _, err := db.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES(?,?,?,?)`, sender, "Sender", now(), now()); err != nil {
		t.Fatal(err)
	}
	var starts int
	server := New(db, "local", false, "test", root, Options{findCodex: func() (string, error) { return "codex-test", nil }, codexStart: func(context.Context, string, []string, bool, string, string, ...string) (codexClient, error) {
		starts++
		return &incidentCodexClient{n: starts, notifications: make(chan connectorcli.Notification)}, nil
	}})
	defer server.Close()
	var result map[string]string
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+target+"/agent-messages", map[string]string{"from_chat_id": sender, "text": "raw agent text"}, &result); code != 200 || result["status"] != "queued" {
		t.Fatalf("agent delivery=%d %#v", code, result)
	}
	var users, errorsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='user'`, target).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='error'`, target).Scan(&errorsCount); err != nil {
		t.Fatal(err)
	}
	var from, text string
	if err := db.QueryRow(`SELECT from_chat_id,text FROM chat_queue WHERE chat_id=?`, target).Scan(&from, &text); err != nil {
		t.Fatal(err)
	}
	if users != 0 || errorsCount != 1 || from != sender || text != "raw agent text" {
		t.Fatalf("users=%d errors=%d queue=(%s,%q)", users, errorsCount, from, text)
	}
}

type incidentCodexClient struct {
	n             int
	notifications chan connectorcli.Notification
	once          sync.Once
}

func (c *incidentCodexClient) Call(_ context.Context, method string, _, out any) error {
	if method == "thread/start" && c.n < 3 {
		return errors.New("thread already has an active writer")
	}
	var value any
	switch method {
	case "thread/start":
		value = map[string]any{"thread": map[string]string{"id": "incident-thread"}}
	case "turn/start":
		value = map[string]any{"turn": map[string]string{"id": "accepted-without-events"}}
	default:
		return errors.New("unexpected Codex call: " + method)
	}
	data, _ := json.Marshal(value)
	return json.Unmarshal(data, out)
}
func (*incidentCodexClient) Models(context.Context) ([]connectorcli.Model, error) { return nil, nil }
func (c *incidentCodexClient) Notifications() <-chan connectorcli.Notification {
	return c.notifications
}
func (*incidentCodexClient) Requests() <-chan connectorcli.Request           { return nil }
func (*incidentCodexClient) Respond(json.RawMessage, any) error              { return nil }
func (*incidentCodexClient) RespondError(json.RawMessage, int, string) error { return nil }
func (c *incidentCodexClient) Close() error                                  { c.once.Do(func() { close(c.notifications) }); return nil }
