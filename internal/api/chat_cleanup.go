package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type chatCleanupSettings struct {
	Enabled bool `json:"enabled"`
	Days    int  `json:"days"`
}

const maxChatCleanupDays = 3650

func (s *Server) chatCleanup() (chatCleanupSettings, error) {
	out := chatCleanupSettings{Days: 7}
	for key, target := range map[string]any{"chat_cleanup": &out.Enabled, "chat_cleanup_days": &out.Days} {
		var raw string
		err := s.db.QueryRow(`SELECT value FROM app_setting WHERE key=?`, key).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return out, err
		}
		if err := json.Unmarshal([]byte(raw), target); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *Server) getChatCleanup(w http.ResponseWriter, _ *http.Request) {
	settings, err := s.chatCleanup()
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, settings)
}

func (s *Server) putChatCleanup(w http.ResponseWriter, r *http.Request) {
	var in chatCleanupSettings
	if decode(r, &in) != nil || in.Days < 1 || in.Days > maxChatCleanupDays {
		errw(w, http.StatusBadRequest, "days must be between 1 and 3650")
		return
	}
	if err := txrun(s.db, func(tx *sql.Tx) error {
		for key, value := range map[string]any{"chat_cleanup": in.Enabled, "chat_cleanup_days": in.Days} {
			data, err := json.Marshal(value)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO app_setting(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, string(data)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, in)
}

// runChatCleanup deletes chats whose last message is older than the configured
// number of days. The caller passes pinned chats in keep, since pins live in the UI.
func (s *Server) runChatCleanup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Keep []string `json:"keep"`
	}
	if decode(r, &in) != nil {
		errw(w, http.StatusBadRequest, "invalid cleanup JSON")
		return
	}
	settings, err := s.chatCleanup()
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	deleted := 0
	if settings.Enabled {
		deleted, err = s.cleanupChats(settings.Days, in.Keep)
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	write(w, http.StatusOK, map[string]int{"deleted": deleted})
}

func (s *Server) cleanupChats(days int, keep []string) (int, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02T15:04:05.000Z")
	rows, err := s.db.Query(`SELECT c.id FROM chat c
		WHERE COALESCE((SELECT MAX(m.created_at) FROM chat_message m WHERE m.chat_id=c.id), c.created_at) < ?
		AND NOT EXISTS(SELECT 1 FROM chat_turn t JOIN app_window w ON w.id=t.window_id WHERE t.chat_id=c.id AND w.heartbeat_at>=?)`,
		cutoff, nowMinusWindowStale())
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	kept := make(map[string]bool, len(keep))
	for _, id := range keep {
		kept[id] = true
	}
	deleted := 0
	for _, id := range ids {
		if kept[id] || s.chats.Running(id) {
			continue
		}
		if err := s.removeChat(id); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
