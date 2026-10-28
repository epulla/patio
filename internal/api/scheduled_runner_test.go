package api

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"patio/internal/db"
)

func TestScheduledClaimLateCollapseAndOverlap(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s := New(database, "local", false, "dev", t.TempDir())
	started := time.Now().Add(-3 * time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")
	id := uid()
	if _, err := database.Exec(`INSERT INTO scheduled_task(id,title,prompt,schedule,enabled,next_run_at,created_at,updated_at) VALUES(?,?,?,'* * * * *',1,?,?,?)`, id, "task", "prompt", started, now(), now()); err != nil {
		t.Fatal(err)
	}
	claims, err := s.claimScheduled(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || !claims[0].late {
		t.Fatalf("claims = %#v, want one late claim", claims)
	}
	var runs int
	if err := database.QueryRow(`SELECT count(*) FROM scheduled_task_run WHERE task_id=?`, id).Scan(&runs); err != nil || runs != 1 {
		t.Fatalf("runs = %d, err = %v", runs, err)
	}
	claims, err = s.claimScheduled(context.Background(), time.Now())
	if err != nil || len(claims) != 0 {
		t.Fatalf("second claim = %#v, err = %v", claims, err)
	}
	chatID := uid()
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES(?,'run',?,?)`, chatID, now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE scheduled_task_run SET chat_id=? WHERE task_id=?`, chatID, id); err != nil {
		t.Fatal(err)
	}
	rt := &chatRuntime{busy: true}
	s.chats.chats[chatID] = rt
	for _, busy := range []bool{true, false} {
		rt.busy = busy
		if _, err := database.Exec(`UPDATE scheduled_task SET next_run_at=? WHERE id=?`, started, id); err != nil {
			t.Fatal(err)
		}
		claims, err = s.claimScheduled(context.Background(), time.Now())
		if err != nil || len(claims) != 1 || claims[0].skipped != busy {
			t.Fatalf("busy=%v claim = %#v, err = %v", busy, claims, err)
		}
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM scheduled_task_run WHERE chat_id=?`, chatID).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("stopped run status = %q, err = %v", status, err)
	}
}

func TestScheduledRunCompletesChatTurn(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	root := t.TempDir()
	connectorID := createTestConnector(t, database)
	if _, err := database.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai,persona_prompt) VALUES('billy','Billy','billy@scheduled',1,1,'Billy prompt')`); err != nil {
		t.Fatal(err)
	}
	fake := newPipeChatProcess()
	s := New(database, "local", false, "dev", root, Options{
		DBPath:         filepath.Join(root, "test.db"),
		findClaude:     func() (string, error) { return "claude-test", nil },
		commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil },
		processFactory: func(string, []string, []string, string) (chatProcess, error) { return fake, nil },
	})
	id, due := uid(), time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if _, err := database.Exec(`INSERT INTO scheduled_task(id,title,prompt,connector_id,bot_id,schedule,run_at,enabled,next_run_at,created_at,updated_at) VALUES(?,?,?,?,'billy','',?,1,?,?,?)`, id, "task", "do this", connectorID, due, due, now(), now()); err != nil {
		t.Fatal(err)
	}
	claims, err := s.claimScheduled(context.Background(), time.Now())
	if err != nil || len(claims) != 1 || claims[0].late {
		t.Fatalf("claim = %#v, err = %v", claims, err)
	}
	if err := s.startScheduled(claims[0]); err != nil {
		t.Fatal(err)
	}
	var chatID, status string
	var late bool
	if err := database.QueryRow(`SELECT chat_id,status,late FROM scheduled_task_run WHERE id=?`, claims[0].runID).Scan(&chatID, &status, &late); err != nil {
		t.Fatal(err)
	}
	c, err := s.scanChat(database.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, chatID))
	if err != nil {
		t.Fatal(err)
	}
	if c.ScheduledTaskID == nil || *c.ScheduledTaskID != id || c.BotID == nil || *c.BotID != "billy" || c.ScheduledLate || status != "running" || late {
		t.Fatalf("chat=%#v status=%q late=%v", c, status, late)
	}
	select {
	case <-fake.input:
	case <-time.After(time.Second):
		t.Fatal("runtime did not receive task prompt")
	}
	if _, err := io.WriteString(fake.stdoutW, `{"type":"result","is_error":false,"result":"done"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		if err := database.QueryRow(`SELECT status FROM scheduled_task_run WHERE id=?`, claims[0].runID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		return status == "done"
	})
}

func TestScheduledRunNow(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	root := t.TempDir()
	connectorID := createTestConnector(t, database)
	fake := newPipeChatProcess()
	s := New(database, "local", false, "dev", root, Options{
		DBPath:         filepath.Join(root, "test.db"),
		findClaude:     func() (string, error) { return "claude-test", nil },
		commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil },
		processFactory: func(string, []string, []string, string) (chatProcess, error) { return fake, nil },
	})
	id, next := uid(), "2099-01-01T00:00:00.000Z"
	if _, err := database.Exec(`INSERT INTO scheduled_task(id,title,prompt,connector_id,schedule,enabled,next_run_at,created_at,updated_at) VALUES(?,'task','do this',?,'0 9 * * *',0,?,?,?)`, id, connectorID, next, now(), now()); err != nil {
		t.Fatal(err)
	}
	var out struct {
		ChatID string `json:"chat_id"`
	}
	if code := requestJSON(t, s, http.MethodPost, "/api/scheduled-tasks/"+id+"/run", nil, &out); code != http.StatusOK || out.ChatID == "" {
		t.Fatalf("run now = %d %+v", code, out)
	}
	var taskID, status, nextAfter string
	if err := database.QueryRow(`SELECT c.scheduled_task_id,r.status,t.next_run_at FROM chat c JOIN scheduled_task_run r ON r.chat_id=c.id JOIN scheduled_task t ON t.id=r.task_id WHERE c.id=?`, out.ChatID).Scan(&taskID, &status, &nextAfter); err != nil {
		t.Fatal(err)
	}
	if taskID != id || status != "running" || nextAfter != next {
		t.Fatalf("task=%q status=%q next=%q", taskID, status, nextAfter)
	}
	if code := requestJSON(t, s, http.MethodPost, "/api/scheduled-tasks/"+id+"/run", nil, nil); code != http.StatusConflict {
		t.Fatalf("second run now = %d, want 409", code)
	}
	if code := requestJSON(t, s, http.MethodPost, "/api/scheduled-tasks/missing/run", nil, nil); code != http.StatusNotFound {
		t.Fatalf("missing run now = %d, want 404", code)
	}
}

func TestScheduledMissingConnectorFails(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s := New(database, "local", false, "dev", t.TempDir())
	claim := scheduledClaim{task: scheduledTask{ID: uid(), Title: "task"}, runID: uid()}
	if _, err := database.Exec(`INSERT INTO scheduled_task(id,title,prompt,schedule,enabled,created_at,updated_at) VALUES(?,?,?,'* * * * *',1,?,?)`, claim.task.ID, "task", "prompt", now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO scheduled_task_run(id,task_id,scheduled_for,status,created_at) VALUES(?,?,?,'running',?)`, claim.runID, claim.task.ID, now(), now()); err != nil {
		t.Fatal(err)
	}
	if err := s.startScheduled(claim); err != errChatConnectorMissing {
		t.Fatalf("start error = %v", err)
	}
	s.failScheduled(claim, errChatConnectorMissing)
	var status, message string
	if err := database.QueryRow(`SELECT status,error FROM scheduled_task_run WHERE id=?`, claim.runID).Scan(&status, &message); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || message == "" {
		t.Fatalf("run = %q %q", status, message)
	}
}
