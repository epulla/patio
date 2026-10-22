package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type monitorClaim struct{ monitor monitor }
type monitorRunResult struct {
	status   string
	newItems []json.RawMessage
	result   string
}

func (s *Server) StartMonitorRunner(ctx context.Context, every time.Duration) error {
	if every <= 0 {
		return fmt.Errorf("monitor interval must be positive")
	}
	sem := make(chan struct{}, 4)
	var inFlightMu sync.Mutex
	inFlight := map[string]bool{}
	check := func() error {
		claims, err := s.claimMonitors(ctx, time.Now())
		if err != nil {
			return err
		}
		for _, claim := range claims {
			inFlightMu.Lock()
			if inFlight[claim.monitor.ID] {
				inFlightMu.Unlock()
				continue
			}
			inFlight[claim.monitor.ID] = true
			inFlightMu.Unlock()
			go func(c monitorClaim) {
				defer func() { inFlightMu.Lock(); delete(inFlight, c.monitor.ID); inFlightMu.Unlock() }()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()
				_, err := s.runMonitor(ctx, c.monitor, time.Now())
				if err != nil {
					log.Printf("run monitor %s: %v", c.monitor.ID, err)
				}
			}(claim)
		}
		return nil
	}
	if err := check(); err != nil {
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

func (s *Server) claimMonitors(ctx context.Context, at time.Time) ([]monitorClaim, error) {
	claims := []monitorClaim{}
	err := txrun(s.db, func(tx *sql.Tx) error {
		stamp := at.UTC().Format("2006-01-02T15:04:05.000Z")
		rows, err := tx.QueryContext(ctx, `SELECT `+monitorColumns+` FROM monitor WHERE enabled=1 AND next_run_at<=? ORDER BY next_run_at,rowid`, stamp)
		if err != nil {
			return err
		}
		var monitors []monitor
		for rows.Next() {
			m, e := scanMonitor(rows)
			if e != nil {
				rows.Close()
				return e
			}
			monitors = append(monitors, m)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, m := range monitors {
			schedule, e := cron.ParseStandard(m.Schedule)
			if e != nil {
				return e
			}
			next := schedule.Next(at).UTC().Format("2006-01-02T15:04:05.000Z")
			if _, e = tx.ExecContext(ctx, `UPDATE monitor SET next_run_at=? WHERE id=?`, next, m.ID); e != nil {
				return e
			}
			claims = append(claims, monitorClaim{m})
		}
		return nil
	})
	return claims, err
}

func (s *Server) runMonitor(ctx context.Context, m monitor, at time.Time) (monitorRunResult, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(m.Args), &args); err != nil {
		return monitorRunResult{}, err
	}
	result, callErr := s.callMonitorTool(ctx, m.MCPServerID, m.Tool, args)
	computed := monitorRunResult{result: result, status: "ok"}
	var rawItems []json.RawMessage
	if callErr == nil {
		sum := sha256.Sum256([]byte(result))
		hash := hex.EncodeToString(sum[:])
		newCount := 0
		changed := false
		switch m.Condition {
		case "changed":
			changed = m.LastHash != "" && m.LastHash != hash
		case "non_empty":
			trimmed := strings.TrimSpace(result)
			changed = trimmed != "" && trimmed != "[]" && trimmed != "{}" && trimmed != "null"
		case "new_items":
			var value any
			if err := json.Unmarshal([]byte(result), &value); err != nil {
				callErr = fmt.Errorf("parse monitor result: %w", err)
				break
			}
			value, ok := monitorPath(value, m.ResultPath)
			if !ok {
				callErr = errors.New("result_path not found")
				break
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				callErr = err
				break
			}
			if err = json.Unmarshal(encoded, &rawItems); err != nil {
				callErr = errors.New("result_path must be an array")
				break
			}
			for _, item := range rawItems {
				var decoded any
				if err := json.Unmarshal(item, &decoded); err != nil {
					callErr = err
					break
				}
				idValue, ok := monitorPath(decoded, m.IDPath)
				if !ok {
					callErr = errors.New("id_path not found")
					break
				}
				id := monitorID(idValue)
				var exists int
				err := s.db.QueryRowContext(ctx, `SELECT 1 FROM monitor_seen WHERE monitor_id=? AND item_id=?`, m.ID, id).Scan(&exists)
				if errors.Is(err, sql.ErrNoRows) {
					if m.LastRunAt != nil {
						newCount++
						computed.newItems = append(computed.newItems, item)
					}
					if _, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO monitor_seen(monitor_id,item_id) VALUES(?,?)`, m.ID, id); err != nil {
						callErr = err
						break
					}
				} else if err != nil {
					callErr = err
					break
				}
			}
			changed = newCount > 0
		}
		if callErr == nil {
			if changed {
				computed.status = "changed"
			}
		}
	}
	if callErr != nil {
		computed.status = "failed"
	}
	stamp := at.UTC().Format("2006-01-02T15:04:05.000Z")
	hashBytes := sha256.Sum256([]byte(result))
	hash := hex.EncodeToString(hashBytes[:])
	errText := ""
	if callErr != nil {
		errText = callErr.Error()
	}
	var firstFailure, recovered bool
	err := txrun(s.db, func(tx *sql.Tx) error {
		var streak int
		if e := tx.QueryRowContext(ctx, `SELECT fail_streak FROM monitor WHERE id=?`, m.ID).Scan(&streak); e != nil {
			return e
		}
		firstFailure = callErr != nil && streak == 0
		recovered = callErr == nil && streak > 0
		nextStreak := streak
		if callErr != nil {
			nextStreak++
		} else {
			nextStreak = 0
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO monitor_run(id,monitor_id,started_at,status,result_hash,new_count,error) VALUES(?,?,?,?,?,?,?)`, uid(), m.ID, stamp, computed.status, hash, len(computed.newItems), errText); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE monitor SET last_run_at=?,last_status=?,last_error=?,last_hash=CASE WHEN ? THEN last_hash ELSE ? END,fail_streak=? WHERE id=?`, stamp, computed.status, errText, callErr != nil, hash, nextStreak, m.ID); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `DELETE FROM monitor_run WHERE monitor_id=? AND id NOT IN (SELECT id FROM monitor_run WHERE monitor_id=? ORDER BY started_at DESC,rowid DESC LIMIT 100)`, m.ID, m.ID)
		return e
	})
	if err != nil {
		return computed, err
	}
	if firstFailure {
		if err := s.Notify("monitor", "Monitor failing: "+m.Name, errText, "/monitors/"+m.ID, true); err != nil {
			log.Printf("notify monitor failure %s: %v", m.ID, err)
		}
	} else if recovered {
		if err := s.Notify("monitor", "Monitor recovered: "+m.Name, "", "/monitors/"+m.ID, true); err != nil {
			log.Printf("notify monitor recovery %s: %v", m.ID, err)
		}
	}
	return computed, nil
}

func monitorPath(value any, path string) (any, bool) {
	if path == "" {
		return value, true
	}
	for _, part := range strings.Split(path, ".") {
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return value, true
}
func monitorID(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
