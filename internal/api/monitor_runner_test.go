package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestMonitorRunnerNewItems(t *testing.T) {
	keyring.MockInit()
	database, server := newMCPCheckServer(t, Options{})
	file := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(file, []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	id := insertRunnerMCPServer(t, database)
	m := insertRunnerMonitor(t, database, id, "read_file", map[string]any{"path": file}, "new_items", "id")
	first, err := server.runMonitor(context.Background(), m, time.Now())
	if err != nil || first.status != "ok" {
		t.Fatalf("baseline=%#v err=%v", first, err)
	}
	if err := os.WriteFile(file, []byte(`[{"id":"a"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	m, _ = scanMonitor(database.QueryRow(`SELECT `+monitorColumns+` FROM monitor WHERE id=?`, m.ID))
	second, err := server.runMonitor(context.Background(), m, time.Now().Add(time.Minute))
	if err != nil || second.status != "changed" || len(second.newItems) != 1 {
		t.Fatalf("new item=%#v err=%v", second, err)
	}
	third, err := server.runMonitor(context.Background(), m, time.Now().Add(2*time.Minute))
	if err != nil || third.status != "ok" {
		t.Fatalf("quiet=%#v err=%v", third, err)
	}
	var count int
	if err := database.QueryRow(`SELECT new_count FROM monitor_run WHERE monitor_id=? ORDER BY started_at LIMIT 1`, m.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("baseline new_count=%d err=%v", count, err)
	}
}

func TestMonitorRunnerFailureRecoveryNotifications(t *testing.T) {
	keyring.MockInit()
	database, server := newMCPCheckServer(t, Options{})
	id := insertRunnerMCPServer(t, database)
	m := insertRunnerMonitor(t, database, id, "missing_tool", map[string]any{}, "changed", "")
	for i := 0; i < 2; i++ {
		m, _ = scanMonitor(database.QueryRow(`SELECT `+monitorColumns+` FROM monitor WHERE id=?`, m.ID))
		if _, err := server.runMonitor(context.Background(), m, time.Now().Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`UPDATE monitor SET tool='zebra' WHERE id=?`, m.ID); err != nil {
		t.Fatal(err)
	}
	m, _ = scanMonitor(database.QueryRow(`SELECT `+monitorColumns+` FROM monitor WHERE id=?`, m.ID))
	if _, err := server.runMonitor(context.Background(), m, time.Now().Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var failures, recoveries int
	if err := database.QueryRow(`SELECT count(*) FROM notification WHERE title LIKE 'Monitor failing:%'`).Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM notification WHERE title LIKE 'Monitor recovered:%'`).Scan(&recoveries); err != nil {
		t.Fatal(err)
	}
	if failures != 1 || recoveries != 1 {
		t.Fatalf("failure notifications=%d recovery notifications=%d", failures, recoveries)
	}
}

func insertRunnerMCPServer(t *testing.T, database *sql.DB) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal([]string{"-test.run=^$"})
	if err != nil {
		t.Fatal(err)
	}
	envJSON, keysJSON, err := encodeMCPEnv(map[string]string{"HEALTH_CHECK_TEST_ENV": "from-keychain", "PATIO_MCP_TEST_SERVER": "1"})
	if err != nil {
		t.Fatal(err)
	}
	id := insertMCPCheckServer(t, database, "stdio", executable, string(args), keysJSON, "", "none", false)
	if err := keyring.Set(connectorService, mcpServerSecret(id, "env"), envJSON); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertRunnerMonitor(t *testing.T, database *sql.DB, serverID, tool string, args map[string]any, condition, idPath string) monitor {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	stamp := now()
	id := uid()
	_, err = database.Exec(`INSERT INTO monitor(id,name,mcp_server_id,tool,args,schedule,next_run_at,"condition",id_path,actions,enabled,created_at,updated_at) VALUES(?,?,?,?,?,'@every 1m',? ,?,?,'{}',1,?,?)`, id, "runner test", serverID, tool, string(encoded), stamp, condition, idPath, stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	m, err := scanMonitor(database.QueryRow(`SELECT `+monitorColumns+` FROM monitor WHERE id=?`, id))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
