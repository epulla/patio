package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"
)

type fakeTelegramCall struct {
	method string
	body   map[string]any
}

type fakeTelegram struct {
	mu    sync.Mutex
	calls []fakeTelegramCall
	file  []byte
}

func (f *fakeTelegram) sent(method string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, call := range f.calls {
		if call.method == method {
			out = append(out, call.body)
		}
	}
	return out
}

// newFakeTelegram serves the Bot API for token "token" and sets it as the stored bot token.
func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()
	f := &fakeTelegram{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/file/bottoken/") {
			_, _ = w.Write(f.file)
			return
		}
		method := strings.TrimPrefix(r.URL.Path, "/bottoken/")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.calls = append(f.calls, fakeTelegramCall{method, body})
		id := len(f.calls)
		f.mu.Unlock()
		switch method {
		case "sendMessage":
			fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, id)
		case "getFile":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"files/upload.bin"}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	t.Cleanup(server.Close)
	oldAPI, oldFile := telegramAPIBase, telegramFileBase
	telegramAPIBase, telegramFileBase = server.URL, server.URL
	t.Cleanup(func() { telegramAPIBase, telegramFileBase = oldAPI, oldFile })
	if err := keyring.Set(connectorService, telegramTokenKey, "token"); err != nil {
		t.Fatal(err)
	}
	return f
}

func telegramText(from, chat int64, text string) *telegramMessage {
	m := &telegramMessage{Text: text}
	m.From.ID, m.Chat.ID = from, chat
	return m
}

func telegramTap(from, chat int64, data string) *telegramCallback {
	q := &telegramCallback{ID: "cb", Data: data, Message: &telegramMessage{MessageID: 1}}
	q.From.ID, q.Message.Chat.ID = from, chat
	return q
}

var telegramTestCfg = telegramSettings{Enabled: true, AllowedUserIDs: []int64{12}}

func mapTelegramChat(t *testing.T, s *Server, chatID string) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO telegram_chat(telegram_chat_id,chat_id,created_at) VALUES(77,?,?)`, chatID, now()); err != nil {
		t.Fatal(err)
	}
	if err := s.saveTelegramSettings(telegramTestCfg); err != nil {
		t.Fatal(err)
	}
}

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
	if _, ok := got["token"]; ok {
		t.Fatal("response exposed token")
	}
	if secret, err := keyring.Get(connectorService, telegramTokenKey); err != nil || secret != "token" {
		t.Fatalf("keyring token = %q, %v", secret, err)
	}
	var tested map[string]string
	if code := requestJSON(t, handler, http.MethodPost, "/api/settings/telegram/test", map[string]any{"token": nil}, &tested); code != 200 || tested["username"] != "patio_bot" {
		t.Fatalf("test = %d %#v", code, tested)
	}
}

func TestTelegramMessageReachesChatAndReplyComesBack(t *testing.T) {
	database, _, chatID, s, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeSSE(w, `{"choices":[{"delta":{"content":"Received"}}]}`)
	})
	fake := newFakeTelegram(t)
	mapTelegramChat(t, s, chatID)
	if err := s.processTelegramUpdate(context.Background(), "token", telegramTestCfg, telegramUpdate{Message: telegramText(12, 77, "hello")}); err != nil {
		t.Fatal(err)
	}
	waitLocalDone(t, events)
	var content string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='user'`, chatID).Scan(&content); err != nil || content != "hello" {
		t.Fatalf("user message = %q, %v", content, err)
	}
	waitFor(t, func() bool {
		for _, body := range fake.sent("sendMessage") {
			if body["text"] == "Received" && body["chat_id"] == float64(77) {
				return true
			}
		}
		return false
	})
}

func TestTelegramRefusesUnknownUser(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	s := New(database, "local", false, "dev", root)
	fake := newFakeTelegram(t)
	if err := s.processTelegramUpdate(context.Background(), "token", telegramTestCfg, telegramUpdate{Message: telegramText(99, 99, "hi")}); err != nil {
		t.Fatal(err)
	}
	if sent := fake.sent("sendMessage"); len(sent) != 1 || !strings.Contains(sent[0]["text"].(string), "Your Telegram ID is 99") {
		t.Fatalf("replies = %v", sent)
	}
	var chats int
	if err := database.QueryRow(`SELECT count(*) FROM chat`).Scan(&chats); err != nil || chats != 0 {
		t.Fatalf("chats = %d, %v", chats, err)
	}
}

func TestTelegramNewMapsFreshChat(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	createTestConnector(t, database)
	s := New(database, "local", false, "dev", root)
	newFakeTelegram(t)
	message := telegramText(12, 77, "/new")
	message.Chat.FirstName = "Erick"
	if err := s.processTelegramUpdate(context.Background(), "token", telegramTestCfg, telegramUpdate{Message: message}); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := database.QueryRow(`SELECT c.title FROM telegram_chat t JOIN chat c ON c.id=t.chat_id WHERE t.telegram_chat_id=77`).Scan(&title); err != nil || title != "Telegram: Erick" {
		t.Fatalf("mapped chat = %q, %v", title, err)
	}
}

