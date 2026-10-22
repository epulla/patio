package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
)

type chatQueueItem struct {
	ID            string           `json:"id"`
	Text          string           `json:"text"`
	Attachments   []ChatAttachment `json:"attachments"`
	CreatedAt     string           `json:"created_at"`
	FromChatID    *string          `json:"from_chat_id"`
	FromChatTitle string           `json:"from_chat_title"`
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
	rows, err := s.db.Query(`SELECT q.id,q.text,q.attachments,q.created_at,q.from_chat_id,COALESCE(sender.title,'') FROM chat_queue q LEFT JOIN chat sender ON sender.id=q.from_chat_id WHERE q.chat_id=? ORDER BY q.created_at,q.rowid`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []chatQueueItem{}
	for rows.Next() {
		var item chatQueueItem
		var raw string
		if err := rows.Scan(&item.ID, &item.Text, &raw, &item.CreatedAt, &item.FromChatID, &item.FromChatTitle); err != nil {
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
	message, err := s.chats.drainQueued(idParam(r, "id"), true)
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
func (m *chatManager) drainQueued(chatID string, manual bool) (ChatMessage, error) {
	lock := m.queueLock(chatID)
	lock.Lock()
	defer lock.Unlock()
	type queuedRow struct {
		id, text, raw, createdAt string
		from, report             sql.NullString
	}
	var claimed []queuedRow
	var oldHops, oldNotified int
	var sender, reportTo sql.NullString
	var texts []string
	var attachments []ChatAttachment
	err := txrun(m.db, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT agent_hops,agent_hops_notified FROM chat WHERE id=?`, chatID).Scan(&oldHops, &oldNotified); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT id,text,attachments,created_at,from_chat_id,report_to_chat_id FROM chat_queue WHERE chat_id=? ORDER BY created_at,rowid`, chatID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var row queuedRow
			var rawItems []ChatAttachment
			if err := rows.Scan(&row.id, &row.text, &row.raw, &row.createdAt, &row.from, &row.report); err != nil {
				rows.Close()
				return err
			}
			if err := json.Unmarshal([]byte(row.raw), &rawItems); err != nil {
				rows.Close()
				return err
			}
			if len(claimed) > 0 && (row.from != sender || len(attachments)+len(rawItems) > maxChatAttachments) {
				break
			}
			if len(claimed) == 0 && !manual && row.from.Valid && oldHops >= 10 {
				break
			}
			if len(claimed) == 0 {
				sender, reportTo = row.from, row.report
			}
			claimed = append(claimed, row)
			if row.text != "" {
				texts = append(texts, row.text)
			}
			attachments = append(attachments, rawItems...)
		}
		rowsErr := rows.Err()
		closeErr := rows.Close()
		if err := errors.Join(rowsErr, closeErr); err != nil {
			return err
		}
		if len(claimed) == 0 {
			return sql.ErrNoRows
		}
		for _, row := range claimed {
			if _, err := tx.Exec(`DELETE FROM chat_queue WHERE id=? AND chat_id=?`, row.id, chatID); err != nil {
				return err
			}
		}
		if manual || !sender.Valid {
			_, err = tx.Exec(`UPDATE chat SET agent_hops=0,agent_hops_notified=0 WHERE id=?`, chatID)
		} else {
			_, err = tx.Exec(`UPDATE chat SET agent_hops=? WHERE id=?`, oldHops+1, chatID)
		}
		return err
	})
	if err != nil {
		return ChatMessage{}, err
	}
	restore := func(cause error, messageID string) error {
		restoreErr := txrun(m.db, func(tx *sql.Tx) error {
			if messageID != "" {
				if _, err := tx.Exec(`DELETE FROM chat_message WHERE id=? AND chat_id=? AND role='user'`, messageID, chatID); err != nil {
					return err
				}
			}
			for _, row := range claimed {
				if _, err := tx.Exec(`INSERT INTO chat_queue(id,chat_id,text,attachments,created_at,from_chat_id,report_to_chat_id) VALUES(?,?,?,?,?,?,?)`, row.id, chatID, row.text, row.raw, row.createdAt, nullableQueueSender(row.from), nullableQueueSender(row.report)); err != nil {
					return err
				}
			}
			_, err := tx.Exec(`UPDATE chat SET agent_hops=?,agent_hops_notified=? WHERE id=?`, oldHops, oldNotified, chatID)
			return err
		})
		m.emit(chatID, map[string]any{"type": "queue"})
		if restoreErr != nil {
			return errors.Join(cause, restoreErr)
		}
		return cause
	}
	var connectorID, sessionID sql.NullString
	if err := m.db.QueryRow(`SELECT connector_id,session_id FROM chat WHERE id=?`, chatID).Scan(&connectorID, &sessionID); err != nil {
		return ChatMessage{}, restore(err, "")
	}
	if !connectorID.Valid {
		return ChatMessage{}, restore(errChatConnectorMissing, "")
	}
	kind, dir, err := m.connectorConfig(connectorID.String)
	if err != nil {
		return ChatMessage{}, restore(err, "")
	}
	c := chat{ID: chatID, ConnectorID: &connectorID.String}
	if sessionID.Valid {
		c.SessionID = &sessionID.String
	}
	text := strings.Join(texts, "\n\n")
	var fromID, reportID *string
	if sender.Valid {
		var senderTitle string
		if err := m.db.QueryRow(`SELECT title FROM chat WHERE id=?`, sender.String).Scan(&senderTitle); err != nil {
			return ChatMessage{}, restore(err, "")
		}
		text = agentMessageLabel(sender.String, senderTitle, reportTo.Valid) + text
		fromID = &sender.String
		if reportTo.Valid {
			reportID = &reportTo.String
		}
	}
	message, err := m.Send(c, kind, dir, ChatSendRequest{Text: text, StoredText: strings.Join(texts, "\n\n"), Attachments: attachments, FromChatID: fromID, ReportToChatID: reportID, AgentHopsManaged: true})
	if err != nil {
		err = restore(err, message.ID)
		log.Printf("queued chat send failed; restored rows for %s: %v", chatID, err)
		return ChatMessage{}, err
	}
	return message, nil
}

func nullableQueueSender(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}
