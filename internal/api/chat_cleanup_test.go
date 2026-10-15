package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestChatCleanup(t *testing.T) {
	database, _, root := chatAPI(t)
	server := New(database, "local", false, "dev", root, Options{WindowID: "current-window"})
	old := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02T15:04:05.000Z")
	addChat := func(created string, lastMessage string) string {
		t.Helper()
		id := uid()
		if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES(?,?,?,?)`, id, "chat", created, created); err != nil {
			t.Fatal(err)
		}
		if lastMessage != "" {
			if _, err := database.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at) VALUES(?,?,'user','hi',?)`, uid(), id, lastMessage); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(filepath.Join(root, "chats", id), 0o700); err != nil {
			t.Fatal(err)
		}
		return id
	}
	stale := addChat(old, old)
	empty := addChat(old, "")
	recent := addChat(old, now())
	pinned := addChat(old, old)
	running := addChat(old, old)
	rt := server.chats.runtime(running)
	rt.mu.Lock()
	rt.busy = true
	rt.mu.Unlock()

	cleanup := func() int {
		t.Helper()
		var out struct {
			Deleted int `json:"deleted"`
		}
		if code := requestJSON(t, server.handler, http.MethodPost, "/api/chats/cleanup", map[string][]string{"keep": {pinned}}, &out); code != http.StatusOK {
			t.Fatalf("cleanup = %d", code)
		}
		return out.Deleted
	}

	var settings chatCleanupSettings
	if code := requestJSON(t, server.handler, http.MethodGet, "/api/settings/chat-cleanup", nil, &settings); code != http.StatusOK || settings != (chatCleanupSettings{Days: 7}) {
		t.Fatalf("default settings = %d, %#v", code, settings)
	}
	if n := cleanup(); n != 0 {
		t.Fatalf("cleanup while off deleted %d", n)
	}
	if code := requestJSON(t, server.handler, http.MethodPut, "/api/settings/chat-cleanup", chatCleanupSettings{Enabled: true, Days: 0}, nil); code != http.StatusBadRequest {
		t.Fatalf("days 0 = %d", code)
	}
	if code := requestJSON(t, server.handler, http.MethodPut, "/api/settings/chat-cleanup", chatCleanupSettings{Enabled: true, Days: 7}, nil); code != http.StatusOK {
		t.Fatalf("enable = %d", code)
	}
	if n := cleanup(); n != 2 {
		t.Fatalf("deleted %d, want 2", n)
	}
	for id, want := range map[string]bool{stale: false, empty: false, recent: true, pinned: true, running: true} {
		var exists bool
		if err := database.QueryRow(`SELECT EXISTS(SELECT 1 FROM chat WHERE id=?)`, id).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		_, statErr := os.Stat(filepath.Join(root, "chats", id))
		if exists != want || (statErr == nil) != want {
			t.Fatalf("chat %s exists=%v folder=%v, want %v", id, exists, statErr == nil, want)
		}
	}
}
