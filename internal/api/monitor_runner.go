package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type monitorClaim struct{ monitor monitor }
type monitorRunResult struct {
	status    string
	runID     string
	startedAt string
	newItems  []json.RawMessage
	result    string
}

var monitorFlights = struct {
	sync.Mutex
	servers map[*Server]map[string]bool
}{servers: map[*Server]map[string]bool{}}

func (s *Server) acquireMonitor(id string) bool {
	monitorFlights.Lock()
	defer monitorFlights.Unlock()
	if monitorFlights.servers[s] == nil {
		monitorFlights.servers[s] = map[string]bool{}
	}
	if monitorFlights.servers[s][id] {
		return false
	}
	monitorFlights.servers[s][id] = true
	return true
}

func (s *Server) releaseMonitor(id string) {
	monitorFlights.Lock()
	defer monitorFlights.Unlock()
	delete(monitorFlights.servers[s], id)
	if len(monitorFlights.servers[s]) == 0 {
		delete(monitorFlights.servers, s)
	}
}

func (s *Server) StartMonitorRunner(ctx context.Context, every time.Duration) error {
	if every <= 0 {
		return fmt.Errorf("monitor interval must be positive")
	}
	sem := make(chan struct{}, 4)
	check := func() error {
		claims, err := s.claimMonitors(ctx, time.Now())
		if err != nil {
			return err
		}
		for _, claim := range claims {
			if !s.acquireMonitor(claim.monitor.ID) {
				continue
			}
			go func(c monitorClaim) {
				defer s.releaseMonitor(c.monitor.ID)
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()
				result, err := s.runMonitor(ctx, c.monitor, time.Now())
				if err != nil {
					log.Printf("run monitor %s: %v", c.monitor.ID, err)
					return
				}
				if result.status == "changed" {
					s.applyChangedMonitorActions(ctx, c.monitor, result)
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
	var itemIDs []string
	if callErr == nil {
		sum := sha256.Sum256([]byte(result))
		hash := hex.EncodeToString(sum[:])
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
				itemIDs = append(itemIDs, monitorID(idValue))
			}
		}
		if callErr == nil {
			if changed && m.Condition != "new_items" {
				computed.status = "changed"
			}
		}
	}
	if callErr != nil {
		computed.status = "failed"
	}
	stamp := at.UTC().Format("2006-01-02T15:04:05.000Z")
	computed.startedAt = stamp
	hashBytes := sha256.Sum256([]byte(result))
	hash := hex.EncodeToString(hashBytes[:])
	errText := ""
	if callErr != nil {
		errText = callErr.Error()
	}
	var firstFailure, recovered bool
	computed.runID = uid()
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
		if callErr == nil && m.Condition == "new_items" {
			rows, e := tx.QueryContext(ctx, `SELECT item_id FROM monitor_seen WHERE monitor_id=?`, m.ID)
			if e != nil {
				return e
			}
			seen := map[string]bool{}
			for rows.Next() {
				var id string
				if e := rows.Scan(&id); e != nil {
					rows.Close()
					return e
				}
				seen[id] = true
			}
			if e := rows.Err(); e != nil {
				rows.Close()
				return e
			}
			if e := rows.Close(); e != nil {
				return e
			}
			for i, id := range itemIDs {
				if seen[id] {
					continue
				}
				seen[id] = true
				if m.LastRunAt != nil {
					computed.newItems = append(computed.newItems, rawItems[i])
				}
				if _, e := tx.ExecContext(ctx, `INSERT INTO monitor_seen(monitor_id,item_id) VALUES(?,?)`, m.ID, id); e != nil {
					return e
				}
			}
			if len(computed.newItems) > 0 {
				computed.status = "changed"
			}
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO monitor_run(id,monitor_id,started_at,status,result_hash,new_count,error) VALUES(?,?,?,?,?,?,?)`, computed.runID, m.ID, stamp, computed.status, hash, len(computed.newItems), errText); e != nil {
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

func (s *Server) fireMonitorActions(ctx context.Context, m monitor, run monitorRunResult) string {
	var actions struct {
		Notify bool `json:"notify"`
		Ticket *struct {
			BoardID string `json:"board_id"`
			Title   string `json:"title"`
		} `json:"ticket"`
		Chat *struct {
			Prompt string `json:"prompt"`
		} `json:"chat"`
	}
	if err := json.Unmarshal([]byte(m.Actions), &actions); err != nil {
		return "actions: " + err.Error()
	}
	count := len(run.newItems)
	countText := fmt.Sprintf("%d", count)
	suffix := countText + " new"
	items := ""
	if m.Condition == "new_items" {
		limit := len(run.newItems)
		if limit > 20 {
			limit = 20
		}
		var lines []string
		for _, item := range run.newItems[:limit] {
			var compact bytes.Buffer
			if err := json.Compact(&compact, item); err != nil {
				continue
			}
			lines = append(lines, "- "+truncateMonitor(compact.String(), 500))
		}
		if len(run.newItems) > limit {
			lines = append(lines, fmt.Sprintf("…and %d more", len(run.newItems)-limit))
		}
		items = strings.Join(lines, "\n")
	} else {
		countText = "changed"
		suffix = "changed"
		items = "```\n" + truncateMonitor(run.result, 4000) + "\n```"
	}
	render := func(value string) string {
		return strings.NewReplacer("{{name}}", m.Name, "{{count}}", countText, "{{items}}", items).Replace(value)
	}
	var errs []string
	if actions.Notify {
		title := m.Name + ": " + suffix
		body := truncateMonitor(items, 500)
		if err := s.Notify("monitor", title, body, "/monitors/"+m.ID, true); err != nil {
			errs = append(errs, "notify: "+err.Error())
		}
	}
	if actions.Ticket != nil {
		_, err := s.insertTicket(actions.Ticket.BoardID, render(actions.Ticket.Title), items, "", false, "", false, "", false, "", false)
		if err != nil {
			errs = append(errs, "ticket: "+err.Error())
		}
	}
	if actions.Chat != nil {
		if err := s.startMonitorChat(m, render(actions.Chat.Prompt)); err != nil {
			errs = append(errs, "chat: "+err.Error())
		}
	}
	return strings.Join(errs, "; ")
}

func (s *Server) startMonitorChat(m monitor, prompt string) error {
	defaults, err := s.chatDefaults()
	if err != nil {
		return err
	}
	if defaults.ConnectorID == nil {
		return errors.New("no default connector configured")
	}
	kind, configDir, err := s.chats.connectorConfig(*defaults.ConnectorID)
	if err != nil {
		return err
	}
	created := now()
	c := chat{ID: uid(), ConnectorID: defaults.ConnectorID, Title: "Monitor: " + m.Name + " · " + time.Now().Format("Jan 2, 15:04"), Model: defaults.Model, Folder: defaults.Folder, CreatedAt: created, UpdatedAt: created, Thinking: defaults.Thinking, AutoAllow: defaults.AutoAllow}
	if c.Folder == nil {
		dir := s.chatWorkdir(c)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return cleanupConnectorDir(dir, err)
		}
	}
	if err := txrun(s.db, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at,thinking,show_thinking,auto_allow) VALUES(?,?,?,?,?,?,?,?,?,?)`, c.ID, c.ConnectorID, c.Title, c.Model, c.Folder, c.CreatedAt, c.UpdatedAt, c.Thinking, defaults.ShowThinking, c.AutoAllow)
		return err
	}); err != nil {
		return err
	}
	_, err = s.chats.Send(c, kind, configDir, ChatSendRequest{Text: prompt})
	return err
}

func truncateMonitor(value string, limit int) string {
	r := []rune(value)
	if len(r) <= limit {
		return value
	}
	return string(r[:limit]) + "…"
}

func (s *Server) applyChangedMonitorActions(ctx context.Context, m monitor, run monitorRunResult) {
	if run.status != "changed" {
		return
	}
	actionErr := s.fireMonitorActions(ctx, m, run)
	if actionErr == "" {
		return
	}
	log.Printf("monitor actions %s: %s", m.ID, actionErr)
	if run.runID != "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE monitor_run SET error=CASE WHEN error='' THEN ? ELSE error||'; '||? END WHERE id=?`, actionErr, actionErr, run.runID); err != nil {
			log.Printf("record monitor action error %s: %v", m.ID, err)
		}
	}
	if run.startedAt != "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE monitor SET last_error=CASE WHEN last_run_at=? THEN ? ELSE last_error END WHERE id=?`, run.startedAt, actionErr, m.ID); err != nil {
			log.Printf("record monitor action error %s: %v", m.ID, err)
		}
	}
}
