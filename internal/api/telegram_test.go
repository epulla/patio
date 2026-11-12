package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	// fileStarted and fileGate, when set, pause file downloads until the test lets them go.
	fileStarted chan struct{}
	fileGate    chan struct{}
	// onCall, when set, runs before the reply and may write its own response.
	onCall func(method string, w http.ResponseWriter) bool
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
			if f.fileGate != nil {
				f.fileStarted <- struct{}{}
				<-f.fileGate
			}
			_, _ = w.Write(f.file)
			return
		}
		method := strings.TrimPrefix(r.URL.Path, "/bottoken/")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.calls = append(f.calls, fakeTelegramCall{method, body})
		id := len(f.calls)
		onCall := f.onCall
		f.mu.Unlock()
		if onCall != nil && onCall(method, w) {
			return
		}
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
	var paths []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
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
	if strings.Join(paths, ",") != "/bottoken/getMe,/bottoken/setMyCommands" {
		t.Fatalf("calls = %v", paths)
	}
}

func TestTelegramMessageReachesChatAndReplyComesBack(t *testing.T) {
	database, _, chatID, s, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeSSE(w, `{"choices":[{"delta":{"content":"Received"}}]}`)
	})
	fake := newFakeTelegram(t)
	// Finish background Telegram replies before the fake server goes away.
	t.Cleanup(func() { _ = s.Close() })
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

func TestTelegramChatUsesDefaultBotConnectorAndModel(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	createTestConnector(t, database)
	botConnector := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'codex','Codex',?)`, botConnector, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai,connector_id,model) VALUES('billy','Billy','billy@bot',1,1,?,'b-model')`, botConnector); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO app_setting(key,value) VALUES('default_bot_id','"billy"') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		t.Fatal(err)
	}
	s := New(database, "local", false, "dev", root)
	newFakeTelegram(t)
	if err := s.processTelegramUpdate(context.Background(), "token", telegramTestCfg, telegramUpdate{Message: telegramText(12, 77, "/new")}); err != nil {
		t.Fatal(err)
	}
	var connector, model, bot string
	if err := database.QueryRow(`SELECT c.connector_id,c.model,c.bot_id FROM telegram_chat t JOIN chat c ON c.id=t.chat_id`).Scan(&connector, &model, &bot); err != nil {
		t.Fatal(err)
	}
	if connector != botConnector || model != "b-model" || bot != "billy" {
		t.Fatalf("chat = %s %s %s", connector, model, bot)
	}
}

func putTelegram(handler http.Handler, body map[string]any) int {
	encoded, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/settings/telegram", bytes.NewReader(encoded)))
	return rec.Code
}

func TestTelegramSettingsAndOffsetSavesTakeTurns(t *testing.T) {
	keyring.MockInit()
	_, handler, _ := chatAPI(t)
	s := handler.(*Server)
	if code := putTelegram(s, map[string]any{"enabled": true, "allowed_user_ids": []int64{5}, "token": "token"}); code != 200 {
		t.Fatalf("put status %d", code)
	}
	s.telegramMu.Lock()
	putDone, offsetDone := make(chan int), make(chan error)
	go func() {
		putDone <- putTelegram(s, map[string]any{"enabled": false, "allowed_user_ids": []int64{5, 6}, "token": nil})
	}()
	go func() { offsetDone <- s.saveTelegramOffset("token", 9) }()
	select {
	case <-putDone:
		t.Fatal("settings saved without the lock")
	case <-offsetDone:
		t.Fatal("offset saved without the lock")
	case <-time.After(200 * time.Millisecond):
	}
	s.telegramMu.Unlock()
	if code := <-putDone; code != 200 {
		t.Fatalf("put status %d", code)
	}
	if err := <-offsetDone; err != nil {
		t.Fatal(err)
	}
	if err := s.saveTelegramOffset("token", 4); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.readTelegramSettings()
	if err != nil || cfg.UpdateOffset != 9 || cfg.Enabled || len(cfg.AllowedUserIDs) != 2 {
		t.Fatalf("settings = %#v, %v", cfg, err)
	}
	if code := putTelegram(s, map[string]any{"enabled": true, "allowed_user_ids": []int64{5}, "token": "new"}); code != 200 {
		t.Fatalf("put status %d", code)
	}
	if err := s.saveTelegramOffset("token", 20); err != nil {
		t.Fatal(err)
	}
	if cfg, err = s.readTelegramSettings(); err != nil || cfg.UpdateOffset != 0 {
		t.Fatalf("new bot offset = %d, %v", cfg.UpdateOffset, err)
	}
}

