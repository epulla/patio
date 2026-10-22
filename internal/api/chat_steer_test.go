package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	connectorcli "patio/internal/connector"
	"testing"
)

type steerTestDriver struct {
	steered []ChatSendRequest
}

func (*steerTestDriver) send(ChatSendRequest) error { return nil }
func (*steerTestDriver) stop() error                { return nil }
func (*steerTestDriver) close() error               { return nil }
func (*steerTestDriver) canSteer() bool             { return true }
func (d *steerTestDriver) steer(request ChatSendRequest) error {
	d.steered = append(d.steered, request)
	return nil
}

type unsteerTestDriver struct{}

func (*unsteerTestDriver) send(ChatSendRequest) error { return nil }
func (*unsteerTestDriver) stop() error                { return nil }
func (*unsteerTestDriver) close() error               { return nil }

type steerCodexClient struct {
	method string
	params map[string]any
}

func (c *steerCodexClient) Call(_ context.Context, method string, params, _ any) error {
	c.method = method
	data, _ := json.Marshal(params)
	return json.Unmarshal(data, &c.params)
}
func (*steerCodexClient) Models(context.Context) ([]connectorcli.Model, error) { return nil, nil }
func (*steerCodexClient) Notifications() <-chan connectorcli.Notification      { return nil }
func (*steerCodexClient) Requests() <-chan connectorcli.Request                { return nil }
func (*steerCodexClient) Respond(json.RawMessage, any) error                   { return nil }
func (*steerCodexClient) RespondError(json.RawMessage, int, string) error      { return nil }
func (*steerCodexClient) Close() error                                         { return nil }

func TestQueueSendSteersRunningTurn(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	server := New(database, "local", false, "dev", root, Options{})
	defer server.Close()
	steerer := &steerTestDriver{}
	rt := server.chats.runtime(chatID)
	rt.mu.Lock()
	rt.busy, rt.driver = true, steerer
	rt.mu.Unlock()
	for i, text := range []string{"first", "second"} {
		if _, err := database.Exec(`INSERT INTO chat_queue(id,chat_id,text,attachments,created_at) VALUES(?,?,?,'[]',?)`, string(rune('a'+i)), chatID, text, string(rune('1'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/queue/send", nil, nil); code != http.StatusAccepted {
		t.Fatalf("send-now status=%d", code)
	}
	var count int
	var content string
	if err := database.QueryRow(`SELECT COUNT(*),MAX(content) FROM chat_message WHERE chat_id=? AND role='user'`, chatID).Scan(&count, &content); err != nil || count != 1 || content != "first\n\nsecond" {
		t.Fatalf("user messages count=%d content=%q err=%v", count, content, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_queue WHERE chat_id=?`, chatID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("queue count=%d err=%v", count, err)
	}
	if len(steerer.steered) != 1 || steerer.steered[0].Text != "first\n\nsecond" || !server.chats.Running(chatID) {
		t.Fatalf("steered=%#v running=%v", steerer.steered, server.chats.Running(chatID))
	}
	if err := server.chats.finishTurn(chatID, "done", nil); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='assistant' AND content='done'`, chatID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("done count=%d err=%v", count, err)
	}
}

func TestQueueSendLeavesQueueWhenDriverCannotSteer(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET queue_held=1 WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	server := New(database, "local", false, "dev", root, Options{})
	defer server.Close()
	rt := server.chats.runtime(chatID)
	rt.mu.Lock()
	rt.busy, rt.driver = true, &unsteerTestDriver{}
	rt.mu.Unlock()
	if _, err := database.Exec(`INSERT INTO chat_queue(id,chat_id,text,attachments,created_at) VALUES('queued',?,'pending','[]','1')`, chatID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/queue/send", nil, nil); code != http.StatusConflict {
		t.Fatalf("send-now status=%d", code)
	}
	var count, held int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_queue WHERE chat_id=?`, chatID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("queue count=%d err=%v", count, err)
	}
	if err := database.QueryRow(`SELECT queue_held FROM chat WHERE id=?`, chatID).Scan(&held); err != nil || held != 1 {
		t.Fatalf("queue_held=%d err=%v", held, err)
	}
}

func TestCodexSteerUsesActiveTurnID(t *testing.T) {
	client := &steerCodexClient{}
	d := &codexRuntime{client: client, threadID: "thread", turnID: "active-turn", workdir: t.TempDir()}
	if err := d.steer(ChatSendRequest{Text: "queued batch"}); err != nil {
		t.Fatal(err)
	}
	if client.method != "turn/steer" || client.params["expectedTurnId"] != "active-turn" || client.params["threadId"] != "thread" {
		t.Fatalf("method=%q params=%#v", client.method, client.params)
	}
}

func TestOpenCodeV2SteerDelivery(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	d := &opencodeRuntime{
		manager: newChatManager(database, Options{}), chatID: chatID, directory: filepath.Dir(root), session: "session",
		server: &opencodeServer{major: 2, baseURL: server.URL, client: server.Client()},
	}
	if err := d.steer(ChatSendRequest{Text: "queued batch"}); err != nil {
		t.Fatal(err)
	}
	if body["delivery"] != "steer" || body["text"] != "queued batch" {
		t.Fatalf("body=%#v", body)
	}
}
