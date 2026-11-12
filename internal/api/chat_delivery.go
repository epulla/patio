package api

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"
)

func (s *Server) DeliverAgentMessages(ctx context.Context) error {
	if !s.agentDeliveryMu.TryLock() {
		return nil
	}
	defer s.agentDeliveryMu.Unlock()
	rows, err := s.db.Query(`SELECT DISTINCT q.chat_id,c.agent_hops,c.queue_held,c.workflow_run_id IS NOT NULL FROM chat_queue q JOIN chat c ON c.id=q.chat_id WHERE q.from_chat_id IS NOT NULL ORDER BY q.chat_id`)
	if err != nil {
		return err
	}
	type pendingChat struct {
		id        string
		hops      int
		queueHeld int
		workflow  bool
	}
	var chats []pendingChat
	for rows.Next() {
		var item pendingChat
		if err := rows.Scan(&item.id, &item.hops, &item.queueHeld, &item.workflow); err != nil {
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
	limit := agentHopLimit(s.db)
	var deliveryErrors []error
	retry := false
	defer func() {
		if retry {
			s.retrySteer(ctx)
		}
	}()
	for _, pending := range chats {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, errors.Join(deliveryErrors...))
		}
		if pending.queueHeld != 0 {
			continue
		}
		blocked, err := queueCapBlocked(s.db, pending.id, pending.hops, limit)
		if err != nil {
			deliveryErrors = append(deliveryErrors, err)
			continue
		}
		if !pending.workflow && blocked {
			if err := s.notifyAgentHopCap(pending.id); err != nil {
				deliveryErrors = append(deliveryErrors, err)
			}
			continue
		}
		chatID := pending.id
		if s.chats.Running(chatID) {
			again, err := s.steerHead(chatID)
			retry = retry || again
			if err != nil {
				deliveryErrors = append(deliveryErrors, err)
			}
			continue
		}
		if _, err := s.chats.drainQueued(chatID, false); err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("deliver queued agent messages to %s: %v", chatID, err)
			deliveryErrors = append(deliveryErrors, err)
		}
	}
	return errors.Join(deliveryErrors...)
}

// steerHead steers the head of a running chat's queue into its turn when the sender asked to steer.
// It reports whether to try again because the turn can't take it yet; if the turn ends first, the row is delivered as usual.
func (s *Server) steerHead(chatID string) (bool, error) {
	var rowID string
	var sender sql.NullString
	var steer bool
	err := s.db.QueryRow(`SELECT id,from_chat_id,steer FROM chat_queue WHERE chat_id=? ORDER BY created_at,rowid LIMIT 1`, chatID).Scan(&rowID, &sender, &steer)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!steer || !sender.Valid) {
		delete(s.steerLogged, chatID)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if reason := s.chats.steerBlocker(chatID); reason != "" {
		if s.steerLogged[chatID] != rowID+" "+reason {
			log.Printf("steer %s into %s waiting: %s", rowID, chatID, reason)
			if s.steerLogged == nil {
				s.steerLogged = make(map[string]string)
			}
			s.steerLogged[chatID] = rowID + " " + reason
		}
		return reason != steerUnsupported, nil
	}
	delete(s.steerLogged, chatID)
	_, err = s.chats.steerQueued(chatID, sender.String)
	if err == nil || errors.Is(err, sql.ErrNoRows) || errors.Is(err, errChatTurnRunning) {
		return false, nil
	}
	log.Printf("steer %s into %s failed; delivering after the turn: %v", rowID, chatID, err)
	return false, nil
}

// retrySteer runs delivery again shortly, since a starting turn may not write to the database before it can steer.
func (s *Server) retrySteer(ctx context.Context) {
	if !s.steerRetry.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(time.Second, func() {
		s.steerRetry.Store(false)
		if ctx.Err() != nil {
			return
		}
		if err := s.DeliverAgentMessages(ctx); err != nil {
			log.Printf("retry steering queued agent messages: %v", err)
		}
	})
}
