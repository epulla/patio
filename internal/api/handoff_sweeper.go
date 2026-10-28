package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"
)

func (s *Server) StartHandoffSweeper(ctx context.Context, every time.Duration) error {
	if every <= 0 {
		return fmt.Errorf("handoff interval must be positive")
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := s.SweepHandoffs(ctx); err != nil && ctx.Err() == nil {
				log.Printf("sweep chat handoffs: %v", err)
			}
		}
	}
}

func (s *Server) SweepHandoffs(ctx context.Context) error {
	type expired struct{ id, parent, child, title, created, deadline string }
	var list []expired
	err := txrun(s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT h.id,h.from_chat_id,h.to_chat_id,h.created_at,h.deadline_at,c.title FROM chat_handoff h JOIN chat c ON c.id=h.to_chat_id WHERE h.status='pending' AND h.deadline_at<?`, now())
		if err != nil {
			return err
		}
		for rows.Next() {
			var e expired
			if err := rows.Scan(&e.id, &e.parent, &e.child, &e.created, &e.deadline, &e.title); err != nil {
				rows.Close()
				return err
			}
			list = append(list, e)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for _, e := range list {
			created, err := time.Parse("2006-01-02T15:04:05.000Z", e.created)
			if err != nil {
				return err
			}
			deadline, err := time.Parse("2006-01-02T15:04:05.000Z", e.deadline)
			if err != nil {
				return err
			}
			n := int(deadline.Sub(created).Minutes())
			if n < 1 {
				n = 30
			}
			if _, err := tx.ExecContext(ctx, `UPDATE chat_handoff SET status='failed',result=?,finished_at=? WHERE id=? AND status='pending'`, fmt.Sprintf("Timed out after %d min", n), now(), e.id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, e := range list {
		if err := s.chats.deliverHandoffReports(e.parent); err != nil {
			return err
		}
		if err := s.Notify("chat", "Handoff timed out: "+e.title, "", "/ai/chats/"+e.child, true); err != nil {
			return err
		}
	}
	return nil
}
