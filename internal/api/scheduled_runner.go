package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/robfig/cron/v3"
)

type scheduledClaim struct {
	task    scheduledTask
	runID   string
	when    time.Time
	late    bool
	skipped bool
}

func (s *Server) StartScheduledRunner(ctx context.Context, every time.Duration) error {
	if every <= 0 {
		return fmt.Errorf("scheduled task interval must be positive")
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE scheduled_task_run SET status='failed',error='The app closed before this run finished' WHERE status='running'`); err != nil {
		return err
	}
	check := func() error {
		claims, err := s.claimScheduled(ctx, time.Now())
		if err != nil {
			return err
		}
		for _, claim := range claims {
			if claim.skipped {
				continue
			}
			if err := s.startScheduled(claim); err != nil {
				s.failScheduled(claim, err)
			}
		}
		return nil
	}
	if err := check(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := check(); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

func (s *Server) claimScheduled(ctx context.Context, nowTime time.Time) ([]scheduledClaim, error) {
	var claims []scheduledClaim
	err := txrun(s.db, func(tx *sql.Tx) error {
		stamp := nowTime.UTC().Format("2006-01-02T15:04:05.000Z")
		rows, err := tx.QueryContext(ctx, `SELECT `+scheduledTaskColumns+` FROM scheduled_task WHERE enabled=1 AND next_run_at<=? ORDER BY next_run_at,rowid`, stamp)
		if err != nil {
			return err
		}
		var tasks []scheduledTask
		for rows.Next() {
			task, err := scanScheduledTask(rows)
			if err != nil {
				rows.Close()
				return err
			}
			tasks = append(tasks, task)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, task := range tasks {
			when, err := time.Parse("2006-01-02T15:04:05.000Z", *task.NextRunAt)
			if err != nil {
				return err
			}
			// burnedout: assumed 1-minute late threshold
			claim := scheduledClaim{task: task, runID: uid(), when: when, late: nowTime.Sub(when) > time.Minute}
			var runningID string
			var chatID sql.NullString
			err = tx.QueryRowContext(ctx, `SELECT id,chat_id FROM scheduled_task_run WHERE task_id=? AND status='running'`, task.ID).Scan(&runningID, &chatID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil {
				claim.skipped = chatID.Valid && s.chats.Running(chatID.String)
				// A stopped or deleted chat never reaches finishTurn.
				if !claim.skipped {
					if _, err := tx.ExecContext(ctx, `UPDATE scheduled_task_run SET status='failed',error='Stopped before finishing' WHERE id=?`, runningID); err != nil {
						return err
					}
				}
			}
			status := "running"
			if claim.skipped {
				status = "skipped"
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO scheduled_task_run(id,task_id,scheduled_for,status,late,created_at) VALUES(?,?,?,?,?,?)`, claim.runID, task.ID, *task.NextRunAt, status, claim.late, stamp); err != nil {
				return err
			}
			var next any
			if task.Schedule != "" {
				schedule, err := cron.ParseStandard(task.Schedule)
				if err != nil {
					return err
				}
				next = schedule.Next(nowTime).UTC().Format("2006-01-02T15:04:05.000Z")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE scheduled_task SET next_run_at=? WHERE id=?`, next, task.ID); err != nil {
				return err
			}
			claims = append(claims, claim)
		}
		return nil
	})
	return claims, err
}

func (s *Server) startScheduled(claim scheduledClaim) error {
	t := claim.task
	if t.ConnectorID == nil {
		return errChatConnectorMissing
	}
	kind, configDir, err := s.chats.connectorConfig(*t.ConnectorID)
	if err != nil {
		return err
	}
	chatID, created := uid(), now()
	title := t.Title + " · " + time.Now().Format("Jan 2, 15:04")
	c := chat{ID: chatID, ConnectorID: t.ConnectorID, Title: title, Model: t.Model, Folder: t.Folder, CreatedAt: created, UpdatedAt: created, Thinking: t.Thinking, AutoAllow: t.AutoAllow}
	if c.Folder != nil {
		if _, allowed, err := s.allowedFolderPath(*c.Folder); err != nil {
			return err
		} else if !allowed {
			return errors.New("folder is no longer allowed")
		}
	} else {
		dir := s.chatWorkdir(c)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return cleanupConnectorDir(dir, err)
		}
	}
	if err := txrun(s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at,thinking,auto_allow,scheduled_task_id) VALUES(?,?,?,?,?,?,?,?,?,?)`, c.ID, c.ConnectorID, c.Title, c.Model, c.Folder, c.CreatedAt, c.UpdatedAt, c.Thinking, c.AutoAllow, t.ID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE scheduled_task_run SET chat_id=? WHERE id=?`, chatID, claim.runID)
		return err
	}); err != nil {
		return err
	}
	_, err = s.chats.Send(c, kind, configDir, ChatSendRequest{Text: t.Prompt})
	if err == nil {
		if notifyErr := s.Notify("scheduled", "Scheduled task started: "+t.Title, "", "/ai/scheduled/"+t.ID, false); notifyErr != nil {
			log.Printf("notify scheduled task start %s: %v", t.ID, notifyErr)
		}
	}
	return err
}

func (s *Server) failScheduled(claim scheduledClaim, cause error) {
	if _, err := s.db.Exec(`UPDATE scheduled_task_run SET status='failed',error=? WHERE id=? AND status='running'`, cause.Error(), claim.runID); err != nil {
		log.Printf("fail scheduled task run %s: %v", claim.runID, err)
	}
	if err := s.Notify("scheduled", "Scheduled task failed: "+claim.task.Title, cause.Error(), "/ai/scheduled/"+claim.task.ID, true); err != nil {
		log.Printf("notify scheduled task run %s: %v", claim.runID, err)
	}
}
