package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestTelegramSettingsAndTokenTest(t *testing.T) {
	keyring.MockInit()
	_, handler := newAPI(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/getMe" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"username":"patio_bot"}}`))
	}))
	defer fake.Close()
	old := telegramAPIBase
	telegramAPIBase = fake.URL
	defer func() { telegramAPIBase = old }()
	var got map[string]any
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/telegram", map[string]any{"enabled": true, "allowed_user_ids": []int64{12}, "token": "token"}, &got); code != 200 {
		t.Fatalf("put status %d", code)
	}
	if got["has_token"] != true || got["enabled"] != true {
		t.Fatalf("settings = %#v", got)
	}
	secret, err := keyring.Get(connectorService, telegramTokenKey)
	if err != nil || secret != "token" {
		t.Fatalf("keyring token = %q, %v", secret, err)
	}
	if _, ok := got["token"]; ok {
		t.Fatal("response exposed token")
	}
	var tested map[string]string
	if code := requestJSON(t, handler, http.MethodPost, "/api/settings/telegram/test", map[string]any{"token": nil}, &tested); code != 200 || tested["username"] != "patio_bot" {
		t.Fatalf("test = %d %#v", code, tested)
	}
	var raw map[string]any
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/telegram", nil, &raw); code != 200 {
		t.Fatal(code)
	}
}

func TestTelegramApprovalNoticeAndAllowCallback(t *testing.T) {
	keyring.MockInit()
	db, _, root := chatAPI(t)
	cid := createTestConnector(t, db)
	chatID := createDriverTestChat(t, db, cid, root)
	if _, err := db.Exec(`INSERT INTO telegram_chat(telegram_chat_id,chat_id,created_at) VALUES(77,?,?)`, chatID, now()); err != nil {
		t.Fatal(err)
	}
	approval, _, err := CreateChatApproval(db, chatID, "read_file", json.RawMessage(`{"path":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(connectorService, telegramTokenKey, "token"); err != nil {
		t.Fatal(err)
	}
	var methods []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, strings.TrimPrefix(r.URL.Path, "/bottoken/"))
		if strings.HasSuffix(r.URL.Path, "sendMessage") {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":8}}`))
		} else {
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	defer fake.Close()
	old := telegramAPIBase
	telegramAPIBase = fake.URL
	defer func() { telegramAPIBase = old }()
	cfg := telegramSettings{Enabled: true, AllowedUserIDs: []int64{12}}
	s := New(db, "local", false, "dev", root)
	if err := s.scanTelegramNotices(context.Background(), "token", cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.scanTelegramNotices(context.Background(), "token", cfg); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(strings.Join(methods, ","), "sendMessage"); got != 1 {
		t.Fatalf("send count=%d methods=%v", got, methods)
	}
	if err := db.QueryRow(`SELECT count(*) FROM telegram_notice WHERE item_id=?`, approval).Scan(&count); err != nil || count != 1 {
		t.Fatalf("notice count=%d err=%v", count, err)
	}
	q := &struct {
		ID   string `json:"id"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Data    string `json:"data"`
		Message *struct {
			MessageID int64 `json:"message_id"`
			Chat      struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	}{ID: "cb", Data: "a:" + approval + ":1"}
	q.From.ID = 12
	q.Message = &struct {
		MessageID int64 `json:"message_id"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	}{MessageID: 8}
	q.Message.Chat.ID = 77
	if err := s.processTelegramCallback(context.Background(), "token", cfg, q); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM chat_approval WHERE id=?`, approval).Scan(&status); err != nil || status != "allowed" {
		t.Fatalf("approval status=%q err=%v", status, err)
	}
	if !strings.Contains(strings.Join(methods, ","), "editMessageText") {
		t.Fatal(fmt.Sprint(methods))
	}
	q.From.ID = 999
	q.Data = "a:" + approval + ":0"
	if err := s.processTelegramCallback(context.Background(), "token", cfg, q); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM chat_approval WHERE id=?`, approval).Scan(&status); err != nil || status != "allowed" {
		t.Fatalf("unauthorized callback changed status=%q err=%v", status, err)
	}
}

var count int
