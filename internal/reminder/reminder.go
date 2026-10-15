package reminder

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

type Item struct {
	ID          string
	Kind        string
	BoardID     sql.NullString
	TicketID    sql.NullString
	Title       string
	Description string
	StartsAt    string
}

func Due(ctx context.Context, database *sql.DB, now time.Time) (_ []Item, err error) {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin reminder claim: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	items, err := selectDue(ctx, tx, now)
	if err != nil {
		return nil, err
	}
	notifiedAt := now.UTC().Format("2006-01-02T15:04:05.000Z")
	for _, item := range items {
		result, err := tx.ExecContext(ctx, `UPDATE calendar_item SET notified_at = ? WHERE id = ? AND notified_at IS NULL`, notifiedAt, item.ID)
		if err != nil {
			return nil, fmt.Errorf("claim reminder %s: %w", item.ID, err)
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("check reminder claim %s: %w", item.ID, err)
		}
		if updated != 1 {
			return nil, fmt.Errorf("claim reminder %s: updated %d rows", item.ID, updated)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reminder claims: %w", err)
	}
	committed = true
	return items, nil
}

func selectDue(ctx context.Context, tx *sql.Tx, now time.Time) (items []Item, err error) {
	timedAt := now.UTC().Format("2006-01-02T15:04:05.000Z")
	localDate := now.Format("2006-01-02")
	rows, err := tx.QueryContext(ctx, `
		SELECT c.id, c.kind, c.title, c.description, c.starts_at, c.board_id, c.ticket_id
		FROM calendar_item c
		LEFT JOIN board b ON b.id = c.board_id
		WHERE c.kind IN ('reminder','event')
		  AND c.deleted_at IS NULL
		  AND c.notified_at IS NULL
		  AND (c.board_id IS NULL OR (b.id IS NOT NULL AND b.deleted_at IS NULL))
		  AND ((c.all_day = 1 AND c.starts_at <= ?) OR (c.all_day = 0 AND c.starts_at <= ?))
		ORDER BY c.starts_at, c.rowid`, localDate, timedAt)
	if err != nil {
		return nil, fmt.Errorf("select due reminders: %w", err)
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Kind, &item.Title, &item.Description, &item.StartsAt, &item.BoardID, &item.TicketID); err != nil {
			return nil, fmt.Errorf("scan due reminder: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due reminders: %w", err)
	}
	return items, nil
}

func Run(ctx context.Context, database *sql.DB, every time.Duration, notify func(Item, string) error) error {
	if every <= 0 {
		return fmt.Errorf("reminder interval must be positive")
	}
	check := func() error {
		items, err := Due(ctx, database, time.Now())
		if err != nil {
			return err
		}
		for _, item := range items {
			message := item.Description
			if strings.TrimSpace(message) == "" {
				message = item.StartsAt
			}
			// Claims commit before notification; failed notifications still fire once.
			if err := notify(item, message); err != nil {
				log.Printf("notify reminder %s: %v", item.ID, err)
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
