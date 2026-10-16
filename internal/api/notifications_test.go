package api

import (
	"encoding/json"
	"fmt"
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
	called, nativeTitle := 0, ""
	s := New(database, "local", false, "dev", t.TempDir(), Options{Notify: func(title, _ string) error { called++; nativeTitle = title; return nil }})
	if err := s.Notify("chat", "Chat", "finished", "/ai/chats/chat", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Notify("calendar", "Event", "soon", "/calendar", true); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("native notifications=%d, want 1", called)
	}
	var chatID string
	if err := database.QueryRow(`SELECT id FROM notification WHERE kind='chat'`).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	native := httptest.NewRecorder()
	s.ServeHTTP(native, httptest.NewRequest(http.MethodPost, "/api/notifications/"+chatID+"/native", nil))
	missing := httptest.NewRecorder()
	s.ServeHTTP(missing, httptest.NewRequest(http.MethodPost, "/api/notifications/missing/native", nil))
	if native.Code != http.StatusNoContent || called != 2 || nativeTitle != "Chat" || missing.Code != http.StatusNotFound {
		t.Fatalf("native status=%d missing=%d calls=%d title=%q", native.Code, missing.Code, called, nativeTitle)
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
	s.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/notifications?limit=100", nil))
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
	deleted := httptest.NewRecorder()
	s.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/api/notifications/"+id, nil))
	if err := database.QueryRow(`SELECT count(*) FROM notification WHERE id=?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if deleted.Code != http.StatusNoContent || count != 0 {
		t.Fatalf("delete status=%d remaining=%d", deleted.Code, count)
	}
}

func TestNotificationPagingAndUnreadFilter(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "paging.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	s := New(database, "local", false, "dev", t.TempDir())
	for i := 1; i <= 5; i++ {
		stamp := time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC).Format("2006-01-02T15:04:05.000Z")
		if _, err := database.Exec(`INSERT INTO notification(id,kind,title,body,link,created_at) VALUES(?,?,?,?,?,?)`, fmt.Sprint(i), "chat", fmt.Sprint(i), "", "", stamp); err != nil {
			t.Fatal(err)
		}
	}
	page := func(url string) struct {
		Notifications []notification `json:"notifications"`
		Unread        int            `json:"unread"`
		NextCursor    *string        `json:"next_cursor"`
	} {
		t.Helper()
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", url, w.Code, w.Body.String())
		}
		var result struct {
			Notifications []notification `json:"notifications"`
			Unread        int            `json:"unread"`
			NextCursor    *string        `json:"next_cursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := page("/api/notifications?limit=2")
	if len(first.Notifications) != 2 || first.NextCursor == nil || first.Notifications[0].ID != "5" || first.Notifications[1].ID != "4" {
		t.Fatalf("first page = %#v", first)
	}
	if _, err := database.Exec(`INSERT INTO notification(id,kind,title,body,link,created_at) VALUES('new','chat','new','','',?)`, "2026-01-01T01:00:00.000Z"); err != nil {
		t.Fatal(err)
	}
	second := page("/api/notifications?limit=2&cursor=" + *first.NextCursor)
	if len(second.Notifications) != 2 || second.Notifications[0].ID != "3" || second.Notifications[1].ID != "2" {
		t.Fatalf("second page = %#v", second)
	}
	if _, err := database.Exec(`UPDATE notification SET read_at=? WHERE id='2'`, now()); err != nil {
		t.Fatal(err)
	}
	unread := page("/api/notifications?limit=10&unread=1")
	if unread.Unread != 5 {
		t.Fatalf("total unread=%d, want 5", unread.Unread)
	}
	for _, item := range unread.Notifications {
		if item.ID == "2" {
			t.Fatal("read notification included in unread page")
		}
	}
	invalid := httptest.NewRecorder()
	s.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/notifications?cursor=invalid", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status=%d, want 400", invalid.Code)
	}
}
