package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func itemHistoryOf(t *testing.T, handler http.Handler, itemType, id string) []itemHistory {
	t.Helper()
	var entries []itemHistory
	if code := requestJSON(t, handler, http.MethodGet, "/api/history?item_type="+itemType+"&item_id="+id, nil, &entries); code != http.StatusOK {
		t.Fatalf("history %s %s = %d", itemType, id, code)
	}
	return entries
}

func changeOf(entry itemHistory, field string) *historyChange {
	for i := range entry.Changes {
		if entry.Changes[i].Field == field {
			return &entry.Changes[i]
		}
	}
	return nil
}

func TestItemHistorySkillAndRestore(t *testing.T) {
	database, handler := newAPI(t)
	ai := New(database, "ai", false, "dev", t.TempDir())
	var skill Skill
	if code := requestJSON(t, handler, http.MethodPost, "/api/skills", map[string]any{"name": "notes", "description": "First", "body": "v1"}, &skill); code != http.StatusCreated {
		t.Fatalf("create = %d", code)
	}
	if code := requestJSON(t, ai, http.MethodPut, "/api/skills/"+skill.ID, map[string]any{"body": "v2"}, nil); code != http.StatusOK {
		t.Fatalf("update = %d", code)
	}
	// An update that changes nothing is not recorded.
	if code := requestJSON(t, handler, http.MethodPut, "/api/skills/"+skill.ID, map[string]any{"body": "v2"}, nil); code != http.StatusOK {
		t.Fatalf("no-op update = %d", code)
	}
	entries := itemHistoryOf(t, handler, "skill", skill.ID)
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	updated, created := entries[0], entries[1]
	if created.Action != "created" || created.UserIsAI || created.ItemName != "notes" || changeOf(created, "body") == nil || changeOf(created, "body").New != "v1" {
		t.Fatalf("created entry = %#v", created)
	}
	body := changeOf(updated, "body")
	if updated.Action != "updated" || !updated.UserIsAI || len(updated.Changes) != 1 || body.Old != "v1" || body.New != "v2" {
		t.Fatalf("updated entry = %#v", updated)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/history/"+created.ID+"/restore", nil, &skill); code != http.StatusOK || skill.Body != "v1" || skill.Description != "First" {
		t.Fatalf("restore = %d, %#v", code, skill)
	}
	entries = itemHistoryOf(t, handler, "skill", skill.ID)
	if len(entries) != 3 || entries[0].Action != "restored" || changeOf(entries[0], "body").New != "v1" {
		t.Fatalf("after restore = %#v", entries)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/skills/"+skill.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
	entries = itemHistoryOf(t, handler, "skill", skill.ID)
	if entries[0].Action != "deleted" || entries[0].ItemName != "notes" || !strings.Contains(string(entries[0].Snapshot), `"body":"v1"`) {
		t.Fatalf("deleted entry = %#v", entries[0])
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/history/"+created.ID+"/restore", nil, nil); code != http.StatusNotFound {
		t.Fatalf("restore deleted skill = %d, want 404", code)
	}
}

func TestItemHistoryHidesSecrets(t *testing.T) {
	keyring.MockInit()
	database, handler := newAPI(t)
	var server mcpServer
	if code := requestJSON(t, handler, http.MethodPost, "/api/mcp-servers", map[string]any{"name": "remote", "kind": "http", "url": "https://mcp.example.test/mcp", "auth": "bearer", "bearer_token": "first-secret"}, &server); code != http.StatusCreated {
		t.Fatalf("create MCP = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/mcp-servers/"+server.ID, map[string]any{"bearer_token": "second-secret"}, nil); code != http.StatusOK {
		t.Fatalf("update MCP = %d", code)
	}
	var connector struct {
		ID string `json:"id"`
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/connectors", map[string]any{"kind": "local", "name": "Local", "base_url": "http://localhost:11434/v1", "secret": "connector-secret"}, &connector); code != http.StatusCreated {
		t.Fatalf("create connector = %d", code)
	}
	entries := itemHistoryOf(t, handler, "mcp_server", server.ID)
	if len(entries) != 2 || changeOf(entries[0], "bearer_token") == nil || !changeOf(entries[0], "bearer_token").Hidden {
		t.Fatalf("MCP entries = %#v", entries)
	}
	if entries := itemHistoryOf(t, handler, "connector", connector.ID); len(entries) != 1 || changeOf(entries[0], "secret") == nil || !changeOf(entries[0], "secret").Hidden {
		t.Fatalf("connector entries = %#v", entries)
	}
	var leaks int
	if err := database.QueryRow(`SELECT COUNT(*) FROM item_history WHERE changes_json || snapshot_json LIKE '%-secret%'`).Scan(&leaks); err != nil || leaks != 0 {
		t.Fatalf("secret values stored: %d, %v", leaks, err)
	}
}

func TestItemHistoryScheduledTaskAndPurge(t *testing.T) {
	database, handler := newAPI(t)
	var task scheduledTask
	if code := requestJSON(t, handler, http.MethodPost, "/api/scheduled-tasks", map[string]any{"title": "Daily", "prompt": "Do work", "schedule": "0 9 * * *"}, &task); code != http.StatusCreated {
		t.Fatalf("create task = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/scheduled-tasks/"+task.ID, map[string]any{"title": "Daily", "prompt": "Do more work", "schedule": "0 9 * * *"}, nil); code != http.StatusOK {
		t.Fatalf("update task = %d", code)
	}
	entries := itemHistoryOf(t, handler, "scheduled_task", task.ID)
	if len(entries) != 2 || len(entries[0].Changes) != 1 || changeOf(entries[0], "prompt") == nil {
		t.Fatalf("task entries = %#v", entries)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/history?item_type=board&item_id=x", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("bad item_type = %d", code)
	}
	old := time.Now().UTC().Add(-91 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	if _, err := database.Exec(`UPDATE item_history SET created_at=? WHERE action='created'`, old); err != nil {
		t.Fatal(err)
	}
	if err := handler.(*Server).PurgeNotifications(); err != nil {
		t.Fatal(err)
	}
	if entries := itemHistoryOf(t, handler, "scheduled_task", task.ID); len(entries) != 1 || entries[0].Action != "updated" {
		t.Fatalf("after purge = %#v", entries)
	}
}
