package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type chatQueueItem struct {
	ID          string           `json:"id"`
	Text        string           `json:"text"`
	Attachments []ChatAttachment `json:"attachments"`
	CreatedAt   string           `json:"created_at"`
}

func (s *Server) listChatQueue(w http.ResponseWriter, r *http.Request) {
	items, err := s.chatQueue(idParam(r, "id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			errw(w, 404, "chat not found")
		} else {
			errw(w, 500, err.Error())
		}
		return
	}
	write(w, 200, items)
}

func (s *Server) chatQueue(chatID string) ([]chatQueueItem, error) {
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM chat WHERE id=?`, chatID).Scan(&exists); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id,text,attachments,created_at FROM chat_queue WHERE chat_id=? ORDER BY created_at,rowid`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []chatQueueItem{}
	for rows.Next() {
		var item chatQueueItem
		var raw string
		if err := rows.Scan(&item.ID, &item.Text, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &item.Attachments); err != nil {
			return nil, err
		}
		if item.Attachments == nil {
			item.Attachments = []ChatAttachment{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) deleteChatQueue(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.Exec(`DELETE FROM chat_queue WHERE id=? AND chat_id=?`, idParam(r, "qid"), idParam(r, "id"))
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if n == 0 {
		errw(w, 404, "queued message not found")
		return
	}
	s.chats.emit(idParam(r, "id"), map[string]any{"type": "queue"})
	noContent(w)
}

func (s *Server) sendQueuedChatMessages(w http.ResponseWriter, r *http.Request) {
	message, err := s.chats.drainQueued(idParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "no queued messages")
		return
	}
	if errors.Is(err, errChatTurnRunning) || errors.Is(err, errChatTurnOtherWindow) {
		errw(w, 409, err.Error())
		return
	}
	if errors.Is(err, errChatConnectorMissing) || errors.Is(err, errChatUnsupported) {
		errw(w, 400, err.Error())
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, http.StatusAccepted, message)
}

// drainQueued sends the chat's queued messages, oldest first, as one user message.
// Items that would push the attachments past the per-message limit stay queued.
func (m *chatManager) drainQueued(chatID string) (ChatMessage, error) {
	lock := m.queueLock(chatID)
	lock.Lock()
	defer lock.Unlock()
	rows, err := m.db.Query(`SELECT id,text,attachments FROM chat_queue WHERE chat_id=? ORDER BY created_at,rowid`, chatID)
	if err != nil {
		return ChatMessage{}, err
	}
	var ids, texts []string
	var attachments []ChatAttachment
	for rows.Next() {
		var id, text, raw string
		var items []ChatAttachment
		if err := rows.Scan(&id, &text, &raw); err != nil {
			rows.Close()
			return ChatMessage{}, err
		}
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			rows.Close()
			return ChatMessage{}, err
		}
		if len(ids) > 0 && len(attachments)+len(items) > maxChatAttachments {
			break
		}
		ids = append(ids, id)
		if text != "" {
			texts = append(texts, text)
		}
		attachments = append(attachments, items...)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ChatMessage{}, err
	}
	if len(ids) == 0 {
		return ChatMessage{}, sql.ErrNoRows
	}
	var connectorID, sessionID sql.NullString
	if err := m.db.QueryRow(`SELECT connector_id,session_id FROM chat WHERE id=?`, chatID).Scan(&connectorID, &sessionID); err != nil {
		return ChatMessage{}, err
	}
	if !connectorID.Valid {
		return ChatMessage{}, errChatConnectorMissing
	}
	kind, dir, err := m.connectorConfig(connectorID.String)
	if err != nil {
		return ChatMessage{}, err
	}
	c := chat{ID: chatID, ConnectorID: &connectorID.String}
	if sessionID.Valid {
		c.SessionID = &sessionID.String
	}
	message, err := m.Send(c, kind, dir, ChatSendRequest{Text: strings.Join(texts, "\n\n"), Attachments: attachments})
	if err != nil {
		return ChatMessage{}, err
	}
	if err := txrun(m.db, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.Exec(`DELETE FROM chat_queue WHERE id=? AND chat_id=?`, id, chatID); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return ChatMessage{}, err
	}
	m.emit(chatID, map[string]any{"type": "queue"})
	return message, nil
}