func TestTelegramErrorsHideToken(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	old := telegramAPIBase
	telegramAPIBase = closed.URL
	defer func() { telegramAPIBase = old }()
	err := telegramCall(context.Background(), "123:SECRET", "getMe", nil, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error = %v", err)
	}
}

func runTelegramBridge(t *testing.T, s *Server) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.StartTelegramBridge(ctx) }()
	return func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTelegramRunnerProcessesAndPersistsOffset(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	s := New(database, "local", false, "dev", root)
	fake := newFakeTelegram(t)
	if err := s.saveTelegramSettings(telegramTestCfg); err != nil {
		t.Fatal(err)
	}
	polls := 0
	fake.onCall = func(method string, w http.ResponseWriter) bool {
		if method != "getUpdates" {
			return false
		}
		polls++
		if polls == 1 {
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":41,"message":{"message_id":1,"text":"hi","chat":{"id":99},"from":{"id":99}}}]}`))
		} else {
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		}
		return true
	}
	stop := runTelegramBridge(t, s)
	waitFor(t, func() bool {
		cfg, err := s.readTelegramSettings()
		return err == nil && cfg.UpdateOffset == 42
	})
	stop()
	if sent := fake.sent("sendMessage"); len(sent) != 1 || !strings.Contains(sent[0]["text"].(string), "99") {
		t.Fatalf("replies = %v", sent)
	}
}

// pollOffset is the offset the runner asked for in its nth getUpdates call (1-based).
func (f *fakeTelegram) pollOffset(n int) float64 {
	polls := f.sent("getUpdates")
	if len(polls) < n {
		return -1
	}
	return polls[n-1]["offset"].(float64)
}

