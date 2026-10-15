package api

import (
	"database/sql"
	"net/http"
	"time"
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
	_, err := s.db.Exec(`INSERT INTO notification(id,kind,title,body,link,created_at) VALUES(?,?,?,?,?,?)`, uid(), kind, title, body, link, now())
	if err != nil {
		return err
	}
	if native && s.notify != nil {
		return s.notify(title, body)
	}
	return nil
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,kind,title,body,link,created_at,read_at FROM notification ORDER BY created_at DESC,rowid DESC LIMIT 100`)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []notification{}
	for rows.Next() {
		var n notification
		var readAt sql.NullString
		if err := rows.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.CreatedAt, &readAt); err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		if readAt.Valid {
			n.ReadAt = &readAt.String
		}
		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	var unread int
	if err := s.db.QueryRow(`SELECT count(*) FROM notification WHERE read_at IS NULL`).Scan(&unread); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, map[string]any{"notifications": items, "unread": unread})
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

func (s *Server) PurgeNotifications() error {
	return txrun(s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM notification WHERE created_at < ?`, time.Now().UTC().Add(-7*24*time.Hour).Format("2006-01-02T15:04:05.000Z")); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM notification WHERE id IN (SELECT id FROM notification ORDER BY created_at DESC,rowid DESC LIMIT -1 OFFSET 500)`)
		return err
	})
}
