package api

import (
	"context"
	"database/sql"
	"errors"
	"log"
)

func (s *Server) DeliverAgentMessages(ctx context.Context) error {
	if !s.agentDeliveryMu.TryLock() {
		return nil
	}
	defer s.agentDeliveryMu.Unlock()
	rows, err := s.db.Query(`SELECT DISTINCT q.chat_id FROM chat_queue q JOIN chat c ON c.id=q.chat_id WHERE q.from_chat_id IS NOT NULL AND c.agent_hops<10 ORDER BY q.chat_id`)
	if err != nil {
		return err
	}
	var chatIDs []string
	for rows.Next() {
		var chatID string
		if err := rows.Scan(&chatID); err != nil {
			rows.Close()
			return err
		}
		chatIDs = append(chatIDs, chatID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var deliveryErrors []error
	for _, chatID := range chatIDs {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, errors.Join(deliveryErrors...))
		}
		if s.chats.Running(chatID) {
			continue
		}
		if _, err := s.chats.drainQueued(chatID, false); err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("deliver queued agent messages to %s: %v", chatID, err)
			deliveryErrors = append(deliveryErrors, err)
		}
	}
	return errors.Join(deliveryErrors...)
}
