package api

import (
	"net/http"
	"testing"
)

func TestMonitorCRUD(t *testing.T) {
	database, handler := newAPI(t)
	if _, err := database.Exec(`INSERT INTO mcp_server(id,name,kind,command,args_json,env_keys_json,url,auth,enabled,created_at) VALUES('server','test','stdio','test','[]','[]','','none',1,'now')`); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"name": "test", "mcp_server_id": "server", "tool": "hello", "schedule": "@every 1m"}
	var created monitor
	if code := requestJSON(t, handler, http.MethodPost, "/api/monitors", input, &created); code != http.StatusCreated {
		t.Fatalf("create status=%d", code)
	}
	var listed []monitor
	if code := requestJSON(t, handler, http.MethodGet, "/api/monitors", nil, &listed); code != 200 || len(listed) != 1 {
		t.Fatalf("list status=%d count=%d", code, len(listed))
	}
	var got monitor
	if code := requestJSON(t, handler, http.MethodGet, "/api/monitors/"+created.ID, nil, &got); code != 200 {
		t.Fatalf("get status=%d", code)
	}
	input["enabled"] = false
	if code := requestJSON(t, handler, http.MethodPut, "/api/monitors/"+created.ID, input, &got); code != 200 || got.NextRunAt != nil {
		t.Fatalf("disable status=%d next=%v", code, got.NextRunAt)
	}
	var runs []map[string]any
	if code := requestJSON(t, handler, http.MethodGet, "/api/monitors/"+created.ID+"/runs", nil, &runs); code != 200 || len(runs) != 0 {
		t.Fatalf("runs status=%d", code)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/monitors/"+created.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete status=%d", code)
	}
}

func TestMonitorValidation(t *testing.T) {
	_, handler := newAPI(t)
	tests := []struct {
		name  string
		input map[string]any
	}{
		{"schedule", map[string]any{"name": "x", "mcp_server_id": "missing", "tool": "x", "schedule": "bad"}},
		{"args", map[string]any{"name": "x", "mcp_server_id": "missing", "tool": "x", "schedule": "@every 30s", "args": []any{}}},
		{"new items", map[string]any{"name": "x", "mcp_server_id": "missing", "tool": "x", "schedule": "@every 1m", "condition": "new_items"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code := requestJSON(t, handler, http.MethodPost, "/api/monitors", tt.input, nil); code != http.StatusBadRequest {
				t.Fatalf("status=%d", code)
			}
		})
	}
}