func TestTelegramOffsetKeepsSettingsSavedMeanwhile(t *testing.T) {
	database, _, root := chatAPI(t)
	s := New(database, "local", false, "dev", root)
	if err := s.saveTelegramSettings(telegramSettings{Enabled: false, AllowedUserIDs: []int64{5}, UpdateOffset: 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.saveTelegramOffset(9); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.readTelegramSettings()
	if err != nil || cfg.UpdateOffset != 9 || cfg.Enabled || len(cfg.AllowedUserIDs) != 1 {
		t.Fatalf("settings = %#v, %v", cfg, err)
	}
}

func TestTelegramSplitsLongReplies(t *testing.T) {
	keyring.MockInit()
	fake := newFakeTelegram(t)
	if err := telegramSend(context.Background(), "token", 77, strings.Repeat("é", 5000)); err != nil {
		t.Fatal(err)
	}
	sent := fake.sent("sendMessage")
	if len(sent) != 2 || len([]rune(sent[0]["text"].(string))) != 4096 || len([]rune(sent[1]["text"].(string))) != 904 {
		t.Fatalf("sent %d messages", len(sent))
	}
}

func TestTelegramApprovalButtons(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	chatID := createDriverTestChat(t, database, createTestConnector(t, database), root)
	s := New(database, "local", false, "dev", root)
	fake := newFakeTelegram(t)
	mapTelegramChat(t, s, chatID)
	approval, _, err := CreateChatApproval(database, chatID, "read_file", json.RawMessage(`{"path":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.scanTelegramNotices(context.Background(), "token"); err != nil {
			t.Fatal(err)
		}
	}
	if sent := fake.sent("sendMessage"); len(sent) != 1 || !strings.Contains(fmt.Sprint(sent[0]["reply_markup"]), "a:"+approval+":1") {
		t.Fatalf("notices = %v", sent)
	}
	status := func() string {
		var status string
		if err := database.QueryRow(`SELECT status FROM chat_approval WHERE id=?`, approval).Scan(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	if err := s.processTelegramCallback(context.Background(), "token", telegramTestCfg, telegramTap(99, 77, "a:"+approval+":1")); err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "pending" {
		t.Fatalf("stranger changed approval to %s", got)
	}
	if err := s.processTelegramCallback(context.Background(), "token", telegramTestCfg, telegramTap(12, 77, "a:"+approval+":1")); err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "allowed" {
		t.Fatalf("approval = %s", got)
	}
	if edits := fake.sent("editMessageText"); len(edits) != 1 || edits[0]["text"] != "✅ Allowed" {
		t.Fatalf("edits = %v", edits)
	}
}

func TestTelegramQuestionButtonAnswers(t *testing.T) {
	database, _, chatID, s, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeSSE(w, `{"choices":[{"delta":{"content":"Ok"}}]}`)
	})
	newFakeTelegram(t)
	mapTelegramChat(t, s, chatID)
	question, err := InsertChatMessage(database, chatID, "question", `{"questions":[{"header":"Scope","question":"Which?","options":[{"label":"Small"},{"label":"Full"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.processTelegramCallback(context.Background(), "token", telegramTestCfg, telegramTap(12, 77, "q:"+question.ID+"#0:1")); err != nil {
		t.Fatal(err)
	}
	waitLocalDone(t, events)
	var content string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='user'`, chatID).Scan(&content); err != nil || content != "Scope: Full" {
		t.Fatalf("answer = %q, %v", content, err)
	}
}

func TestTelegramPhotoBecomesAttachment(t *testing.T) {
	database, _, chatID, s, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeSSE(w, `{"choices":[{"delta":{"content":"Nice"}}]}`)
	})
	fake := newFakeTelegram(t)
	fake.file = append([]byte("\xff\xd8\xff\xe0"), make([]byte, 64)...)
	mapTelegramChat(t, s, chatID)
	message := telegramText(12, 77, "")
	message.Caption = "look"
	message.Photo = append(message.Photo, struct {
		FileID string `json:"file_id"`
	}{"small"}, struct {
		FileID string `json:"file_id"`
	}{"large"})
	if err := s.processTelegramUpdate(context.Background(), "token", telegramTestCfg, telegramUpdate{Message: message}); err != nil {
		t.Fatal(err)
	}
	waitLocalDone(t, events)
	if got := fake.sent("getFile"); len(got) != 1 || got[0]["file_id"] != "large" {
		t.Fatalf("getFile = %v", got)
	}
	var content, attachments string
	if err := database.QueryRow(`SELECT content,attachments FROM chat_message WHERE chat_id=? AND role='user'`, chatID).Scan(&content, &attachments); err != nil {
		t.Fatal(err)
	}
	if content != "look" || !strings.Contains(attachments, `"photo.jpg"`) || !strings.Contains(attachments, `"image"`) {
		t.Fatalf("message = %q %s", content, attachments)
	}
}

func TestTelegramRejectsUnsupportedFile(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	chatID := createDriverTestChat(t, database, createTestConnector(t, database), root)
	s := New(database, "local", false, "dev", root)
	fake := newFakeTelegram(t)
	fake.file = []byte("ID3\x03\x00audio")
	mapTelegramChat(t, s, chatID)
	message := telegramText(12, 77, "")
	message.Document = &struct {
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
	}{"doc", "song.mp3"}
	if err := s.processTelegramUpdate(context.Background(), "token", telegramTestCfg, telegramUpdate{Message: message}); err != nil {
		t.Fatal(err)
	}
	if sent := fake.sent("sendMessage"); len(sent) != 1 || !strings.HasPrefix(sent[0]["text"].(string), "Patio can't take this file") {
		t.Fatalf("replies = %v", sent)
	}
	var messages int
	if err := database.QueryRow(`SELECT count(*) FROM chat_message WHERE chat_id=?`, chatID).Scan(&messages); err != nil || messages != 0 {
		t.Fatalf("messages = %d, %v", messages, err)
	}
}