func TestTelegramRunnerLeavesUpdateWhenDisabledMidBatch(t *testing.T) {
	keyring.MockInit()
	database, handler, _ := chatAPI(t)
	s := handler.(*Server)
	fake := newFakeTelegram(t)
	if code := putTelegram(s, map[string]any{"enabled": true, "allowed_user_ids": []int64{12}, "token": nil}); code != 200 {
		t.Fatalf("put status %d", code)
	}
	var polls int
	polled := make(chan struct{})
	fake.onCall = func(method string, w http.ResponseWriter) bool {
		if method != "getUpdates" {
			return false
		}
		polls++
		if polls == 1 {
			// Turned off while this batch is in flight; saved without a wake-up so the poll isn't cancelled
			// and the batch itself must notice.
			if err := s.saveTelegramSettings(telegramSettings{AllowedUserIDs: []int64{12}}); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":41,"message":{"message_id":1,"text":"hi","chat":{"id":77},"from":{"id":12}}}]}`))
			close(polled)
			return true
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		return true
	}
	stop := runTelegramBridge(t, s)
	<-polled
	stop() // returns once the batch is handled
	var chats int
	if err := database.QueryRow(`SELECT count(*) FROM chat`).Scan(&chats); err != nil || chats != 0 {
		t.Fatalf("chats = %d, %v", chats, err)
	}
	defer runTelegramBridge(t, s)()
	if code := putTelegram(s, map[string]any{"enabled": true, "allowed_user_ids": []int64{12}, "token": nil}); code != 200 {
		t.Fatalf("put status %d", code)
	}
	waitFor(t, func() bool { return fake.pollOffset(2) >= 0 })
	if got := fake.pollOffset(2); got != 0 {
		t.Fatalf("next poll offset = %v, want update 41 not acknowledged", got)
	}
}

func TestTelegramRunnerLeavesUpdateWhenDisabledDuringDownload(t *testing.T) {
	database, root, chatID, s, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeSSE(w, `{"choices":[{"delta":{"content":"Nice"}}]}`)
	})
	fake := newFakeTelegram(t)
	// Finish background Telegram replies before the fake server goes away.
	t.Cleanup(func() { _ = s.Close() })
	fake.file = append([]byte("\xff\xd8\xff\xe0"), make([]byte, 64)...)
	fake.fileStarted, fake.fileGate = make(chan struct{}, 2), make(chan struct{})
	mapTelegramChat(t, s, chatID)
	var polls int
	fake.onCall = func(method string, w http.ResponseWriter) bool {
		if method != "getUpdates" {
			return false
		}
		polls++
		if polls <= 2 {
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":41,"message":{"message_id":1,"caption":"look","photo":[{"file_id":"p"}],"chat":{"id":77},"from":{"id":12}}}]}`))
		} else {
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		}
		return true
	}
	stop := runTelegramBridge(t, s)
	<-fake.fileStarted
	if code := putTelegram(s, map[string]any{"enabled": false, "allowed_user_ids": []int64{12}, "token": nil}); code != 200 {
		t.Fatalf("put status %d", code)
	}
	close(fake.fileGate)
	stop()
	var messages int
	if err := database.QueryRow(`SELECT count(*) FROM chat_message WHERE chat_id=?`, chatID).Scan(&messages); err != nil || messages != 0 {
		t.Fatalf("messages after abort = %d, %v", messages, err)
	}
	if files, _ := os.ReadDir(filepath.Join(root, "chats", chatID, "attachments")); len(files) != 0 {
		t.Fatalf("files after abort = %v", files)
	}
	if cfg, err := s.readTelegramSettings(); err != nil || cfg.UpdateOffset != 0 {
		t.Fatalf("offset after abort = %d, %v", cfg.UpdateOffset, err)
	}
	// Turned back on, the same bot gets the update again and it goes through.
	defer runTelegramBridge(t, s)()
	if code := putTelegram(s, map[string]any{"enabled": true, "allowed_user_ids": []int64{12}, "token": nil}); code != 200 {
		t.Fatalf("put status %d", code)
	}
	waitLocalDone(t, events)
	if got := fake.pollOffset(2); got != 0 {
		t.Fatalf("second poll offset = %v", got)
	}
	var content string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='user'`, chatID).Scan(&content); err != nil || content != "look" {
		t.Fatalf("message = %q, %v", content, err)
	}
}

func TestTelegramRunnerBacksOffOnConflict(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	s := New(database, "local", false, "dev", root)
	fake := newFakeTelegram(t)
	if err := s.saveTelegramSettings(telegramTestCfg); err != nil {
		t.Fatal(err)
	}
	fake.onCall = func(method string, w http.ResponseWriter) bool {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Conflict: terminated by other getUpdates request"}`))
		return true
	}
	stop := runTelegramBridge(t, s)
	time.Sleep(2500 * time.Millisecond)
	stop()
	if polls := len(fake.sent("getUpdates")); polls < 1 || polls > 3 {
		t.Fatalf("getUpdates calls = %d", polls)
	}
}

func TestTelegramSlowReplyDoesNotBlockTurn(t *testing.T) {
	_, _, chatID, s, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeSSE(w, `{"choices":[{"delta":{"content":"Done"}}]}`)
	})
	fake := newFakeTelegram(t)
	// Finish background Telegram replies before the fake server goes away.
	t.Cleanup(func() { _ = s.Close() })
	release := make(chan struct{})
	fake.onCall = func(method string, _ http.ResponseWriter) bool {
		if method == "sendMessage" {
			<-release
		}
		return false
	}
	t.Cleanup(func() { close(release) })
	mapTelegramChat(t, s, chatID)
	if code := sendLocalMessage(t, s, chatID, "hello"); code != http.StatusAccepted {
		t.Fatalf("send status = %d", code)
	}
	waitLocalDone(t, events)
	if s.chats.Running(chatID) {
		t.Fatal("turn still running while Telegram reply is stuck")
	}
}

func TestTelegramSplitsLongReplies(t *testing.T) {
	keyring.MockInit()
	fake := newFakeTelegram(t)
	if err := telegramSend(context.Background(), "token", 77, strings.Repeat("é", 5000)); err != nil {
		t.Fatal(err)
	}
	sent := fake.sent("sendMessage")
	if len(sent) != 2 || len([]rune(sent[0]["text"].(string))) != 3000 || len([]rune(sent[1]["text"].(string))) != 2000 {
		t.Fatalf("sent %d messages", len(sent))
	}
}

