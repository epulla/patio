package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"time"
)

func (s *Server) openChatWindow(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM chat WHERE id=?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "chat not found")
		return
	} else if err != nil {
		errw(w, 500, err.Error())
		return
	}
	var windowID string
	err := s.db.QueryRow(`SELECT id FROM app_window WHERE chat_id=? AND heartbeat_at>=? ORDER BY heartbeat_at DESC LIMIT 1`, id, nowMinusWindowStale()).Scan(&windowID)
	if err == nil {
		if _, err := s.db.Exec(`UPDATE app_window SET focus_at=? WHERE id=?`, now(), windowID); err != nil {
			errw(w, 500, err.Error())
			return
		}
		noContent(w)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		errw(w, 500, err.Error())
		return
	}
	if s.startWindow == nil {
		errw(w, 500, "window process starter unavailable")
		return
	}
	if err := s.startWindow(append([]string{"-chat", id}, dbArgs(s.dbPath, s.showHidden)...)); err != nil {
		errw(w, 500, err.Error())
		return
	}
	noContent(w)
}

func (s *Server) openMainWindow(w http.ResponseWriter, _ *http.Request) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM app_window WHERE chat_id IS NULL AND heartbeat_at>=? ORDER BY heartbeat_at DESC LIMIT 1`, nowMinusWindowStale()).Scan(&id)
	if err == nil {
		if _, err := s.db.Exec(`UPDATE app_window SET focus_at=? WHERE id=?`, now(), id); err != nil {
			errw(w, 500, err.Error())
			return
		}
		noContent(w)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		errw(w, 500, err.Error())
		return
	}
	if s.startWindow == nil {
		errw(w, 500, "window process starter unavailable")
		return
	}
	if err := s.startWindow(dbArgs(s.dbPath, s.showHidden)); err != nil {
		errw(w, 500, err.Error())
		return
	}
	noContent(w)
}

func dbArgs(path string, hidden bool) []string {
	args := []string{"-d", path}
	if hidden {
		args = append(args, "-show-hidden")
	}
	return args
}

func nowMinusWindowStale() string {
	return time.Now().UTC().Add(-15 * time.Second).Format("2006-01-02T15:04:05.000Z")
}

func (s *Server) RegisterWindow(chatID string, pid int) error {
	_, err := s.db.Exec(`INSERT INTO app_window(id,chat_id,pid,heartbeat_at) VALUES(?,?,?,?)`, s.windowID, nullableChat(chatID), pid, now())
	return err
}

func nullableChat(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func (s *Server) RunWindowHeartbeat(stop <-chan struct{}, focus func()) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		var seen sql.NullString
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if _, err := s.db.Exec(`UPDATE app_window SET heartbeat_at=? WHERE id=?`, now(), s.windowID); err != nil {
					log.Printf("update app window heartbeat: %v", err)
					continue
				}
				var value sql.NullString
				if err := s.db.QueryRow(`SELECT focus_at FROM app_window WHERE id=?`, s.windowID).Scan(&value); err != nil {
					log.Printf("read app window focus request: %v", err)
					continue
				}
				if value.Valid && (!seen.Valid || value.String != seen.String) {
					focus()
					seen = value
				}
			}
		}
	}()
}

func (s *Server) UnregisterWindow() error {
	_, err := s.db.Exec(`DELETE FROM app_window WHERE id=?`, s.windowID)
	return err
}

func (s *Server) FocusCurrentWindow() {
	if s.focusWindow != nil {
		s.focusWindow()
	}
}

func NewWindowID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return hex.EncodeToString(b[:])
}
