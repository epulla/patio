package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type telegramMessage struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Caption   string `json:"caption"`
	Photo     []struct {
		FileID string `json:"file_id"`
	} `json:"photo"`
	Document *struct {
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
	} `json:"document"`
	Chat struct {
		ID        int64  `json:"id"`
		FirstName string `json:"first_name"`
		Username  string `json:"username"`
	} `json:"chat"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
}

type telegramCallback struct {
	ID   string `json:"id"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Data    string           `json:"data"`
	Message *telegramMessage `json:"message"`
}

type telegramUpdate struct {
	UpdateID      int64             `json:"update_id"`
	Message       *telegramMessage  `json:"message"`
	CallbackQuery *telegramCallback `json:"callback_query"`
}

// telegramPick is one ask_user question's picks made with Telegram buttons, kept until all are answered.
type telegramPick struct {
	labels map[string]bool
	done   bool
}

type telegramQuestions struct {
	Questions []struct {
		Header   string `json:"header"`
		Question string `json:"question"`
		Multiple bool   `json:"multiple"`
		Options  []struct {
			Label string `json:"label"`
		} `json:"options"`
	} `json:"questions"`
}

func (s *Server) StartTelegramBridge(ctx context.Context) error {
	ctx, stop := context.WithCancel(ctx)
	notices := make(chan struct{})
	go func() {
		defer close(notices)
		s.telegramNoticeLoop(ctx)
	}()
	defer func() {
		stop()
		<-notices
	}()
	backoff := time.Second
	for {
		cfg, err := s.readTelegramSettings()
		if err != nil {
			return err
		}
		token, tokenErr := telegramToken()
		if !cfg.Enabled || tokenErr != nil || token == "" {
			select {
			case <-ctx.Done():
				return nil
			case <-s.telegramWake:
				continue
			case <-time.After(time.Second):
				continue
			}
		}
		var updates []telegramUpdate
		pollCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		wake := make(chan struct{})
		woken := false
		go func() {
			select {
			case <-s.telegramWake:
				woken = true
				cancel()
			case <-pollCtx.Done():
			}
			close(wake)
		}()
		err = telegramCall(pollCtx, token, "getUpdates", map[string]any{"offset": cfg.UpdateOffset, "timeout": 30, "allowed_updates": []string{"message", "callback_query"}}, &updates)
		cancel()
		<-wake
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if woken {
				backoff = time.Second
				continue
			}
			// 409: another Patio polls the same bot; back off quietly instead of spamming the log.
			if !strings.Contains(err.Error(), "Conflict") || backoff == time.Second {
				log.Printf("Telegram getUpdates: %v", err)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-s.telegramWake:
				backoff = time.Second
				continue
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		offset := cfg.UpdateOffset
		for _, update := range updates {
			// Settings may change mid-batch: stop before side effects; unhandled updates stay on Telegram.
			latest, ok := s.telegramActive(token)
			if !ok {
				break
			}
			err := s.processTelegramUpdate(ctx, token, latest, update)
			if errors.Is(err, errTelegramAborted) || ctx.Err() != nil {
				break
			}
			if err != nil {
				log.Printf("process Telegram update %d: %v", update.UpdateID, err)
			}
			offset = max(offset, update.UpdateID+1)
		}
		if err := s.saveTelegramOffset(token, offset); err != nil {
			log.Printf("save Telegram offset: %v", err)
		}
	}
}

// errTelegramAborted means settings changed while an update was handled; it stays unacknowledged.
var errTelegramAborted = errors.New("Telegram bridge settings changed")

// telegramActive returns current settings while the bridge is enabled and token is still the stored token.
func (s *Server) telegramActive(token string) (telegramSettings, bool) {
	cfg, err := s.readTelegramSettings()
	if err != nil {
		log.Printf("read Telegram settings: %v", err)
		return cfg, false
	}
	current, err := telegramToken()
	return cfg, err == nil && cfg.Enabled && current == token
}

// saveTelegramOffset holds telegramMu like the settings PUT, so neither overwrites the other, and skips
// offsets of a bot whose token was replaced meanwhile.
func (s *Server) saveTelegramOffset(token string, offset int64) error {
	s.telegramMu.Lock()
	defer s.telegramMu.Unlock()
	if current, err := telegramToken(); err != nil || current != token {
		return nil
	}
	cfg, err := s.readTelegramSettings()
	if err != nil || offset <= cfg.UpdateOffset {
		return err
	}
	cfg.UpdateOffset = offset
	return s.saveTelegramSettings(cfg)
}

func (s *Server) processTelegramUpdate(ctx context.Context, token string, cfg telegramSettings, update telegramUpdate) error {
	if update.Message != nil {
		return s.processTelegramMessage(ctx, token, cfg, update.Message)
	}
	if update.CallbackQuery != nil {
		return s.processTelegramCallback(ctx, token, cfg, update.CallbackQuery)
	}
	return nil
}

func telegramAllowed(cfg telegramSettings, id int64) bool {
	for _, allowed := range cfg.AllowedUserIDs {
		if allowed == id {
			return true
		}
	}
	return false
}

func (s *Server) processTelegramMessage(ctx context.Context, token string, cfg telegramSettings, m *telegramMessage) error {
	reply := func(text string) error {
		return telegramCallMarkdown(ctx, token, "sendMessage", map[string]any{"chat_id": m.Chat.ID}, text, nil)
	}
	if !telegramAllowed(cfg, m.From.ID) {
		return reply(fmt.Sprintf("Not allowed. Your Telegram ID is %d: add it in Patio Connections › Channels.", m.From.ID))
	}
	if command, arg, _ := strings.Cut(strings.TrimSpace(m.Text), " "); command == "/continue" {
		return s.continueTelegramChat(ctx, token, m.Chat.ID, strings.TrimSpace(arg))
	}
	var chatID string
	err := s.db.QueryRow(`SELECT chat_id FROM telegram_chat WHERE telegram_chat_id=?`, m.Chat.ID).Scan(&chatID)
	if errors.Is(err, sql.ErrNoRows) || m.Text == "/new" {
		name := m.Chat.FirstName
		if name == "" {
			name = m.Chat.Username
		}
		if name == "" {
			name = fmt.Sprint(m.Chat.ID)
		}
		chatID, err = s.createTelegramChat("Telegram: " + name)
		if err != nil {
			return err
		}
		if err := s.mapTelegramChat(m.Chat.ID, chatID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	switch m.Text {
	case "/new":
		return reply("Started a new Patio chat.")
	case "/chat":
		var title string
		if err := s.db.QueryRow(`SELECT title FROM chat WHERE id=?`, chatID).Scan(&title); err != nil {
			return err
		}
		return reply("You're in the Patio chat “" + title + "”. Open it in Patio › Chats.")
	}
	text := m.Text
	if text == "" {
		text = m.Caption
	}
	type telegramFile struct{ id, name string }
	var files []telegramFile
	if len(m.Photo) > 0 {
		files = append(files, telegramFile{m.Photo[len(m.Photo)-1].FileID, "photo.jpg"})
	}
	if m.Document != nil {
		files = append(files, telegramFile{m.Document.FileID, m.Document.FileName})
	}
	if strings.TrimSpace(text) == "" && len(files) == 0 {
		return nil
	}
	c, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, chatID))
	if err != nil {
		return err
	}
	if c.ConnectorID == nil {
		return reply("⚠️ " + errChatConnectorMissing.Error())
	}
	kind, dir, err := s.chats.connectorConfig(*c.ConnectorID)
	if err != nil {
		return err
	}
	attachments := make([]ChatAttachment, 0, len(files))
	for _, file := range files {
		attachment, err := s.downloadTelegramFile(ctx, token, c, file.id, file.name)
		if errors.Is(err, errChatAttachmentType) || errors.Is(err, errChatAttachmentTooLarge) || errors.Is(err, errChatAttachmentEmpty) {
			s.removeTelegramFiles(c, attachments)
			return reply("Patio can't take this file: " + err.Error())
		}
		if err != nil {
			s.removeTelegramFiles(c, attachments)
			return err
		}
		attachments = append(attachments, attachment)
	}
	// Downloads can be slow: recheck that the bridge is still on and the sender still allowed.
	if latest, ok := s.telegramActive(token); !ok || !telegramAllowed(latest, m.From.ID) {
		s.removeTelegramFiles(c, attachments)
		return errTelegramAborted
	}
	s.attachmentsMu.Lock()
	message, queued, err := s.sendOrQueueChatMessage(c, kind, dir, text, attachments, nil)
	s.attachmentsMu.Unlock()
	if queued {
		_, err = s.queueChatMessage(chatID, text, attachments)
		return err
	}
	if err == nil && message.ID != "" {
		go s.telegramTyping(ctx, token, m.Chat.ID, chatID)
	}
	return err
}

// downloadTelegramFile stores a Telegram photo or document as a chat attachment, with the upload limits.
func (s *Server) downloadTelegramFile(ctx context.Context, token string, c chat, fileID, name string) (ChatAttachment, error) {
	var file struct {
		FilePath string `json:"file_path"`
	}
	if err := telegramCall(ctx, token, "getFile", map[string]any{"file_id": fileID}, &file); err != nil {
		return ChatAttachment{}, err
	}
	if name == "" {
		name = filepath.Base(file.FilePath)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(telegramFileBase, "/")+"/file/bot"+token+"/"+file.FilePath, nil)
	if err != nil {
		return ChatAttachment{}, redactTelegram(err, token)
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return ChatAttachment{}, redactTelegram(err, token)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatAttachment{}, fmt.Errorf("Telegram file download: %s", resp.Status)
	}
	return s.storeChatAttachment(c, name, resp.Body)
}

// createTelegramChat uses the app's chat creation, so it gets the same defaults (default bot
// with its connector and model) and validation.
func (s *Server) createTelegramChat(title string) (string, error) {
	c, _, err := s.newChat(createChatInput{Title: title})
	return c.ID, err
}

// mapTelegramChat points a Telegram chat at a Patio chat; created_at=now makes it the chat's newest
// mapping, which outbound replies use.
func (s *Server) mapTelegramChat(telegramID int64, chatID string) error {
	_, err := s.db.Exec(`INSERT INTO telegram_chat(telegram_chat_id,chat_id,created_at) VALUES(?,?,?) ON CONFLICT(telegram_chat_id) DO UPDATE SET chat_id=excluded.chat_id,created_at=excluded.created_at`, telegramID, chatID, now())
	return err
}

var telegramChatIDPattern = regexp.MustCompile(`[0-9a-f]{32}`)

// continueTelegramChat lists recent Patio chats as buttons, or switches to the chat whose id or link is given.
func (s *Server) continueTelegramChat(ctx context.Context, token string, telegramID int64, arg string) error {
	if arg == "" {
		rows, err := s.db.Query(`SELECT id,title FROM chat ORDER BY updated_at DESC LIMIT 8`)
		if err != nil {
			return err
		}
		var keyboard [][]any
		for rows.Next() {
			var id, title string
			if err := rows.Scan(&id, &title); err != nil {
				rows.Close()
				return err
			}
			keyboard = append(keyboard, []any{telegramButton(truncateTelegram(title, 40), "c:"+id)})
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		if len(keyboard) == 0 {
			return telegramCallMarkdown(ctx, token, "sendMessage", map[string]any{"chat_id": telegramID}, "No Patio chats yet.", nil)
		}
		return telegramCallMarkdown(ctx, token, "sendMessage", map[string]any{"chat_id": telegramID, "reply_markup": map[string]any{"inline_keyboard": keyboard}}, "Pick a Patio chat to continue:", nil)
	}
	text, err := s.switchTelegramChat(telegramID, telegramChatIDPattern.FindString(arg))
	if err != nil {
		return err
	}
	return telegramCallMarkdown(ctx, token, "sendMessage", map[string]any{"chat_id": telegramID}, text, nil)
}

// switchTelegramChat remaps a Telegram chat to an existing Patio chat and returns the reply text.
func (s *Server) switchTelegramChat(telegramID int64, chatID string) (string, error) {
	var title string
	err := s.db.QueryRow(`SELECT title FROM chat WHERE id=?`, chatID).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return "Chat not found.", nil
	}
	if err != nil {
		return "", err
	}
	var mapped int
	if err := s.db.QueryRow(`SELECT count(*) FROM telegram_chat WHERE chat_id=?`, chatID).Scan(&mapped); err != nil {
		return "", err
	}
	if err := s.mapTelegramChat(telegramID, chatID); err != nil {
		return "", err
	}
	// Drivers apply ChatPrompt only when a session starts, so a chat new to Telegram gets the note on its next turn.
	if mapped == 0 {
		s.chats.turnNotes.Store(chatID, telegramPrompt)
	}
	return "Continuing: " + title, nil
}

func (s *Server) removeTelegramFiles(c chat, attachments []ChatAttachment) {
	for _, attachment := range attachments {
		if path, err := s.attachmentPath(c, attachment.Name); err == nil {
			_ = os.Remove(path)
		}
	}
}

// telegramTyping keeps Telegram's typing status on (it clears after 5s) while the chat's turn runs.
func (s *Server) telegramTyping(ctx context.Context, token string, telegramID int64, chatID string) {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	deadline := time.After(30 * time.Minute)
	for s.chats.Running(chatID) {
		if err := telegramCall(ctx, token, "sendChatAction", map[string]any{"chat_id": telegramID, "action": "typing"}, nil); err != nil && ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case <-ticker.C:
		}
	}
}

