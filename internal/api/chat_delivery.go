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
	rows, err := s.db.Query(`SELECT DISTINCT q.chat_id,c.agent_hops,c.queue_held FROM chat_queue q JOIN chat c ON c.id=q.chat_id WHERE q.from_chat_id IS NOT NULL ORDER BY q.chat_id`)
	if err != nil {
		return err
	}
	type pendingChat struct {
		id        string
		hops      int
		queueHeld int
	}
	var chats []pendingChat
	for rows.Next() {
		var item pendingChat
		if err := rows.Scan(&item.id, &item.hops, &item.queueHeld); err != nil {
			rows.Close()
			return err
		}
		chats = append(chats, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var deliveryErrors []error
	for _, pending := range chats {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, errors.Join(deliveryErrors...))
		}
		if pending.queueHeld != 0 {
			continue
		}
		if pending.hops >= 10 {
			if err := s.notifyAgentHopCap(pending.id); err != nil {
				deliveryErrors = append(deliveryErrors, err)
			}
			continue
		}
		chatID := pending.id
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
