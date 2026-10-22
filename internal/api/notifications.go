package api

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type notification struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Link      string  `json:"link"`
	CreatedAt string  `json:"created_at"`
	ReadAt    *string `json:"read_at"`
}

func (s *Server) Notify(kind, title, body, link string, native bool) error {
	n, err := s.insertNotification(kind, title, body, link)
	if err != nil {
		return err
	}
	if native && s.notify != nil {
		return s.notify(n.Title, n.Body, n.Link)
	}
	return nil
}

func (s *Server) insertNotification(kind, title, body, link string) (notification, error) {
	n := notification{ID: uid(), Kind: kind, Title: notificationText(s.db, title), Body: notificationText(s.db, body), Link: link, CreatedAt: now()}
	_, err := s.db.Exec(`INSERT INTO notification(id,kind,title,body,link,created_at) VALUES(?,?,?,?,?,?)`, n.ID, n.Kind, n.Title, n.Body, n.Link, n.CreatedAt)
	return n, err
}

// createNotification stores a custom notification. The app pops it as a toast, or natively when Patio is in the background.
func (s *Server) createNotification(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Link  string `json:"link"`
	}
	if err := decode(r, &in); err != nil {
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Title, in.Link = strings.TrimSpace(in.Title), strings.TrimSpace(in.Link)
	switch {
	case in.Title == "":
		errw(w, http.StatusBadRequest, "title is required")
		return
	case utf8.RuneCountInString(in.Title) > 200:
		errw(w, http.StatusBadRequest, "title must be at most 200 characters")
		return
	case utf8.RuneCountInString(in.Body) > 4000:
		errw(w, http.StatusBadRequest, "body must be at most 4000 characters")
		return
	case !validNotificationLink(in.Link):
		errw(w, http.StatusBadRequest, "link must be an in-app path like /tickets/<id> or an https URL")
		return
	}
	n, err := s.insertNotification("custom", in.Title, in.Body, in.Link)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusCreated, n)
}

func validNotificationLink(link string) bool {
	if link == "" {
		return true
	}
	if strings.HasPrefix(link, "/") {
		return !strings.HasPrefix(link, "//") && !strings.Contains(link, `\`)
	}
	u, err := url.Parse(link)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

func (s *Server) notifyMCPSignIn(name string) error {
	title := "MCP server " + name + " needs sign-in"
	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM notification WHERE kind='mcp' AND title=? AND read_at IS NULL)`, title).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.Notify("mcp", title, "Chats and scheduled tasks run without its tools until you sign in again.", "/ai/mcp", false)
}

func NotifyScheduledQuestion(database *sql.DB, chatID string) error {
	var title string
	err := database.QueryRow(`SELECT t.title FROM scheduled_task_run r JOIN scheduled_task t ON t.id=r.task_id WHERE r.chat_id=? AND r.status IN ('running','waiting') ORDER BY r.created_at DESC,r.rowid DESC LIMIT 1`, chatID).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		err = database.QueryRow(`SELECT c.title FROM chat c WHERE c.id=? AND (c.parent_chat_id IS NOT NULL OR COALESCE((SELECT from_chat_id FROM chat_message m WHERE m.chat_id=c.id AND m.role='user' ORDER BY m.created_at DESC,m.rowid DESC LIMIT 1),'')<>'')`, chatID).Scan(&title)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
	}
	if err != nil {
		return err
	}
	title = notificationText(database, title+" has a question")
	_, err = database.Exec(`INSERT INTO notification(id,kind,title,body,link,created_at) VALUES(?,'chat',?,'',?,?)`, uid(), title, "/ai/chats/"+chatID, now())
	return err
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			errw(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = parsed
	}
	query := `SELECT rowid,id,kind,title,body,link,created_at,read_at FROM notification`
	conditions := []string{}
	args := []any{}
	if r.URL.Query().Get("unread") == "1" {
		conditions = append(conditions, `read_at IS NULL`)
	}
	var cursorAt string
	var cursorRowID int64
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			errw(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		stamp, row, ok := strings.Cut(string(decoded), "|")
		cursorRowID, err = strconv.ParseInt(row, 10, 64)
		if !ok || err != nil || cursorRowID < 1 {
			errw(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		if _, err := time.Parse("2006-01-02T15:04:05.000Z", stamp); err != nil {
			errw(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		cursorAt = stamp
		conditions = append(conditions, `(created_at < ? OR (created_at = ? AND rowid < ?))`)
		args = append(args, cursorAt, cursorAt, cursorRowID)
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	query += ` ORDER BY created_at DESC,rowid DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []notification{}
	rowIDs := []int64{}
	for rows.Next() {
		var n notification
		var readAt sql.NullString
		var rowID int64
		if err := rows.Scan(&rowID, &n.ID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.CreatedAt, &readAt); err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		if readAt.Valid {
			n.ReadAt = &readAt.String
		}
		items = append(items, n)
		rowIDs = append(rowIDs, rowID)
	}
	if err := rows.Err(); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := rows.Close(); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
		rowIDs = rowIDs[:limit]
	}
	var unread int
	if err := s.db.QueryRow(`SELECT count(*) FROM notification WHERE read_at IS NULL`).Scan(&unread); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	var nextCursor *string
	if more {
		last := items[len(items)-1]
		encoded := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s|%d", last.CreatedAt, rowIDs[len(rowIDs)-1])))
		nextCursor = &encoded
	}
	write(w, http.StatusOK, map[string]any{"notifications": items, "unread": unread, "next_cursor": nextCursor})
}

func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	result, err := s.db.Exec(`UPDATE notification SET read_at=? WHERE id=? AND read_at IS NULL`, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), idParam(r, "id"))
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count == 0 {
		var exists int
		if err := s.db.QueryRow(`SELECT 1 FROM notification WHERE id=?`, idParam(r, "id")).Scan(&exists); err != nil {
			if err == sql.ErrNoRows {
				errw(w, http.StatusNotFound, "notification not found")
			} else {
				errw(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
	}
	noContent(w)
}

func (s *Server) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.Exec(`UPDATE notification SET read_at=? WHERE read_at IS NULL`, now()); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	noContent(w)
}

func (s *Server) deleteNotification(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.Exec(`DELETE FROM notification WHERE id=?`, idParam(r, "id")); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	noContent(w)
}

// The UI asks for this when a notification arrives while the window is unfocused; only it knows focus.
func (s *Server) nativeNotification(w http.ResponseWriter, r *http.Request) {
	var title, body, link string
	if err := s.db.QueryRow(`SELECT title,body,link FROM notification WHERE id=?`, idParam(r, "id")).Scan(&title, &body, &link); err == sql.ErrNoRows {
		errw(w, http.StatusNotFound, "notification not found")
		return
	} else if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.notify != nil {
		if err := s.notify(title, body, link); err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	noContent(w)
}

func (s *Server) PurgeNotifications() error {
	return txrun(s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM notification WHERE created_at < ?`, time.Now().UTC().Add(-7*24*time.Hour).Format("2006-01-02T15:04:05.000Z")); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM notification WHERE id IN (SELECT id FROM notification ORDER BY created_at DESC,rowid DESC LIMIT -1 OFFSET 500)`); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM item_history WHERE created_at < ?`, time.Now().UTC().Add(-90*24*time.Hour).Format("2006-01-02T15:04:05.000Z"))
		return err
	})
}
