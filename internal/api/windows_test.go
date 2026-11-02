package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"patio/internal/db"
	"reflect"
	"strings"
	"testing"
)

func TestChatWindowOpensExistingChat(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "window.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES('c','Chat','x','x')`); err != nil {
		t.Fatal(err)
	}
	var opened []string
	s := New(database, "local", false, "dev", t.TempDir(), Options{OpenChatWindow: func(id string) error { opened = append(opened, id); return nil }})
	for _, id := range []string{"c", "missing"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/chats/"+id+"/window", nil))
		if want := map[string]int{"c": 204, "missing": 404}[id]; w.Code != want {
			t.Errorf("chat %q status = %d, want %d", id, w.Code, want)
		}
	}
	if !reflect.DeepEqual(opened, []string{"c"}) {
		t.Fatalf("opened = %v", opened)
	}
}

func TestMainWindowShowsPath(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "main.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var shown []string
	s := New(database, "local", false, "dev", t.TempDir(), Options{ShowMain: func(path string) { shown = append(shown, path) }})
	for _, body := range []string{"", `{"path":"/boards/b"}`} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/windows/main", strings.NewReader(body)))
		if w.Code != 204 {
			t.Fatalf("body %q status = %d", body, w.Code)
		}
	}
	if !reflect.DeepEqual(shown, []string{"", "/boards/b"}) {
		t.Fatalf("shown = %v", shown)
	}
}

func TestMainWindowRejectsInvalidPath(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "invalid-open.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s := New(database, "local", false, "dev", t.TempDir())
	for _, path := range []string{"relative", "//boards", "/%", "/boards/%zz"} {
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

func TestValidAppPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/": true, "/boards/b": true, "/ai/chats/c?window=chat#end": true, "/boards/%20b": true,
		"": false, "boards": false, "//host/x": false, "/%": false, "/boards/%zz": false, "https://example.com": false,
	} {
		if got := ValidAppPath(path); got != want {
			t.Errorf("ValidAppPath(%q) = %v, want %v", path, got, want)
		}
	}
}
