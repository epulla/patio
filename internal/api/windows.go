package api

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
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
	if s.openChat == nil {
		errw(w, 500, "chat windows unavailable")
		return
	}
	if err := s.openChat(id); err != nil {
		errw(w, 500, err.Error())
		return
	}
	noContent(w)
}

func (s *Server) openMainWindow(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path *string `json:"path"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		errw(w, 400, "valid JSON required")
		return
	}
	if len(bytes.TrimSpace(body)) != 0 {
		d := json.NewDecoder(bytes.NewReader(body))
		d.DisallowUnknownFields()
		if err := d.Decode(&in); err != nil {
			errw(w, 400, "valid JSON required")
			return
		}
	}
	focusPath := ""
	if in.Path != nil {
		focusPath = *in.Path
		if !ValidAppPath(focusPath) {
			errw(w, 400, "path must be an app path")
			return
		}
	}
	if s.showMain == nil {
		errw(w, 500, "main window unavailable")
		return
	}
	s.showMain(focusPath)
	noContent(w)
}

// ValidAppPath reports whether p is an in-app path that a window URL can load.
func ValidAppPath(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return false
	}
	u, err := url.Parse(p)
	return err == nil && u.Scheme == "" && u.Host == ""
}

func nowMinusWindowStale() string {
	return time.Now().UTC().Add(-15 * time.Second).Format("2006-01-02T15:04:05.000Z")
}

// RegisterWindow records this process so chat_turn ownership and stale-turn cleanup keep working.
func (s *Server) RegisterWindow(pid int) error {
	_, err := s.db.Exec(`INSERT INTO app_window(id,pid,heartbeat_at) VALUES(?,?,?)`, s.windowID, pid, now())
	return err
}

func (s *Server) RunWindowHeartbeat(stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if _, err := s.db.Exec(`UPDATE app_window SET heartbeat_at=? WHERE id=?`, now(), s.windowID); err != nil {
					log.Printf("update app window heartbeat: %v", err)
				}
			}
		}
	}()
}

func (s *Server) UnregisterWindow() error {
	_, err := s.db.Exec(`DELETE FROM app_window WHERE id=?`, s.windowID)
	return err
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
