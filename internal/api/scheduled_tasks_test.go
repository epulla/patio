package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestScheduledTaskCRUD(t *testing.T) {
	_, handler, _ := chatAPI(t)
	input := map[string]any{"title": "Daily", "prompt": "Do work", "schedule": "0 9 * * *"}
	var created scheduledTask
	if code := requestJSON(t, handler, http.MethodPost, "/api/scheduled-tasks", input, &created); code != http.StatusCreated || created.NextRunAt == nil {
		t.Fatalf("create = %d, %#v", code, created)
	}
	if !created.AutoAllow || !created.Enabled {
		t.Fatal("omitted auto_allow and enabled should default true")
	}
	var listed []scheduledTask
	if code := requestJSON(t, handler, http.MethodGet, "/api/scheduled-tasks", nil, &listed); code != 200 || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("list = %d, %#v", code, listed)
	}
	input["title"] = "Updated"
	var updated scheduledTask
	if code := requestJSON(t, handler, http.MethodPut, "/api/scheduled-tasks/"+created.ID, input, &updated); code != 200 || updated.Title != "Updated" {
		t.Fatalf("update = %d, %#v", code, updated)
	}
	var detail struct {
		Task scheduledTask     `json:"task"`
		Runs []json.RawMessage `json:"runs"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/scheduled-tasks/"+created.ID, nil, &detail); code != 200 || detail.Task.ID != created.ID {
		t.Fatalf("get = %d, %#v", code, detail)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/scheduled-tasks/"+created.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
}

func TestCalendarScheduledOccurrences(t *testing.T) {
	database, handler, _ := chatAPI(t)
	input := map[string]any{"title": "Calendar task", "prompt": "run", "schedule": "0 9 * * *", "enabled": true}
	var task scheduledTask
	if code := requestJSON(t, handler, http.MethodPost, "/api/scheduled-tasks", input, &task); code != http.StatusCreated {
		t.Fatalf("create task = %d", code)
	}
	past := "2026-10-10T09:00:00.000Z"
	if _, err := database.Exec(`INSERT INTO scheduled_task_run(id,task_id,scheduled_for,status,late,created_at) VALUES('past-run',?,?,?,0,?)`, task.ID, past, "done", past); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Scheduled []scheduledCalendarItem `json:"scheduled"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/calendar?from=2026-10-01&to=2026-10-31", nil, &result); code != http.StatusOK {
		t.Fatalf("calendar = %d", code)
	}
	if len(result.Scheduled) < 2 {
		t.Fatalf("scheduled = %#v, want past run and cron occurrence", result.Scheduled)
	}
	foundRun, foundUpcoming := false, false
	for _, item := range result.Scheduled {
		if item.RunID != nil && *item.RunID == "past-run" {
			foundRun = true
		}
		if item.RunID == nil {
			foundUpcoming = true
		}
	}
	if !foundRun || !foundUpcoming {
		t.Fatalf("scheduled = %#v", result.Scheduled)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/calendar?from=2026-10-01&to=2026-10-31&board_id=board", nil, &result); code != http.StatusOK || len(result.Scheduled) != 0 {
		t.Fatalf("board-filtered scheduled = %d %#v", code, result.Scheduled)
	}
}

func TestScheduledTaskValidationAndPreview(t *testing.T) {
	_, handler, _ := chatAPI(t)
	for _, body := range []map[string]any{
		{"title": "x", "prompt": "x", "schedule": "bad cron"},
		{"title": "x", "prompt": "x", "schedule": "0 9 * * *", "run_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
		{"title": "x", "prompt": "x"},
	} {
		if code := requestJSON(t, handler, http.MethodPost, "/api/scheduled-tasks", body, nil); code != http.StatusBadRequest {
			t.Fatalf("invalid create %v = %d", body, code)
		}
	}
	var result struct {
		Runs []string `json:"runs"`
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/scheduled-tasks/preview", map[string]string{"schedule": "*/5 * * * *"}, &result); code != 200 || len(result.Runs) != 3 {
		t.Fatalf("preview = %d, %#v", code, result)
	}
	for i := 1; i < len(result.Runs); i++ {
		a, _ := time.Parse(time.RFC3339Nano, result.Runs[i-1])
		b, _ := time.Parse(time.RFC3339Nano, result.Runs[i])
		if !b.After(a) {
			t.Fatalf("preview not increasing: %#v", result.Runs)
		}
	}
}
