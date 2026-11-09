package api

import (
	"net/http"
	"testing"
	"time"
)

func TestChatDraftRoundTripAndDelete(t *testing.T) {
	database, handler := newAPI(t)
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES('draft-chat','Draft','now','now')`); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"text": "unsent", "attachments": []any{map[string]any{"name": "file.txt", "mime": "text/plain"}}}
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/draft-chat/draft", body, nil); code != http.StatusNoContent {
		t.Fatalf("PUT draft = %d", code)
	}
	var got struct {
		Text        string           `json:"text"`
		Attachments []map[string]any `json:"attachments"`
		UpdatedAt   string           `json:"updated_at"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats/draft-chat/draft", nil, &got); code != http.StatusOK || got.Text != "unsent" || len(got.Attachments) != 1 || got.UpdatedAt == "" {
		t.Fatalf("GET draft = %d, %#v", code, got)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/draft-chat/draft", map[string]any{"text": "", "attachments": []any{}}, nil); code != http.StatusNoContent {
		t.Fatalf("empty PUT = %d", code)
	}
	got.Text, got.UpdatedAt = "stale", "stale"
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats/draft-chat/draft", nil, &got); code != http.StatusOK || got.Text != "" || len(got.Attachments) != 0 || got.UpdatedAt != "" {
		t.Fatalf("GET deleted draft = %d, %#v", code, got)
	}
}

func TestPruneChatDrafts(t *testing.T) {
	database, _ := newAPI(t)
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES('old-chat','Old','now','now'),('fresh-chat','Fresh','now','now')`); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	if _, err := database.Exec(`INSERT INTO chat_draft(chat_id,text,attachments,updated_at) VALUES('old-chat','old','[]',?),('fresh-chat','fresh','[]',?)`, old, now()); err != nil {
		t.Fatal(err)
	}
	s := New(database, "local", false, "dev", t.TempDir())
	defer s.Close()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_draft`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("remaining drafts = %d, err=%v", count, err)
	}
	var id string
	if err := database.QueryRow(`SELECT chat_id FROM chat_draft`).Scan(&id); err != nil || id != "fresh-chat" {
		t.Fatalf("remaining draft = %q, err=%v", id, err)
	}
}
