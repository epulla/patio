package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
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
		Chat      struct {
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
	if strings.TrimSpace(m.Text) == "" {
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
	_, err = s.chats.Send(c, kind, dir, ChatSendRequest{Text: m.Text})
	if errors.Is(err, errChatTurnRunning) || errors.Is(err, errChatTurnOtherWindow) {
		_, err = s.db.Exec(`INSERT INTO chat_queue(id,chat_id,text,attachments,created_at) VALUES(?,?,?,'[]',?)`, uid(), chatID, m.Text, now())
	}
	return err
}

func (s *Server) createTelegramChat(title string) (chat, error) {
	defaults, err := s.chatDefaults()
	if err != nil {
		return chat{}, err
	}
	var connectorID, kind string
	if defaults.ConnectorID != nil {
		connectorID = *defaults.ConnectorID
	} else if err = s.db.QueryRow(`SELECT id,kind FROM connector ORDER BY created_at,rowid LIMIT 1`).Scan(&connectorID, &kind); err != nil {
		return chat{}, err
	}
	if kind == "" {
		if err = s.db.QueryRow(`SELECT kind FROM connector WHERE id=?`, connectorID).Scan(&kind); err != nil {
			return chat{}, err
		}
	}
	c := chat{ID: uid(), ConnectorID: &connectorID, Title: title, Model: defaults.Model, Folder: defaults.Folder, CreatedAt: now(), UpdatedAt: now(), Thinking: defaults.Thinking, ShowThinking: defaults.ShowThinking, AutoAllow: defaults.AutoAllow, Capabilities: capabilitiesForKind(kind)}
	if defaults.DefaultBotID != "none" {
		c.BotID = &defaults.DefaultBotID
	}
	if c.Folder == nil {
		dir := s.chatWorkdir(c)
		if err = os.MkdirAll(dir, 0700); err != nil {
			return chat{}, err
		}
	}
	_, err = s.db.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at,thinking,show_thinking,auto_allow,bot_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, c.ID, c.ConnectorID, c.Title, c.Model, c.Folder, c.CreatedAt, c.UpdatedAt, c.Thinking, c.ShowThinking, c.AutoAllow, c.BotID)
	if err != nil {
		return chat{}, err
	}
	c.Workdir = s.chatWorkdir(c)
	return c, nil
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
