package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const telegramTokenKey = "telegram:bot_token"

type telegramSettings struct {
	Enabled        bool    `json:"enabled"`
	AllowedUserIDs []int64 `json:"allowed_user_ids"`
	UpdateOffset   int64   `json:"update_offset"`
}

var telegramAPIBase = "https://api.telegram.org"
var telegramFileBase = "https://api.telegram.org"

func (s *Server) readTelegramSettings() (telegramSettings, error) {
	var cfg telegramSettings
	var raw string
	err := s.db.QueryRow(`SELECT value FROM app_setting WHERE key='telegram'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return telegramSettings{AllowedUserIDs: []int64{}}, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, err
	}
	if cfg.AllowedUserIDs == nil {
		cfg.AllowedUserIDs = []int64{}
	}
	return cfg, nil
}

func (s *Server) saveTelegramSettings(cfg telegramSettings) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO app_setting(key,value) VALUES('telegram',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw))
	return err
}

func (s *Server) telegramSettingsResponse(cfg telegramSettings) map[string]any {
	_, err := keyring.Get(connectorService, telegramTokenKey)
	return map[string]any{"enabled": cfg.Enabled, "allowed_user_ids": cfg.AllowedUserIDs, "has_token": err == nil}
}

func (s *Server) getTelegramSettings(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.readTelegramSettings()
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, s.telegramSettingsResponse(cfg))
}

func (s *Server) putTelegramSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled        bool            `json:"enabled"`
		AllowedUserIDs []int64         `json:"allowed_user_ids"`
		Token          json.RawMessage `json:"token"`
	}
	if decode(r, &in) != nil {
		errw(w, 400, "valid Telegram settings required")
		return
	}
	s.telegramMu.Lock()
	defer s.telegramMu.Unlock()
	cfg, err := s.readTelegramSettings()
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if len(in.Token) > 0 && string(in.Token) != "null" {
		var token string
		if json.Unmarshal(in.Token, &token) != nil {
			errw(w, 400, "token must be a string or null")
			return
		}
		// Update ids belong to one bot, so a new or removed token starts over.
		if old, _ := telegramToken(); old != token {
			cfg.UpdateOffset = 0
		}
		if token == "" {
			if err := keyring.Delete(connectorService, telegramTokenKey); err != nil && !errors.Is(err, keyring.ErrNotFound) {
				errw(w, 500, err.Error())
				return
			}
		} else if err := keyring.Set(connectorService, telegramTokenKey, token); err != nil {
			errw(w, 500, err.Error())
			return
		}
	}
	cfg.Enabled, cfg.AllowedUserIDs = in.Enabled, in.AllowedUserIDs
	if cfg.AllowedUserIDs == nil {
		cfg.AllowedUserIDs = []int64{}
	}
	if err := s.saveTelegramSettings(cfg); err != nil {
		errw(w, 500, err.Error())
		return
	}
	select {
	case s.telegramWake <- struct{}{}:
	default:
	}
	write(w, 200, s.telegramSettingsResponse(cfg))
}

func (s *Server) testTelegramToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token json.RawMessage `json:"token"`
	}
	if decode(r, &in) != nil {
		errw(w, 400, "valid token required")
		return
	}
	token := ""
	if len(in.Token) > 0 && string(in.Token) != "null" {
		if json.Unmarshal(in.Token, &token) != nil {
			errw(w, 400, "token must be a string or null")
			return
		}
	}
	if token == "" {
		token, _ = keyring.Get(connectorService, telegramTokenKey)
	}
	if token == "" {
		errw(w, 400, "bot token required")
		return
	}
	var result struct {
		Username string `json:"username"`
	}
	if err := telegramCall(r.Context(), token, "getMe", nil, &result); err != nil {
		errw(w, 502, err.Error())
		return
	}
	write(w, 200, map[string]string{"username": result.Username})
}

type telegramEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
}

// telegramError hides the bot token, which net/http puts in request URLs of transport errors.
type telegramError struct {
	message string
	err     error
}

func (e telegramError) Error() string { return e.message }
func (e telegramError) Unwrap() error { return e.err }

func redactTelegram(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	return telegramError{strings.ReplaceAll(err.Error(), token, "<token>"), err}
}

func telegramCall(ctx context.Context, token, method string, payload any, out any) (err error) {
	defer func() { err = redactTelegram(err, token) }()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(telegramAPIBase, "/")+"/bot"+token+"/"+method, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var envelope telegramEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !envelope.OK {
		if envelope.Description == "" {
			envelope.Description = resp.Status
		}
		return fmt.Errorf("Telegram %s: %s", method, envelope.Description)
	}
	if out != nil {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}

// telegramCallMarkdown sends payload with md rendered as Telegram HTML; if Telegram rejects the
// markup, it resends md as plain text.
func telegramCallMarkdown(ctx context.Context, token, method string, payload map[string]any, md string, out any) error {
	payload["text"], payload["parse_mode"] = telegramHTML(md), "HTML"
	err := telegramCall(ctx, token, method, payload, out)
	if err == nil || !strings.Contains(err.Error(), "can't parse entities") {
		return err
	}
	delete(payload, "parse_mode")
	payload["text"] = telegramPlain(md)
	return telegramCall(ctx, token, method, payload, out)
}

func telegramToken() (string, error) { return keyring.Get(connectorService, telegramTokenKey) }
