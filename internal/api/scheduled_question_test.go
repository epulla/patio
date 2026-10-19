package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"patio/internal/db"
)

func TestScheduledQuestionNotifiesWaitsAndAnswerCompletesRun(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "scheduled-question.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	root := t.TempDir()
	connectorID := createTestConnector(t, database)
	var nativeTitle, nativeLink string
	server := New(database, "local", false, "test", root, Options{Notify: func(title, _, link string) error { nativeTitle, nativeLink = title, link; return nil }})
	defer server.Close()
	chatID := createDriverTestChat(t, database, connectorID, root)
	taskID, runID := uid(), uid()
	if _, err := database.Exec(`INSERT INTO scheduled_task(id,title,prompt,schedule,next_run_at,created_at,updated_at) VALUES(?, 'Daily report','prompt','* * * * *',?,?,?)`, taskID, now(), now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE chat SET scheduled_task_id=? WHERE id=?`, taskID, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO scheduled_task_run(id,task_id,chat_id,scheduled_for,status,created_at) VALUES(?,?,?,?,'running',?)`, runID, taskID, chatID, now(), now()); err != nil {
		t.Fatal(err)
	}
	input := `{"questions":[{"header":"Scope","question":"Which scope?","options":[{"label":"Small"},{"label":"Full"}]}]}`
	if err := server.chats.emitToolCall(chatID, "patio.ask_user", input); err != nil {
		t.Fatal(err)
	}
	if err := server.chats.finishTurn(chatID, "", nil); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM scheduled_task_run WHERE id=?`, runID).Scan(&status); err != nil || status != "waiting" {
		t.Fatalf("run status = %q, %v", status, err)
	}
	var title, link string
	if err := database.QueryRow(`SELECT title,link FROM notification WHERE kind='scheduled'`).Scan(&title, &link); err != nil {
		t.Fatal(err)
	}
	if title != "Daily report has a question" || link != "/ai/chats/"+chatID || nativeTitle != title || nativeLink != link {
		t.Fatalf("notification = %q %q; native = %q %q", title, link, nativeTitle, nativeLink)
	}
	var questionID string
	if err := database.QueryRow(`SELECT id FROM chat_message WHERE chat_id=? AND role='question'`, chatID).Scan(&questionID); err != nil {
		t.Fatal(err)
	}
	if _, err := InsertChatMessage(database, chatID, "user", "Scope: Small"); err != nil {
		t.Fatal(err)
	}
	if err := server.chats.finishTurn(chatID, "Done", nil); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT status FROM scheduled_task_run WHERE id=?`, runID).Scan(&status); err != nil || status != "done" {
		t.Fatalf("answered run status = %q, %v", status, err)
	}
	var answered bool
	if err := database.QueryRow(`SELECT answered FROM chat_message WHERE id=?`, questionID).Scan(&answered); err != nil || !answered {
		t.Fatalf("question answered = %v, %v", answered, err)
	}
}

func TestWaitingScheduledRunDoesNotBlockNextClaim(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "scheduled-claims.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server := New(database, "local", false, "test", t.TempDir())
	next := time.Now().Add(-time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")
	taskID := uid()
	if _, err := database.Exec(`INSERT INTO scheduled_task(id,title,prompt,schedule,enabled,next_run_at,created_at,updated_at) VALUES(?,?,?,'* * * * *',1,?,?,?)`, taskID, "task", "prompt", next, now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO scheduled_task_run(id,task_id,scheduled_for,status,created_at) VALUES(?,?,?,'waiting',?)`, uid(), taskID, next, now()); err != nil {
		t.Fatal(err)
	}
	claims, err := server.claimScheduled(context.Background(), time.Now())
	if err != nil || len(claims) != 1 || claims[0].skipped {
		t.Fatalf("claims = %#v, %v", claims, err)
	}
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM scheduled_task_run WHERE task_id=?`, taskID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("run count = %d, %v", count, err)
	}
}