func telegramSend(ctx context.Context, token string, chatID int64, text string) error {
	for _, chunk := range telegramChunks(text) {
		if err := telegramCallMarkdown(ctx, token, "sendMessage", map[string]any{"chat_id": chatID}, chunk, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) sendTelegramResult(chatID, text string, turnErr error) {
	var telegramID int64
	if err := s.db.QueryRow(`SELECT telegram_chat_id FROM telegram_chat WHERE chat_id=? ORDER BY created_at DESC LIMIT 1`, chatID).Scan(&telegramID); err != nil {
		return
	}
	cfg, err := s.readTelegramSettings()
	if err != nil || !cfg.Enabled {
		return
	}
	token, err := telegramToken()
	if err != nil {
		return
	}
	if turnErr != nil {
		text = "⚠️ " + turnErr.Error()
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := telegramSend(ctx, token, telegramID, text); err != nil {
		log.Printf("send Telegram reply for chat %s: %v", chatID, err)
	}
}

// telegramNoticeLoop scans the database rather than hooking requestApproval, because the MCP
// subprocess also creates approvals.
func (s *Server) telegramNoticeLoop(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cfg, err := s.readTelegramSettings()
			if err != nil || !cfg.Enabled {
				continue
			}
			token, err := telegramToken()
			if err != nil || token == "" {
				continue
			}
			if err := s.scanTelegramNotices(ctx, token); err != nil {
				log.Printf("Telegram notices: %v", err)
			}
		}
	}
}

// scanTelegramNotices sends each pending approval and open question of a mapped chat once.
func (s *Server) scanTelegramNotices(ctx context.Context, token string) error {
	s.pruneTelegramPicks()
	type notice struct {
		telegramID      int64
		id, tool, input string
	}
	rows, err := s.db.Query(`SELECT t.telegram_chat_id,a.id,a.tool_name,a.input_json FROM chat_approval a JOIN telegram_chat t ON t.chat_id=a.chat_id
		WHERE a.status='pending' AND NOT EXISTS(SELECT 1 FROM telegram_notice n WHERE n.item_id=a.id)
		UNION ALL SELECT t.telegram_chat_id,m.id,'',m.content FROM chat_message m JOIN telegram_chat t ON t.chat_id=m.chat_id
		WHERE m.role='question' AND m.answered=0 AND NOT EXISTS(SELECT 1 FROM telegram_notice n WHERE n.item_id=m.id)`)
	if err != nil {
		return err
	}
	var notices []notice
	for rows.Next() {
		var n notice
		if err := rows.Scan(&n.telegramID, &n.id, &n.tool, &n.input); err != nil {
			rows.Close()
			return err
		}
		notices = append(notices, n)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, n := range notices {
		if n.tool != "" {
			keyboard := [][]any{{telegramButton("Allow", "a:"+n.id+":1"), telegramButton("Deny", "a:"+n.id+":0")}}
			err = s.sendTelegramNotice(ctx, token, n.telegramID, n.id, "Approve `"+n.tool+"`?\n```\n"+truncateTelegram(n.input, 500)+"\n```", keyboard)
		} else {
			err = s.sendTelegramQuestion(ctx, token, n.telegramID, n.id, n.input)
		}
		if err != nil {
			log.Printf("Telegram notice %s: %v", n.id, err)
		}
	}
	return nil
}

func (s *Server) sendTelegramNotice(ctx context.Context, token string, telegramID int64, itemID, text string, keyboard [][]any) error {
	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	if err := telegramCallMarkdown(ctx, token, "sendMessage", map[string]any{"chat_id": telegramID, "reply_markup": map[string]any{"inline_keyboard": keyboard}}, text, &sent); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO telegram_notice(item_id,telegram_chat_id,message_id,created_at) VALUES(?,?,?,?)`, itemID, telegramID, sent.MessageID, now())
	return err
}

// sendTelegramQuestion sends one message per question (item "<message id>#<index>"), then marks the
// whole ask sent with a "<message id>" row so a failed send is retried for the missing questions only.
func (s *Server) sendTelegramQuestion(ctx context.Context, token string, telegramID int64, messageID, content string) error {
	var data telegramQuestions
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return err
	}
	for qi := range data.Questions {
		item := fmt.Sprintf("%s#%d", messageID, qi)
		var exists int
		if err := s.db.QueryRow(`SELECT 1 FROM telegram_notice WHERE item_id=?`, item).Scan(&exists); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		q := data.Questions[qi]
		text := q.Header
		if q.Question != "" {
			text += "\n" + q.Question
		}
		text += "\nTap an option, or reply with text to answer something else."
		if q.Multiple {
			text += " Pick one or more, then Done."
		}
		if err := s.sendTelegramNotice(ctx, token, telegramID, item, text, telegramOptions(item, q.Options, nil, q.Multiple)); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO telegram_notice(item_id,telegram_chat_id,message_id,created_at) VALUES(?,?,0,?)`, messageID, telegramID, now())
	return err
}

func telegramOptions(item string, options []struct {
	Label string `json:"label"`
}, picked map[string]bool, multiple bool) [][]any {
	keyboard := make([][]any, 0, len(options)+1)
	for oi, option := range options {
		label := option.Label
		if picked[label] {
			label = "✓ " + label
		}
		keyboard = append(keyboard, []any{telegramButton(label, fmt.Sprintf("q:%s:%d", item, oi))})
	}
	if multiple {
		keyboard = append(keyboard, []any{telegramButton("Done", "q:"+item+":d")})
	}
	return keyboard
}

func telegramButton(text, data string) map[string]string {
	return map[string]string{"text": text, "callback_data": data}
}

func truncateTelegram(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (s *Server) processTelegramCallback(ctx context.Context, token string, cfg telegramSettings, q *telegramCallback) error {
	toast := ""
	defer func() {
		if err := telegramCall(ctx, token, "answerCallbackQuery", map[string]any{"callback_query_id": q.ID, "text": toast}, nil); err != nil {
			log.Printf("answer Telegram callback: %v", err)
		}
	}()
	if !telegramAllowed(cfg, q.From.ID) {
		toast = "Not allowed"
		return nil
	}
	if q.Message == nil {
		return nil
	}
	edit := func(text string) error {
		return telegramCallMarkdown(ctx, token, "editMessageText", map[string]any{"chat_id": q.Message.Chat.ID, "message_id": q.Message.MessageID}, text, nil)
	}
	if chatID, ok := strings.CutPrefix(q.Data, "c:"); ok {
		text, err := s.switchTelegramChat(q.Message.Chat.ID, chatID)
		if err != nil {
			return err
		}
		return edit(text)
	}
	parts := strings.Split(q.Data, ":")
	if len(parts) != 3 {
		toast = "Unknown action"
		return nil
	}
	if parts[0] == "a" {
		var chatID string
		err := s.db.QueryRow(`SELECT a.chat_id FROM chat_approval a JOIN telegram_chat t ON t.chat_id=a.chat_id WHERE a.id=? AND t.telegram_chat_id=?`, parts[1], q.Message.Chat.ID).Scan(&chatID)
		if errors.Is(err, sql.ErrNoRows) {
			return edit("Approval not found")
		}
		if err != nil {
			return err
		}
		status, err := s.answerApproval(chatID, parts[1], parts[2] == "1")
		switch {
		case errors.Is(err, errApprovalAnswered):
			return edit("Already answered")
		case err != nil:
			return err
		case status == "allowed":
			return edit("✅ Allowed")
		default:
			return edit("❌ Denied")
		}
	}
	if parts[0] == "q" {
		return s.pickTelegramOption(ctx, token, q, parts[1], parts[2], &toast)
	}
	toast = "Unknown action"
	return nil
}

// pickTelegramOption records a button tap and, once every question has a pick, answers the ask_user.
func (s *Server) pickTelegramOption(ctx context.Context, token string, q *telegramCallback, item, choice string, toast *string) error {
	messageID, index, ok := strings.Cut(item, "#")
	qi, err := strconv.Atoi(index)
	if !ok || err != nil {
		*toast = "Unknown action"
		return nil
	}
	var chatID, content string
	var answered bool
	err = s.db.QueryRow(`SELECT m.chat_id,m.content,m.answered FROM chat_message m JOIN telegram_chat t ON t.chat_id=m.chat_id WHERE m.id=? AND m.role='question' AND t.telegram_chat_id=?`, messageID, q.Message.Chat.ID).Scan(&chatID, &content, &answered)
	if errors.Is(err, sql.ErrNoRows) {
		*toast = "Question not found"
		return nil
	}
	if err != nil {
		return err
	}
	var data telegramQuestions
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return err
	}
	if qi < 0 || qi >= len(data.Questions) {
		*toast = "Unknown action"
		return nil
	}
	question := data.Questions[qi]
	if answered {
		s.forgetTelegramPicks(messageID)
		return telegramCallMarkdown(ctx, token, "editMessageText", map[string]any{"chat_id": q.Message.Chat.ID, "message_id": q.Message.MessageID}, "Already answered", nil)
	}
	s.telegramPicksMu.Lock()
	if s.telegramPicks == nil {
		s.telegramPicks = map[string]map[int]*telegramPick{}
	}
	if s.telegramPicks[messageID] == nil {
		s.telegramPicks[messageID] = map[int]*telegramPick{}
	}
	pick := s.telegramPicks[messageID][qi]
	if pick == nil {
		pick = &telegramPick{labels: map[string]bool{}}
		s.telegramPicks[messageID][qi] = pick
	}
	if choice == "d" {
		if len(pick.labels) == 0 {
			s.telegramPicksMu.Unlock()
			*toast = "Pick at least one"
			return nil
		}
		pick.done = true
	} else {
		oi, err := strconv.Atoi(choice)
		if err != nil || oi < 0 || oi >= len(question.Options) {
			s.telegramPicksMu.Unlock()
			*toast = "Unknown action"
			return nil
		}
		label := question.Options[oi].Label
		if question.Multiple {
			if pick.labels[label] {
				delete(pick.labels, label)
			} else {
				pick.labels[label] = true
			}
			pick.done = false
		} else {
			pick.labels = map[string]bool{label: true}
			pick.done = true
		}
	}
	done := pick.done
	picked := make(map[string]bool, len(pick.labels))
	for label := range pick.labels {
		picked[label] = true
	}
	var answers []questionAnswer
	var missing []string
	for i, other := range data.Questions {
		p := s.telegramPicks[messageID][i]
		if p == nil || !p.done {
			missing = append(missing, other.Header)
			continue
		}
		answer := questionAnswer{Header: other.Header}
		for _, option := range other.Options {
			if p.labels[option.Label] {
				answer.Selected = append(answer.Selected, option.Label)
			}
		}
		answers = append(answers, answer)
	}
	s.telegramPicksMu.Unlock()
	chatTarget := q.Message.Chat.ID
	// Until the whole ask is sent, keep every keyboard so picks can change (and be redone after a restart).
	if len(missing) > 0 {
		if done {
			*toast = "Saved. Still to answer: " + strings.Join(missing, ", ")
		}
		return telegramCall(ctx, token, "editMessageReplyMarkup", map[string]any{"chat_id": chatTarget, "message_id": q.Message.MessageID, "reply_markup": map[string]any{"inline_keyboard": telegramOptions(item, question.Options, picked, question.Multiple)}}, nil)
	}
	_, _, err = s.answerQuestion(chatID, messageID, answers)
	if err != nil && !errors.Is(err, errQuestionAnswered) {
		*toast = err.Error()
		return nil
	}
	s.forgetTelegramPicks(messageID)
	for i, answer := range answers {
		var telegramMessageID int64
		if err := s.db.QueryRow(`SELECT message_id FROM telegram_notice WHERE item_id=?`, fmt.Sprintf("%s#%d", messageID, i)).Scan(&telegramMessageID); err != nil {
			continue
		}
		text := answer.Header + ": " + strings.Join(answer.Selected, ", ")
		if err != nil {
			text = "Already answered"
		}
		if editErr := telegramCallMarkdown(ctx, token, "editMessageText", map[string]any{"chat_id": chatTarget, "message_id": telegramMessageID}, text, nil); editErr != nil {
			log.Printf("edit Telegram question: %v", editErr)
		}
	}
	return nil
}

// pruneTelegramPicks drops picks of questions answered another way (text reply, the app) or deleted.
func (s *Server) pruneTelegramPicks() {
	s.telegramPicksMu.Lock()
	ids := make([]string, 0, len(s.telegramPicks))
	for id := range s.telegramPicks {
		ids = append(ids, id)
	}
	s.telegramPicksMu.Unlock()
	for _, id := range ids {
		var open int
		err := s.db.QueryRow(`SELECT 1 FROM chat_message WHERE id=? AND answered=0`, id).Scan(&open)
		if errors.Is(err, sql.ErrNoRows) {
			s.forgetTelegramPicks(id)
		} else if err != nil {
			log.Printf("prune Telegram picks: %v", err)
		}
	}
}

func (s *Server) forgetTelegramPicks(messageID string) {
	s.telegramPicksMu.Lock()
	delete(s.telegramPicks, messageID)
	s.telegramPicksMu.Unlock()
}
