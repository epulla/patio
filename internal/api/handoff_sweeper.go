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
	type expired struct{ id, parent, child, title, created, deadline, updated string }
	var list []expired
	err := txrun(s.db, func(tx *sql.Tx) error {
		// A child waiting on its own handoff (e.g. a Reviewer chat) is active; that handoff has its own timeout.
		// Handoffs that lead back to the child form a cycle and don't count, or an idle cycle would never time out.
		rows, err := tx.QueryContext(ctx, `SELECT h.id,h.from_chat_id,h.to_chat_id,h.created_at,h.deadline_at,c.title,c.updated_at FROM chat_handoff h JOIN chat c ON c.id=h.to_chat_id WHERE h.status='pending' AND h.deadline_at<? AND NOT EXISTS(SELECT 1 FROM chat_handoff g WHERE g.from_chat_id=h.to_chat_id AND g.status='pending' AND h.to_chat_id NOT IN (WITH RECURSIVE r(id) AS (SELECT g.to_chat_id UNION SELECT x.to_chat_id FROM chat_handoff x JOIN r ON x.from_chat_id=r.id WHERE x.status='pending') SELECT id FROM r))`, now())
		if err != nil {
			return err
		}
		var candidates []expired
		for rows.Next() {
			var e expired
			if err := rows.Scan(&e.id, &e.parent, &e.child, &e.created, &e.deadline, &e.title, &e.updated); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, e)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for _, e := range candidates {
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
			// The timeout counts from the child's last saved message or tool event, not from the handoff start.
			if s.chats.Running(e.child) || s.chats.subagentBusy(e.child) || e.updated >= time.Now().UTC().Add(-time.Duration(n)*time.Minute).Format("2006-01-02T15:04:05.000Z") {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE chat_handoff SET status='failed',result=?,finished_at=? WHERE id=? AND status='pending'`, fmt.Sprintf("No activity for %d min", n), now(), e.id); err != nil {
				return err
			}
			list = append(list, e)
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
	type expiredJoin struct{ run, workflowID, node, edgeID string }
	var joins []expiredJoin
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT i.run_id,c.workflow_id,i.node_id FROM workflow_join_input i JOIN chat c ON c.id=i.run_id WHERE i.delivered=0 AND i.deadline_at<?`, now())
	if err != nil {
		return err
	}
	for rows.Next() {
		var j expiredJoin
		if err := rows.Scan(&j.run, &j.workflowID, &j.node); err != nil {
			rows.Close()
			return err
		}
		joins = append(joins, j)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, j := range joins {
		wf, err := s.loadWorkflow(j.workflowID)
		if errors.Is(err, sql.ErrNoRows) {
			// The workflow was deleted mid-run: drop its waiting inputs instead of retrying forever.
			if _, err := s.db.ExecContext(ctx, `UPDATE workflow_join_input SET delivered=1 WHERE run_id=? AND node_id=? AND delivered=0`, j.run, j.node); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		present := map[string]bool{}
		rows, err := s.db.QueryContext(ctx, `SELECT edge_id FROM workflow_join_input WHERE run_id=? AND node_id=? AND delivered=0`, j.run, j.node)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			present[id] = true
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		var trigger workflowEdge
		err = txrun(s.db, func(tx *sql.Tx) error {
			for _, e := range wf.Graph.Edges {
				if e.To != j.node {
					continue
				}
				if trigger.ID == "" {
					trigger = e
				}
				if present[e.ID] {
					continue
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO workflow_join_input(id,run_id,node_id,edge_id,status,text,created_at,deadline_at,delivered) VALUES(?,?,?,?,'failed','No report before the timeout.',?,?,0)`, uid(), j.run, j.node, e.ID, now(), now()); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		chatID, err := fireWorkflowJoin(s, j.run, j.workflowID, j.node, trigger, wf)
		if err != nil {
			return err
		}
		if chatID != "" {
			if err = s.Notify("chat", "Workflow step started with missing inputs", wf.Name, "/ai/chats/"+chatID, true); err != nil {
				return err
			}
		}
	}
	return nil
}
