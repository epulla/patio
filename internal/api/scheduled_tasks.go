package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/robfig/cron/v3"
)

type scheduledTask struct {
	ID          string  `json:"id"`
	BotID       *string `json:"bot_id"`
	Title       string  `json:"title"`
	Prompt      string  `json:"prompt"`
	ConnectorID *string `json:"connector_id"`
	Model       string  `json:"model"`
	Folder      *string `json:"folder"`
	Thinking    bool    `json:"thinking"`
	AutoAllow   bool    `json:"auto_allow"`
	Schedule    string  `json:"schedule"`
	RunAt       *string `json:"run_at"`
	Enabled     bool    `json:"enabled"`
	NextRunAt   *string `json:"next_run_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type scheduledTaskInput struct {
	Title       string  `json:"title"`
	Prompt      string  `json:"prompt"`
	ConnectorID *string `json:"connector_id"`
	BotID       *string `json:"bot_id"`
	Model       string  `json:"model"`
	Folder      *string `json:"folder"`
	Thinking    bool    `json:"thinking"`
	AutoAllow   *bool   `json:"auto_allow"`
	Schedule    string  `json:"schedule"`
	RunAt       *string `json:"run_at"`
	Enabled     *bool   `json:"enabled"`
}

const scheduledTaskColumns = `id,title,prompt,connector_id,bot_id,model,folder,thinking,auto_allow,schedule,run_at,enabled,next_run_at,created_at,updated_at`

func scanScheduledTask(row interface{ Scan(...any) error }) (scheduledTask, error) {
	var t scheduledTask
	var thinking, autoAllow, enabled int
	err := row.Scan(&t.ID, &t.Title, &t.Prompt, &t.ConnectorID, &t.BotID, &t.Model, &t.Folder, &thinking, &autoAllow, &t.Schedule, &t.RunAt, &enabled, &t.NextRunAt, &t.CreatedAt, &t.UpdatedAt)
	t.Thinking, t.AutoAllow, t.Enabled = thinking != 0, autoAllow != 0, enabled != 0
	return t, err
}

func taskSchedule(schedule string, runAt *string, enabled bool, now time.Time) (*string, error) {
	if (schedule != "") == (runAt != nil) {
		return nil, errors.New("exactly one of schedule or run_at must be set")
	}
	if runAt != nil {
		parsed, err := time.Parse(time.RFC3339, *runAt)
		if err != nil {
			return nil, errors.New("run_at must be valid RFC3339")
		}
		if !parsed.After(now) {
			return nil, errors.New("run_at must be in the future")
		}
		value := parsed.UTC().Format("2006-01-02T15:04:05.000Z")
		*runAt = value
		if enabled {
			return &value, nil
		}
		return nil, nil
	}
	parsed, err := cron.ParseStandard(schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule: %w", err)
	}
	if !enabled {
		return nil, nil
	}
	value := parsed.Next(now).UTC().Format("2006-01-02T15:04:05.000Z")
	return &value, nil
}

func (s *Server) validateScheduledTask(in *scheduledTaskInput) (*string, error) {
	if !required(in.Title) || !required(in.Prompt) {
		return nil, errors.New("title and prompt required")
	}
	if in.BotID != nil {
		var exists int
		if err := s.db.QueryRow(`SELECT 1 FROM "user" WHERE id=? AND is_ai=1 AND deleted_at IS NULL`, *in.BotID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("bot not found")
		} else if err != nil {
			return nil, err
		}
	}
	if in.ConnectorID != nil {
		var exists int
		if err := s.db.QueryRow(`SELECT 1 FROM connector WHERE id=?`, *in.ConnectorID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("connector not found")
		} else if err != nil {
			return nil, err
		}
	}
	if in.Folder != nil {
		cleaned, allowed, err := s.allowedFolderPath(*in.Folder)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, errors.New("folder is not allowed")
		}
		in.Folder = &cleaned
	}
	return taskSchedule(in.Schedule, in.RunAt, *in.Enabled, time.Now())
}

func (s *Server) listScheduledTasks(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(`SELECT ` + scheduledTaskColumns + ` FROM scheduled_task ORDER BY created_at DESC,rowid DESC`)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []scheduledTask{}
	for rows.Next() {
		t, err := scanScheduledTask(rows)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, out)
}

func (s *Server) getScheduledTask(w http.ResponseWriter, r *http.Request) {
	t, err := scanScheduledTask(s.db.QueryRow(`SELECT `+scheduledTaskColumns+` FROM scheduled_task WHERE id=?`, idParam(r, "id")))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "scheduled task not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	runs := []map[string]any{}
	rows, err := s.db.Query(`SELECT id,task_id,chat_id,scheduled_for,status,late,error,created_at FROM scheduled_task_run WHERE task_id=? ORDER BY created_at DESC,rowid DESC LIMIT 20`, t.ID)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, taskID, scheduledFor, status, msg, created string
		var chatID *string
		var late bool
		if err := rows.Scan(&id, &taskID, &chatID, &scheduledFor, &status, &late, &msg, &created); err != nil {
			errw(w, 500, err.Error())
			return
		}
		runs = append(runs, map[string]any{"id": id, "task_id": taskID, "chat_id": chatID, "scheduled_for": scheduledFor, "status": status, "late": late, "error": msg, "created_at": created})
	}
	if err := rows.Err(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, map[string]any{"task": t, "runs": runs})
}

func (s *Server) saveScheduledTask(w http.ResponseWriter, r *http.Request, create bool) {
	var in scheduledTaskInput
	if decode(r, &in) != nil {
		errw(w, 400, "invalid scheduled task JSON")
		return
	}
	yes := true
	if in.AutoAllow == nil {
		in.AutoAllow = &yes
	}
	if in.Enabled == nil {
		in.Enabled = &yes
	}
	next, err := s.validateScheduledTask(&in)
	if err != nil {
		errw(w, 400, err.Error())
		return
	}
	id, created := idParam(r, "id"), now()
	if create {
		id = uid()
		_, err = s.db.Exec(`INSERT INTO scheduled_task(`+scheduledTaskColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, in.Title, in.Prompt, in.ConnectorID, in.BotID, in.Model, in.Folder, in.Thinking, in.AutoAllow, in.Schedule, in.RunAt, in.Enabled, next, created, created)
	} else {
		_, err = s.db.Exec(`UPDATE scheduled_task SET title=?,prompt=?,connector_id=?,bot_id=?,model=?,folder=?,thinking=?,auto_allow=?,schedule=?,run_at=?,enabled=?,next_run_at=?,updated_at=? WHERE id=?`, in.Title, in.Prompt, in.ConnectorID, in.BotID, in.Model, in.Folder, in.Thinking, in.AutoAllow, in.Schedule, in.RunAt, in.Enabled, next, created, id)
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	t, err := scanScheduledTask(s.db.QueryRow(`SELECT `+scheduledTaskColumns+` FROM scheduled_task WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "scheduled task not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	code := 200
	if create {
		code = 201
	}
	write(w, code, t)
}

func (s *Server) createScheduledTask(w http.ResponseWriter, r *http.Request) {
	s.saveScheduledTask(w, r, true)
}
func (s *Server) updateScheduledTask(w http.ResponseWriter, r *http.Request) {
	s.saveScheduledTask(w, r, false)
}
func (s *Server) deleteScheduledTask(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.Exec(`DELETE FROM scheduled_task WHERE id=?`, idParam(r, "id"))
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		errw(w, 500, err.Error())
	} else if n == 0 {
		errw(w, 404, "scheduled task not found")
	} else {
		noContent(w)
	}
}

func (s *Server) previewScheduledTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Schedule string  `json:"schedule"`
		RunAt    *string `json:"run_at"`
	}
	if decode(r, &in) != nil {
		errw(w, 400, "invalid preview JSON")
		return
	}
	now := time.Now()
	_, err := taskSchedule(in.Schedule, in.RunAt, true, now)
	if err != nil {
		errw(w, 400, err.Error())
		return
	}
	if in.RunAt != nil {
		write(w, 200, map[string]any{"runs": []string{*in.RunAt}})
		return
	}
	schedule, err := cron.ParseStandard(in.Schedule)
	if err != nil {
		errw(w, 400, err.Error())
		return
	}
	runs := []string{}
	for next := now; len(runs) < 3; {
		next = schedule.Next(next)
		runs = append(runs, next.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	write(w, 200, map[string]any{"runs": runs})
}
