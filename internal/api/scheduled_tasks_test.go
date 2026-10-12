package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestScheduledTaskCRUD(t *testing.T) {
	_, handler, _ := chatAPI(t)
	input := map[string]any{"title": "Daily", "prompt": "Do work", "schedule": "0 9 * * *", "enabled": true}
	var created scheduledTask
	if code := requestJSON(t, handler, http.MethodPost, "/api/scheduled-tasks", input, &created); code != http.StatusCreated || created.NextRunAt == nil {
		t.Fatalf("create = %d, %#v", code, created)
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
