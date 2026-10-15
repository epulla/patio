package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"patio/internal/db"
)

func TestNotificationAPIAndRetention(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	called := 0
	s := New(database, "local", false, "dev", t.TempDir(), Options{Notify: func(string, string) error { called++; return nil }})
	if err := s.Notify("chat", "Chat", "finished", "/ai/chats/chat", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Notify("calendar", "Event", "soon", "/calendar", true); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("native notifications=%d, want 1", called)
	}
	if _, err := database.Exec(`INSERT INTO notification(id,kind,title,body,link,created_at) VALUES(?,?,?,?,?,?)`, "old", "chat", "", "", "", time.Now().UTC().Add(-8*24*time.Hour).Format("2006-01-02T15:04:05.000Z")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 501; i++ {
		if _, err := database.Exec(`INSERT INTO notification(id,kind,title,body,link,created_at) VALUES(?,?,?,?,?,?)`, uid(), "change", "x", "", "", now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PurgeNotifications(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM notification`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 500 {
		t.Fatalf("retained rows=%d, want 500", count)
	}
	if err := database.QueryRow(`SELECT count(*) FROM notification WHERE id='old'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("8-day-old notification was not purged")
	}
	if _, err := database.Exec(`UPDATE notification SET created_at=? WHERE id=(SELECT id FROM notification ORDER BY rowid DESC LIMIT 1)`, now()); err != nil {
		t.Fatal(err)
	}
	list := httptest.NewRecorder()
	s.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/notifications", nil))
	var response struct {
		Notifications []notification `json:"notifications"`
		Unread        int            `json:"unread"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Unread != 500 || len(response.Notifications) != 100 {
		t.Fatalf("unread=%d listed=%d", response.Unread, len(response.Notifications))
	}
	id := response.Notifications[0].ID
	read := httptest.NewRecorder()
	s.ServeHTTP(read, httptest.NewRequest(http.MethodPost, "/api/notifications/"+id+"/read", nil))
	if read.Code != http.StatusNoContent {
		t.Fatalf("read status=%d", read.Code)
	}
	list = httptest.NewRecorder()
	s.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/notifications", nil))
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Unread != 499 {
		t.Fatalf("unread after one read=%d, want 499", response.Unread)
	}
	all := httptest.NewRecorder()
	s.ServeHTTP(all, httptest.NewRequest(http.MethodPost, "/api/notifications/read-all", nil))
	if all.Code != http.StatusNoContent {
		t.Fatalf("read-all status=%d", all.Code)
	}
	if err := database.QueryRow(`SELECT count(*) FROM notification WHERE read_at IS NULL`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unread after read-all=%d", count)
	}
}
