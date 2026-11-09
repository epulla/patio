package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type chatDraft struct {
	Text        string          `json:"text"`
	Attachments json.RawMessage `json:"attachments"`
	UpdatedAt   string          `json:"updated_at"`
}

func (s *Server) getChatDraft(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	var draft chatDraft
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM chat WHERE id=?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			errw(w, 404, "chat not found")
		} else {
			errw(w, 500, err.Error())
		}
		return
	}
	draft.Attachments = json.RawMessage(`[]`)
	var attachments string
	err := s.db.QueryRow(`SELECT text,attachments,updated_at FROM chat_draft WHERE chat_id=?`, id).Scan(&draft.Text, &attachments, &draft.UpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		errw(w, 500, err.Error())
		return
	}
	if err == nil {
		draft.Attachments = json.RawMessage(attachments)
	}
	write(w, 200, draft)
}

func (s *Server) putChatDraft(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM chat WHERE id=?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			errw(w, 404, "chat not found")
		} else {
			errw(w, 500, err.Error())
		}
		return
	}
	var in struct {
		Text        string          `json:"text"`
		Attachments json.RawMessage `json:"attachments"`
	}
	var items []json.RawMessage
	if decode(r, &in) != nil || json.Unmarshal(in.Attachments, &items) != nil || items == nil {
		errw(w, 400, "attachments must be an array")
		return
	}
	if in.Text == "" && len(items) == 0 {
		if _, err := s.db.Exec(`DELETE FROM chat_draft WHERE chat_id=?`, id); err != nil {
			errw(w, 500, err.Error())
			return
		}
	} else if _, err := s.db.Exec(`INSERT INTO chat_draft(chat_id,text,attachments,updated_at) VALUES(?,?,?,?) ON CONFLICT(chat_id) DO UPDATE SET text=excluded.text,attachments=excluded.attachments,updated_at=excluded.updated_at`, id, in.Text, string(in.Attachments), now()); err != nil {
		errw(w, 500, err.Error())
		return
	}
	noContent(w)
}

func (s *Server) pruneChatDrafts() error {
	_, err := s.db.Exec(`DELETE FROM chat_draft WHERE updated_at < ?`, time.Now().UTC().Add(-7*24*time.Hour).Format("2006-01-02T15:04:05.000Z"))
	return err
}