func TestTelegramSendsHTMLAndFallsBackToPlain(t *testing.T) {
	keyring.MockInit()
	fake := newFakeTelegram(t)
	fake.onCall = func(method string, w http.ResponseWriter) bool {
		if method != "sendMessage" || len(fake.sent("sendMessage")) > 1 {
			return false
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: can't parse entities: unexpected end tag"}`))
		return true
	}
	if err := telegramSend(context.Background(), "token", 77, "**Hi** <you>"); err != nil {
		t.Fatal(err)
	}
	sent := fake.sent("sendMessage")
	if len(sent) != 2 || sent[0]["parse_mode"] != "HTML" || sent[0]["text"] != "<b>Hi</b> &lt;you&gt;" {
		t.Fatalf("first send = %v", sent)
	}
	if _, html := sent[1]["parse_mode"]; html || sent[1]["text"] != "Hi <you>" {
		t.Fatalf("fallback = %v", sent[1])
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
	// Finish background Telegram replies before the fake server goes away.
	t.Cleanup(func() { _ = s.Close() })
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
	// Finish background Telegram replies before the fake server goes away.
	t.Cleanup(func() { _ = s.Close() })
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

func TestTelegramMultiQuestionWaitsForEveryPick(t *testing.T) {
	database, _, chatID, s, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeSSE(w, `{"choices":[{"delta":{"content":"Ok"}}]}`)
	})
	fake := newFakeTelegram(t)
	// Finish background Telegram replies before the fake server goes away.
	t.Cleanup(func() { _ = s.Close() })
	mapTelegramChat(t, s, chatID)
	question, err := InsertChatMessage(database, chatID, "question", `{"questions":[{"header":"Size","options":[{"label":"S"},{"label":"L"}]},{"header":"Extras","multiple":true,"options":[{"label":"Tests"},{"label":"Docs"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	tap := func(data string) {
		t.Helper()
		if err := s.processTelegramCallback(context.Background(), "token", telegramTestCfg, telegramTap(12, 77, "q:"+question.ID+data)); err != nil {
			t.Fatal(err)
		}
	}
	tap("#0:0")
	tap("#1:0")
	tap("#1:1")
	if got := len(fake.sent("editMessageReplyMarkup")); got != 3 {
		t.Fatalf("keyboards kept = %d", got)
	}
	tap("#0:1") // change of mind before sending
	tap("#1:d")
	waitLocalDone(t, events)
	var content string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='user'`, chatID).Scan(&content); err != nil || content != "Size: L\nExtras: Tests, Docs" {
		t.Fatalf("answer = %q, %v", content, err)
	}
}

func TestTelegramPicksDroppedWhenAnsweredElsewhere(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	chatID := createDriverTestChat(t, database, createTestConnector(t, database), root)
	s := New(database, "local", false, "dev", root)
	newFakeTelegram(t)
	mapTelegramChat(t, s, chatID)
	question, err := InsertChatMessage(database, chatID, "question", `{"questions":[{"header":"A","options":[{"label":"x"}]},{"header":"B","options":[{"label":"y"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.processTelegramCallback(context.Background(), "token", telegramTestCfg, telegramTap(12, 77, "q:"+question.ID+"#0:0")); err != nil {
		t.Fatal(err)
	}
	if _, err := InsertChatMessage(database, chatID, "user", "something else"); err != nil {
		t.Fatal(err)
	}
	if err := s.scanTelegramNotices(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if len(s.telegramPicks) != 0 {
		t.Fatalf("picks = %v", s.telegramPicks)
	}
}

func TestTelegramContinueListsRecentChats(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	s := New(database, "local", false, "dev", root)
	fake := newFakeTelegram(t)
	old, recent := strings.Repeat("a", 32), strings.Repeat("b", 32)
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES(?,'Old','now','2026-01-01'),(?,?,'now','2026-02-01')`, old, recent, strings.Repeat("Long title ", 10)); err != nil {
		t.Fatal(err)
	}
	commands := []string{"/continue", "/recents", "/recent"}
	for _, command := range commands {
		if err := s.processTelegramUpdate(context.Background(), "token", telegramTestCfg, telegramUpdate{Message: telegramText(12, 77, command)}); err != nil {
			t.Fatal(err)
		}
	}
	sent := fake.sent("sendMessage")
	if len(sent) != len(commands) {
		t.Fatalf("replies = %v", sent)
	}
	for _, reply := range sent {
		rows := reply["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
		first := rows[0].([]any)[0].(map[string]any)
		if len(rows) != 2 || first["callback_data"] != "c:"+recent || len([]rune(first["text"].(string))) != 41 {
			t.Fatalf("keyboard = %v", rows)
		}
	}
}

func TestTelegramContinueSwitchesAndRoutesReplies(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	s := New(database, "local", false, "dev", root)
	fake := newFakeTelegram(t)
	target := strings.Repeat("c", 32)
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES(?,'Target','now','now')`, target); err != nil {
		t.Fatal(err)
	}
	mapTelegramChat(t, s, target)
	if _, err := database.Exec(`UPDATE telegram_chat SET created_at='2026-01-01' WHERE telegram_chat_id=77`); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"/continue https://patio.local/ai/chats/" + target + "?x=1", "/continue " + strings.Repeat("d", 32)} {
		if err := s.processTelegramUpdate(context.Background(), "token", telegramTestCfg, telegramUpdate{Message: telegramText(12, 88, text)}); err != nil {
			t.Fatal(err)
		}
	}
	sent := fake.sent("sendMessage")
	if len(sent) != 2 || sent[0]["text"] != "Continuing: Target" || sent[1]["text"] != "Chat not found." {
		t.Fatalf("replies = %v", sent)
	}
	s.sendTelegramResult(target, "done", nil)
	if sent := fake.sent("sendMessage"); len(sent) != 3 || sent[2]["chat_id"] != float64(88) {
		t.Fatalf("reply after switch = %v", sent)
	}
}

func TestTelegramContinueTapRemaps(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	s := New(database, "local", false, "dev", root)
	fake := newFakeTelegram(t)
	target := strings.Repeat("e", 32)
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES(?,'Target','now','now')`, target); err != nil {
		t.Fatal(err)
	}
	if err := s.processTelegramUpdate(context.Background(), "token", telegramTestCfg, telegramUpdate{CallbackQuery: telegramTap(12, 77, "c:"+target)}); err != nil {
		t.Fatal(err)
	}
	var mapped string
	if err := database.QueryRow(`SELECT chat_id FROM telegram_chat WHERE telegram_chat_id=77`).Scan(&mapped); err != nil || mapped != target {
		t.Fatalf("mapped = %q, %v", mapped, err)
	}
	if edits := fake.sent("editMessageText"); len(edits) != 1 || edits[0]["text"] != "Continuing: Target" {
		t.Fatalf("edits = %v", edits)
	}
}

func TestTelegramContinueAddsNoteToNextTurnOnly(t *testing.T) {
	s, c := attachmentTestChat(t)
	driver := &attachmentTestDriver{}
	previous, existed := runtimes["claude"]
	runtimes["claude"] = func(*chatManager, chat, string, *chatRuntime) (chatRuntimeDriver, error) { return driver, nil }
	t.Cleanup(func() {
		if existed {
			runtimes["claude"] = previous
		} else {
			delete(runtimes, "claude")
		}
	})
	if _, err := s.switchTelegramChat(88, c.ID); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{telegramPrompt + "\n\nhi", "again"} {
		text := strings.TrimPrefix(want, telegramPrompt+"\n\n")
		if code := requestJSON(t, s, http.MethodPost, "/api/chats/"+c.ID+"/messages", map[string]any{"text": text}, nil); code != http.StatusAccepted {
			t.Fatalf("send %d status = %d", i, code)
		}
		driver.mu.Lock()
		got := driver.request.Text
		driver.mu.Unlock()
		if got != want {
			t.Fatalf("turn %d driver text = %q", i, got)
		}
		if err := s.chats.finishTurn(c.ID, "ok", nil); err != nil {
			t.Fatal(err)
		}
	}
	var stored string
	if err := s.db.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='user' ORDER BY created_at LIMIT 1`, c.ID).Scan(&stored); err != nil || stored != "hi" {
		t.Fatalf("stored = %q, %v", stored, err)
	}
}

func TestTelegramOversizeChunkFallsBackToPlainPieces(t *testing.T) {
	keyring.MockInit()
	fake := newFakeTelegram(t)
	if err := telegramCallMarkdown(context.Background(), "token", "sendMessage", map[string]any{"chat_id": 77}, "**"+strings.Repeat("x", 5000)+"**", nil); err != nil {
		t.Fatal(err)
	}
	sent := fake.sent("sendMessage")
	if len(sent) != 2 || sent[0]["parse_mode"] != nil || len([]rune(sent[0]["text"].(string))) != 4000 || len([]rune(sent[1]["text"].(string))) != 1000 {
		t.Fatalf("sent = %d messages", len(sent))
	}
}
