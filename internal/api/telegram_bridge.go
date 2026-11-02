package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
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
	go s.telegramNoticeLoop(ctx)
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
			case <-s.telegramWakeChannel():
				continue
			case <-time.After(time.Second):
				continue
			}
		}
		var updates []telegramUpdate
		pollCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		wake := make(chan struct{})
		go func() {
			select {
			case <-s.telegramWakeChannel():
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
			// 409: another Patio polls the same bot; back off quietly instead of spamming the log.
			if !strings.Contains(err.Error(), "Conflict") || backoff == time.Second {
				log.Printf("Telegram getUpdates: %v", err)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-s.telegramWakeChannel():
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
		if len(updates) == 0 {
			continue
		}
		latest, err := s.readTelegramSettings()
		if err != nil {
			log.Printf("read Telegram settings: %v", err)
			continue
		}
		for _, update := range updates {
			if err := s.processTelegramUpdate(ctx, token, latest, update); err != nil {
				log.Printf("process Telegram update %d: %v", update.UpdateID, err)
			}
			if update.UpdateID >= latest.UpdateOffset {
				latest.UpdateOffset = update.UpdateID + 1
			}
		}
		if err := s.saveTelegramOffset(latest.UpdateOffset); err != nil {
			log.Printf("save Telegram offset: %v", err)
		}
	}
}

// saveTelegramOffset re-reads settings so a Settings save during processing isn't overwritten.
func (s *Server) saveTelegramOffset(offset int64) error {
	cfg, err := s.readTelegramSettings()
	if err != nil {
		return err
	}
	if offset <= cfg.UpdateOffset {
		return nil
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
		return telegramCall(ctx, token, "sendMessage", map[string]any{"chat_id": m.Chat.ID, "text": text}, nil)
	}
	if !telegramAllowed(cfg, m.From.ID) {
		return reply(fmt.Sprintf("Not allowed. Your Telegram ID is %d: add it in Patio Settings › Telegram.", m.From.ID))
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
		c, err := s.createTelegramChat("Telegram: " + name)
		if err != nil {
			return err
		}
		chatID = c.ID
		if _, err := s.db.Exec(`INSERT INTO telegram_chat(telegram_chat_id,chat_id,created_at) VALUES(?,?,?) ON CONFLICT(telegram_chat_id) DO UPDATE SET chat_id=excluded.chat_id`, m.Chat.ID, chatID, now()); err != nil {
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
		return reply(title + " (" + chatID + ")")
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
		if errors.Is(err, errChatAttachmentType) || errors.Is(err, errChatAttachmentTooLarge) {
			return reply("Patio can't take this file: " + err.Error())
		}
		if err != nil {
			return err
		}
		attachments = append(attachments, attachment)
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
		return ChatAttachment{}, err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return ChatAttachment{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatAttachment{}, fmt.Errorf("Telegram file download: %s", resp.Status)
	}
	return s.storeChatAttachment(c, name, resp.Body)
}

func (s *Server) createTelegramChat(title string) (chat, error) {
	defaults, err := s.chatDefaults()
	if err != nil {
		return chat{}, err
	}
	connectorID := defaults.ConnectorID
	var id, kind string
	if connectorID == nil {
		if err := s.db.QueryRow(`SELECT id,kind FROM connector ORDER BY created_at,rowid LIMIT 1`).Scan(&id, &kind); err != nil {
			return chat{}, err
		}
		connectorID = &id
	} else if err := s.db.QueryRow(`SELECT kind FROM connector WHERE id=?`, *connectorID).Scan(&kind); err != nil {
		return chat{}, err
	}
	var bot *string
	if defaults.DefaultBotID != "none" {
		bot = &defaults.DefaultBotID
	}
	return s.createChatFromDefaults(chat{ConnectorID: connectorID, Title: title, Model: defaults.Model, Folder: defaults.Folder, Thinking: defaults.Thinking, ShowThinking: defaults.ShowThinking, AutoAllow: defaults.AutoAllow, BotID: bot}, kind)
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
	runes := []rune(text)
	for len(runes) > 4096 {
		if err := telegramCall(ctx, token, "sendMessage", map[string]any{"chat_id": chatID, "text": string(runes[:4096])}, nil); err != nil {
			return err
		}
		runes = runes[4096:]
	}
	return telegramCall(ctx, token, "sendMessage", map[string]any{"chat_id": chatID, "text": string(runes)}, nil)
}

func (s *Server) sendTelegramResult(chatID, text string, turnErr error) {
	var telegramID int64
	if err := s.db.QueryRow(`SELECT telegram_chat_id FROM telegram_chat WHERE chat_id=?`, chatID).Scan(&telegramID); err != nil {
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
			err = s.sendTelegramNotice(ctx, token, n.telegramID, n.id, "Approve "+n.tool+"?\n"+truncateTelegram(n.input, 500), keyboard)
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
	if err := telegramCall(ctx, token, "sendMessage", map[string]any{"chat_id": telegramID, "text": text, "reply_markup": map[string]any{"inline_keyboard": keyboard}}, &sent); err != nil {
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
		return telegramCall(ctx, token, "editMessageText", map[string]any{"chat_id": q.Message.Chat.ID, "message_id": q.Message.MessageID, "text": text}, nil)
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
		return telegramCall(ctx, token, "editMessageText", map[string]any{"chat_id": q.Message.Chat.ID, "message_id": q.Message.MessageID, "text": "Already answered"}, nil)
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
		} else {
			pick.labels = map[string]bool{label: true}
			pick.done = true
		}
	}
	picked := make(map[string]bool, len(pick.labels))
	for label := range pick.labels {
		picked[label] = true
	}
	done := pick.done
	var answers []questionAnswer
	complete := true
	for i, other := range data.Questions {
		p := s.telegramPicks[messageID][i]
		if p == nil || !p.done {
			complete = false
			break
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
	if !done {
		return telegramCall(ctx, token, "editMessageReplyMarkup", map[string]any{"chat_id": chatTarget, "message_id": q.Message.MessageID, "reply_markup": map[string]any{"inline_keyboard": telegramOptions(item, question.Options, picked, true)}}, nil)
	}
	if !complete {
		var labels []string
		for _, option := range question.Options {
			if picked[option.Label] {
				labels = append(labels, option.Label)
			}
		}
		*toast = "Saved. Answer the other questions to send."
		return telegramCall(ctx, token, "editMessageText", map[string]any{"chat_id": chatTarget, "message_id": q.Message.MessageID, "text": question.Header + ": " + strings.Join(labels, ", ")}, nil)
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
		if editErr := telegramCall(ctx, token, "editMessageText", map[string]any{"chat_id": chatTarget, "message_id": telegramMessageID, "text": text}, nil); editErr != nil {
			log.Printf("edit Telegram question: %v", editErr)
		}
	}
	return nil
}

func (s *Server) forgetTelegramPicks(messageID string) {
	s.telegramPicksMu.Lock()
	delete(s.telegramPicks, messageID)
	s.telegramPicksMu.Unlock()
}
