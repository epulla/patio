package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
	"patio/internal/db"
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

func TestMonitorRunNow(t *testing.T) {
	keyring.MockInit()
	database, server := newMCPCheckServer(t, Options{})
	file := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(file, []byte(`{"value":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	serverID := insertRunnerMCPServer(t, database)
	m := insertRunnerMonitor(t, database, serverID, "read_file", map[string]any{"path": file}, "changed", "")
	var nextBefore string
	if err := database.QueryRow(`SELECT next_run_at FROM monitor WHERE id=?`, m.ID).Scan(&nextBefore); err != nil {
		t.Fatal(err)
	}
	var updated monitor
	if code := requestJSON(t, server, "POST", "/api/monitors/"+m.ID+"/run", nil, &updated); code != 200 {
		t.Fatalf("run-now status=%d", code)
	}
	if updated.ID != m.ID || updated.LastStatus == nil || *updated.LastStatus != "ok" {
		t.Fatalf("updated monitor=%#v", updated)
	}
	var nextAfter string
	var runs int
	if err := database.QueryRow(`SELECT next_run_at FROM monitor WHERE id=?`, m.ID).Scan(&nextAfter); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM monitor_run WHERE monitor_id=?`, m.ID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if nextAfter != nextBefore || runs != 1 {
		t.Fatalf("next_run_at %q -> %q, runs=%d", nextBefore, nextAfter, runs)
	}
	if code := requestJSON(t, server, "POST", "/api/monitors/missing/run", nil, nil); code != 404 {
		t.Fatalf("unknown monitor status=%d", code)
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

func TestMonitorActionsNotifyTicketChat(t *testing.T) {
	database, server, fake, board, m := monitorActionFixture(t, true)
	m.Actions = `{"notify":true,"ticket":{"board_id":"` + board.ID + `","title":"{{name}} ({{count}})"},"chat":{"prompt":"{{name}} found {{count}}: {{items}}"}}`
	stamp := now()
	runID := uid()
	if _, err := database.Exec(`UPDATE monitor SET actions=?,last_run_at=? WHERE id=?`, m.Actions, stamp, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO monitor_run(id,monitor_id,started_at,status,result_hash,new_count,error) VALUES(?,?,?,'changed','',1,'')`, runID, m.ID, stamp); err != nil {
		t.Fatal(err)
	}
	s := monitorRunResult{status: "changed", runID: runID, startedAt: stamp, newItems: []json.RawMessage{json.RawMessage(`{"id":"a"}`)}}
	server.applyChangedMonitorActions(context.Background(), m, s)
	var notifications, tickets int
	if err := database.QueryRow(`SELECT count(*) FROM notification WHERE title='runner test: 1 new'`).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM ticket WHERE board_id=? AND title='runner test (1)'`, board.ID).Scan(&tickets); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 || tickets != 1 {
		t.Fatalf("notifications=%d tickets=%d", notifications, tickets)
	}
	select {
	case line := <-fake.input:
		if !strings.Contains(line, "runner test found 1: - {\\\"id\\\":\\\"a\\\"}") {
			t.Fatalf("chat input=%s", line)
		}
	case <-time.After(time.Second):
		t.Fatal("chat driver did not receive prompt")
	}
	var chatTitle string
	if err := database.QueryRow(`SELECT title FROM chat WHERE title LIKE 'Monitor: runner test%'`).Scan(&chatTitle); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorQuietRunDoesNotFireActions(t *testing.T) {
	database, server, _, board, m := monitorActionFixture(t, false)
	m.Actions = `{"notify":true,"ticket":{"board_id":"` + board.ID + `","title":"quiet"}}`
	server.applyChangedMonitorActions(context.Background(), m, monitorRunResult{status: "ok"})
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM notification`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	var tickets int
	if err := database.QueryRow(`SELECT count(*) FROM ticket`).Scan(&tickets); err != nil {
		t.Fatal(err)
	}
	if count != 0 || tickets != 0 {
		t.Fatalf("notifications=%d tickets=%d", count, tickets)
	}
}

func TestMonitorMissingDefaultConnectorRecordsActionError(t *testing.T) {
	database, server, _, board, m := monitorActionFixture(t, false)
	m.Actions = `{"ticket":{"board_id":"` + board.ID + `","title":"{{name}}"},"chat":{"prompt":"start"}}`
	stamp, runID := now(), uid()
	if _, err := database.Exec(`UPDATE monitor SET actions=?,last_run_at=? WHERE id=?`, m.Actions, stamp, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO monitor_run(id,monitor_id,started_at,status,result_hash,new_count,error) VALUES(?,?,?,'changed','',0,'')`, runID, m.ID, stamp); err != nil {
		t.Fatal(err)
	}
	server.applyChangedMonitorActions(context.Background(), m, monitorRunResult{status: "changed", runID: runID, startedAt: stamp, result: "result"})
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM ticket WHERE board_id=? AND title='runner test'`, board.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tickets=%d", count)
	}
	var actionError string
	if err := database.QueryRow(`SELECT error FROM monitor_run WHERE id=?`, runID).Scan(&actionError); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(actionError, "no default connector configured") {
		t.Fatalf("run error=%q", actionError)
	}
}

func monitorActionFixture(t *testing.T, withConnector bool) (*sql.DB, *Server, *pipeChatProcess, testBoard, monitor) {
	t.Helper()
	root := t.TempDir()
	database, err := db.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	fake := newPipeChatProcess()
	opts := Options{DBPath: filepath.Join(root, "test.db"), findClaude: func() (string, error) { return "claude-test", nil }, commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil }, processFactory: func(string, []string, []string, string) (chatProcess, error) { return fake, nil }}
	server := New(database, "local", false, "test", root, opts)
	t.Cleanup(func() { _ = server.Close(); _ = database.Close() })
	if withConnector {
		id := createTestConnector(t, database)
		connector, err := json.Marshal(id)
		if err != nil {
			t.Fatal(err)
		}
		model, _ := json.Marshal("default-model")
		if _, err := database.Exec(`INSERT INTO app_setting(key,value) VALUES('connector_id',?),('model',?)`, string(connector), string(model)); err != nil {
			t.Fatal(err)
		}
	}
	board := createBoard(t, server.handler, "ACT", nil)
	serverID := insertMCPCheckServer(t, database, "stdio", "test", "[]", "[]", "", "none", false)
	m := insertRunnerMonitor(t, database, serverID, "tool", map[string]any{}, "new_items", "id")
	return database, server, fake, board, m
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
