package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

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
			// Notices are intentionally scanned only while bridge is configured.
			_ = token
		}
	}
}

type telegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64  `json:"message_id"`
		Text      string `json:"text"`
		Caption   string `json:"caption"`
		Photo     []struct {
			FileID   string `json:"file_id"`
			FileSize int64  `json:"file_size"`
		} `json:"photo"`
		Document *struct {
			FileID   string `json:"file_id"`
			FileName string `json:"file_name"`
			FileSize int64  `json:"file_size"`
		} `json:"document"`
		Chat struct {
			ID        int64  `json:"id"`
			FirstName string `json:"first_name"`
			Username  string `json:"username"`
		} `json:"chat"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
	} `json:"message"`
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
			if !strings.Contains(err.Error(), "409 Conflict") {
				log.Printf("Telegram getUpdates: %v", err)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-s.telegramWakeChannel():
				backoff = time.Second
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for _, update := range updates {
			latest, settingsErr := s.readTelegramSettings()
			if settingsErr != nil {
				log.Printf("read Telegram settings: %v", settingsErr)
				break
			}
			if update.Message != nil {
				if err := s.processTelegramMessage(ctx, token, latest, update); err != nil {
					log.Printf("process Telegram update %d: %v", update.UpdateID, err)
				}
			}
			if update.UpdateID >= cfg.UpdateOffset {
				cfg.UpdateOffset = update.UpdateID + 1
			}
		}
		latest, err := s.readTelegramSettings()
		if err != nil {
			log.Printf("read Telegram settings: %v", err)
			continue
		}
		if cfg.UpdateOffset > latest.UpdateOffset {
			latest.UpdateOffset = cfg.UpdateOffset
		}
		if err := s.saveTelegramSettings(latest); err != nil {
			log.Printf("save Telegram offset: %v", err)
		}
	}
}

func telegramAllowed(cfg telegramSettings, id int64) bool {
	for _, allowed := range cfg.AllowedUserIDs {
		if allowed == id {
			return true
		}
	}
	return false
}

func (s *Server) processTelegramMessage(ctx context.Context, token string, cfg telegramSettings, update telegramUpdate) error {
	m := update.Message
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
	if m.Text == "/chat" {
		var title string
		if err := s.db.QueryRow(`SELECT title FROM chat WHERE id=?`, chatID).Scan(&title); err != nil {
			return err
		}
		return reply(title + " (" + chatID + ")")
	}
	if m.Text == "/new" {
		return reply("Started a new Patio chat.")
	}
	text := m.Text
	if text == "" {
		text = m.Caption
	}
	var files []struct {
		id, name string
		size     int64
	}
	if len(m.Photo) > 0 {
		photo := m.Photo[len(m.Photo)-1]
		files = append(files, struct {
			id, name string
			size     int64
		}{photo.FileID, "photo.jpg", photo.FileSize})
	}
	if m.Document != nil {
		files = append(files, struct {
			id, name string
			size     int64
		}{m.Document.FileID, m.Document.FileName, m.Document.FileSize})
	}
	if strings.TrimSpace(text) == "" && len(files) == 0 {
		return nil
	}
	c, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, chatID))
	if err != nil {
		return err
	}
	kind, dir, err := s.chats.connectorConfig(*c.ConnectorID)
	if err != nil {
		return err
	}
	selected := make([]ChatAttachment, 0, len(files))
	selectedNames := make(map[string]bool, len(files))
	s.attachmentsMu.Lock()
	defer s.attachmentsMu.Unlock()
	for _, file := range files {
		if file.size > maxChatTextAttachment {
			return reply("Patio can't take this file: attachment exceeds size limit")
		}
		var result struct {
			FilePath string `json:"file_path"`
		}
		if err := telegramCall(ctx, token, "getFile", map[string]any{"file_id": file.id}, &result); err != nil {
			return err
		}
		if file.name == "" {
			file.name = filepath.Base(result.FilePath)
		}
		if file.name == "" || file.name == "." {
			file.name = "photo.jpg"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(telegramFileBase, "/")+"/file/bot"+token+"/"+result.FilePath, nil)
		if err != nil {
			return err
		}
		resp, err := (&http.Client{Timeout: 40 * time.Second}).Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return fmt.Errorf("Telegram file download: %s", resp.Status)
		}
		attachment, storeErr := s.storeChatAttachment(c, file.name, resp.Body)
		closeErr := resp.Body.Close()
		if err := errors.Join(storeErr, closeErr); err != nil {
			if errors.Is(err, errChatAttachmentType) || errors.Is(err, errChatAttachmentTooLarge) {
				return reply("Patio can't take this file: " + err.Error())
			}
			return err
		}
		if s.attachments[c.ID] == nil {
			s.attachments[c.ID] = make(map[string]ChatAttachment)
		}
		s.attachments[c.ID][attachment.Name] = attachment
		selected = append(selected, attachment)
		selectedNames[attachment.Name] = true
	}
	message, queued, err := s.sendOrQueueChatMessage(c, kind, dir, text, selected, selectedNames)
	if queued {
		_, err = s.queueChatMessage(chatID, text, selected)
		return err
	}
	if err == nil && message.ID != "" {
		go s.telegramTyping(ctx, token, m.Chat.ID, chatID)
	}
	return err
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

func (s *Server) telegramTyping(ctx context.Context, token string, telegramID int64, chatID string) {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Minute)
	defer deadline.Stop()
	for s.chats.Running(chatID) {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
		}
		if !s.chats.Running(chatID) {
			return
		}
		if err := telegramCall(ctx, token, "sendChatAction", map[string]any{"chat_id": telegramID, "action": "typing"}, nil); err != nil && ctx.Err() != nil {
			return
		}
	}
}

func telegramSend(ctx context.Context, token string, chatID int64, text string) error {
	for len([]rune(text)) > 4096 {
		runes := []rune(text)
		if err := telegramCall(ctx, token, "sendMessage", map[string]any{"chat_id": chatID, "text": string(runes[:4096])}, nil); err != nil {
			return err
		}
		text = string(runes[4096:])
	}
	return telegramCall(ctx, token, "sendMessage", map[string]any{"chat_id": chatID, "text": text}, nil)
}

func (s *Server) sendTelegramResult(chatID, text string, turnErr error) {
	var telegramID int64
	if err := s.db.QueryRow(`SELECT telegram_chat_id FROM telegram_chat WHERE chat_id=?`, chatID).Scan(&telegramID); err != nil {
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
