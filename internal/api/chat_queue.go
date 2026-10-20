package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
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

// sendQueuedChatMessage sends one queued item; callers may reuse this operation to drain FIFO.
func (s *Server) sendQueuedChatMessage(w http.ResponseWriter, r *http.Request) {
	chatID, qid := idParam(r, "id"), idParam(r, "qid")
	message, err := s.chats.sendQueued(chatID, qid)
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "queued message not found")
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

func (m *chatManager) sendQueued(chatID, qid string) (ChatMessage, error) {
	lock := m.queueLock(chatID)
	lock.Lock()
	defer lock.Unlock()
	return m.sendQueuedLocked(chatID, qid)
}

func (m *chatManager) sendQueuedLocked(chatID, qid string) (ChatMessage, error) {
	var item chatQueueItem
	var raw string
	err := m.db.QueryRow(`SELECT id,text,attachments,created_at FROM chat_queue WHERE id=? AND chat_id=?`, qid, chatID).Scan(&item.ID, &item.Text, &raw, &item.CreatedAt)
	if err != nil {
		return ChatMessage{}, err
	}
	if err := json.Unmarshal([]byte(raw), &item.Attachments); err != nil {
		return ChatMessage{}, err
	}
	var connectorID sql.NullString
	if err := m.db.QueryRow(`SELECT connector_id FROM chat WHERE id=?`, chatID).Scan(&connectorID); err != nil {
		return ChatMessage{}, err
	}
	if !connectorID.Valid {
		return ChatMessage{}, errChatConnectorMissing
	}
	kind, dir, err := m.connectorConfig(connectorID.String)
	if err != nil {
		return ChatMessage{}, err
	}
	message, err := m.Send(chat{ID: chatID, ConnectorID: &connectorID.String}, kind, dir, ChatSendRequest{Text: item.Text, Attachments: item.Attachments})
	if err != nil {
		return ChatMessage{}, err
	}
	if _, err := m.db.Exec(`DELETE FROM chat_queue WHERE id=? AND chat_id=?`, qid, chatID); err != nil {
		return ChatMessage{}, err
	}
	m.emit(chatID, map[string]any{"type": "queue"})
	return message, nil
}
