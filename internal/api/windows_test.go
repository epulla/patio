package api

import (
	"encoding/json"
	"my-tickets/internal/db"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestChatWindowFocusOrStart(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "window.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES('c','Chat','x','x')`); err != nil {
		t.Fatal(err)
	}
	started, focusID := 0, ""
	s := New(database, "local", true, "dev", t.TempDir(), Options{DBPath: "db", WindowID: "self", StartWindow: func(args []string) error { started++; return nil }})
	call := func() int {
		r := httptest.NewRequest(http.MethodPost, "/api/chats/c/window", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	if _, err := database.Exec(`INSERT INTO app_window(id,chat_id,pid,heartbeat_at) VALUES('live','c',1,?)`, now()); err != nil {
		t.Fatal(err)
	}
	if call() != 204 {
		t.Fatal("live window not accepted")
	}
	if err := database.QueryRow(`SELECT focus_at FROM app_window WHERE id='live'`).Scan(&focusID); err != nil || focusID == "" {
		t.Fatalf("focus timestamp %q, %v", focusID, err)
	}
	if _, err := database.Exec(`UPDATE app_window SET heartbeat_at='2000-01-01T00:00:00.000Z' WHERE id='live'`); err != nil {
		t.Fatal(err)
	}
	if call() != 204 || started != 1 {
		t.Fatalf("stale window start count %d", started)
	}
}

func TestMainWindowFocusOrStart(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "main.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	started := 0
	s := New(database, "local", false, "dev", t.TempDir(), Options{WindowID: "self", StartWindow: func([]string) error { started++; return nil }})
	call := func() int {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/windows/main", nil))
		return w.Code
	}
	if _, err := database.Exec(`INSERT INTO app_window(id,pid,heartbeat_at) VALUES('main',1,?)`, now()); err != nil {
		t.Fatal(err)
	}
	if call() != 204 {
		t.Fatal("focus failed")
	}
	if _, err := database.Exec(`DELETE FROM app_window`); err != nil {
		t.Fatal(err)
	}
	if call() != 204 || started != 1 {
		t.Fatalf("start count %d", started)
	}
}

func TestMainWindowFocusStoresPath(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "focus-path.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO app_window(id,pid,heartbeat_at) VALUES('main',1,?)`, now()); err != nil {
		t.Fatal(err)
	}
	s := New(database, "local", false, "dev", t.TempDir(), Options{WindowID: "caller"})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/windows/main", strings.NewReader(`{"path":"/boards/b"}`)))
	var got string
	if w.Code != 204 {
		t.Fatalf("status = %d", w.Code)
	}
	if err := database.QueryRow(`SELECT focus_path FROM app_window WHERE id='main'`).Scan(&got); err != nil || got != "/boards/b" {
		t.Fatalf("focus path = %q, %v", got, err)
	}
}

func TestMainWindowColdStartPassesOpenPath(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "cold-open.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var got []string
	s := New(database, "local", false, "dev", t.TempDir(), Options{DBPath: "db", StartWindow: func(args []string) error { got = args; return nil }})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/windows/main", strings.NewReader(`{"path":"/boards/b"}`)))
	if w.Code != 204 || !reflect.DeepEqual(got, []string{"-d", "db", "-open", "/boards/b"}) {
		t.Fatalf("status=%d args=%v", w.Code, got)
	}
}

func TestMainWindowRejectsInvalidPath(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "invalid-open.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s := New(database, "local", false, "dev", t.TempDir())
	for _, path := range []string{"relative", "//boards"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/windows/main", strings.NewReader(`{"path":"`+path+`"}`)))
		if w.Code != 400 {
			t.Errorf("path %q status = %d", path, w.Code)
		}
	}
}

func TestChatTurnGuardLiveAndStale(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "guard.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO app_window(id,pid,heartbeat_at) VALUES('other',1,?)`, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO chat_turn(chat_id,window_id,started_at) VALUES('c','other',?)`, now()); err != nil {
		t.Fatal(err)
	}
	m := newChatManager(database, Options{WindowID: "mine"})
	if err := m.claimTurn("c"); err != errChatTurnOtherWindow {
		t.Fatalf("claim live = %v", err)
	}
	if _, err := database.Exec(`UPDATE app_window SET heartbeat_at='2000-01-01T00:00:00.000Z' WHERE id='other'`); err != nil {
		t.Fatal(err)
	}
	if err := m.claimTurn("c"); err != nil {
		t.Fatalf("take over stale: %v", err)
	}
	var owner string
	if err := database.QueryRow(`SELECT window_id FROM chat_turn WHERE chat_id='c'`).Scan(&owner); err != nil || owner != "mine" {
		t.Fatalf("owner %q, %v", owner, err)
	}
}

func TestConfigIncludesChatWindow(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s := New(database, "local", false, "dev", t.TempDir(), Options{ChatWindow: "chat-id", OpenPath: "/boards/b"})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["chat_window"] != "chat-id" {
		t.Fatalf("config = %#v", got)
	}
	if got["open_path"] != "/boards/b" {
		t.Fatalf("open_path = %#v", got["open_path"])
	}
}

func TestLiveChatWindowCountExcludesSelfAndStale(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "count.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, row := range []struct{ id, chat, heartbeat string }{
		{"self", "one", now()}, {"other", "two", now()}, {"stale", "three", "2000-01-01T00:00:00.000Z"},
	} {
		if _, err := database.Exec(`INSERT INTO app_window(id,chat_id,pid,heartbeat_at) VALUES(?,?,1,?)`, row.id, row.chat, row.heartbeat); err != nil {
			t.Fatal(err)
		}
	}
	s := New(database, "local", false, "dev", t.TempDir(), Options{WindowID: "self"})
	count, err := s.LiveChatWindowCount()
	if err != nil || count != 1 {
		t.Fatalf("live other chat windows = %d, %v; want 1", count, err)
	}
}
