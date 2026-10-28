package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestOpenCreatesExpectedTablesAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Errorf("close first database: %v", err)
		}
	})
	tables := []string{"user", "board", "status", "ticket_type", "ticket", "ticket_history", "ticket_comment", "status_order", "ticket_order", "ticket_type_order", "board_counter", "old_ticket_code", "ticket_status_transition", "calendar_item", "connector", "allowed_folder", "chat", "chat_message", "chat_approval", "mcp_server", "skill", "ticket_link", "item_history", "scheduled_task"}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			var count int
			if err := first.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("table %q missing", table)
			}
		})
	}
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("close second database: %v", err)
		}
	})
	var migrations int
	if err := second.QueryRow("PRAGMA user_version").Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != 38 {
		t.Fatalf("migration count = %d, want 38", migrations)
	}
	if _, err := second.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES('migration-test','test','now','now')`); err != nil {
		t.Fatal(err)
	}
	var thinking int
	if err := second.QueryRow(`SELECT thinking FROM chat WHERE id='migration-test'`).Scan(&thinking); err != nil {
		t.Fatalf("read chat thinking default: %v", err)
	}
	if thinking != 0 {
		t.Fatalf("new chat thinking = %d, want 0", thinking)
	}
	if _, err := second.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at) VALUES('migration-question','migration-test','question','{}','now')`); err != nil {
		t.Fatalf("insert question message: %v", err)
	}
	var answered int
	if err := second.QueryRow(`SELECT answered FROM chat_message WHERE id='migration-question'`).Scan(&answered); err != nil || answered != 0 {
		t.Fatalf("question answered default = %d, %v", answered, err)
	}
	var toolsColumn int
	if err := second.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('chat') WHERE name='tools'`).Scan(&toolsColumn); err != nil || toolsColumn != 0 {
		t.Fatalf("chat tools column count = %d, %v", toolsColumn, err)
	}
	var autoAllow, auto int
	if err := second.QueryRow(`SELECT auto_allow FROM chat WHERE id='migration-test'`).Scan(&autoAllow); err != nil {
		t.Fatalf("read chat auto_allow default: %v", err)
	}
	if autoAllow != 0 {
		t.Fatalf("new chat auto_allow = %d, want 0", autoAllow)
	}
	if _, err := second.Exec(`INSERT INTO chat_approval(id,chat_id,tool_name,input_json,created_at) VALUES('migration-approval','migration-test','write','{}','now')`); err != nil {
		t.Fatal(err)
	}
	if err := second.QueryRow(`SELECT auto FROM chat_approval WHERE id='migration-approval'`).Scan(&auto); err != nil {
		t.Fatalf("read approval auto default: %v", err)
	}
	if auto != 0 {
		t.Fatalf("new approval auto = %d, want 0", auto)
	}
	if _, err := second.Exec(`INSERT INTO scheduled_task(id,title,prompt,schedule,created_at,updated_at) VALUES('migration-task','task','prompt','* * * * *','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Exec(`INSERT INTO scheduled_task_run(id,task_id,scheduled_for,status,created_at) VALUES('migration-waiting','migration-task','now','waiting','now')`); err != nil {
		t.Fatalf("insert waiting run: %v", err)
	}
}

func TestBotMigrationMapsDisabledMartinPersonaToNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-bot-setting.db")
	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for version, entry := range entries[:36] {
		script, e := migrations.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = legacy.Exec(string(script)); e != nil {
			t.Fatalf("apply %s: %v", entry.Name(), e)
		}
		if _, e = legacy.Exec(fmt.Sprintf("PRAGMA user_version=%d", version+1)); e != nil {
			t.Fatal(e)
		}
	}
	if _, err := legacy.Exec(`INSERT INTO app_setting(key,value) VALUES('martin_persona','false')`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var got string
	if err := database.QueryRow(`SELECT value FROM app_setting WHERE key='default_bot_id'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "\"none\"" {
		t.Fatalf("default_bot_id=%q, want JSON string none", got)
	}
	var old int
	if err := database.QueryRow(`SELECT count(*) FROM app_setting WHERE key='martin_persona'`).Scan(&old); err != nil || old != 0 {
		t.Fatalf("martin_persona rows=%d, %v", old, err)
	}
}

func TestThinkingMigrationUpdatesExistingChatsAndRemovesTools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for version, entry := range entries[:14] {
		script, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := legacy.Exec(string(script)); err != nil {
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
		if _, err := legacy.Exec(fmt.Sprintf("PRAGMA user_version=%d", version+1)); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO connector(id,kind,name,created_at) VALUES('local-connector','local','Local','now')`,
		`INSERT INTO connector(id,kind,name,created_at) VALUES('claude-connector','claude','Claude','now')`,
		`INSERT INTO chat(id,connector_id,title,model,created_at,updated_at,thinking,tools) VALUES('local-chat','local-connector','Local','', 'now','now',0,1)`,
		`INSERT INTO chat(id,connector_id,title,model,created_at,updated_at,thinking,tools) VALUES('claude-chat','claude-connector','Claude','', 'now','now',0,1)`,
		`INSERT INTO app_setting(key,value) VALUES('tools','false')`,
	} {
		if _, err := legacy.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for id, want := range map[string]int{"local-chat": 0, "claude-chat": 1} {
		var thinking int
		if err := database.QueryRow(`SELECT thinking FROM chat WHERE id=?`, id).Scan(&thinking); err != nil || thinking != want {
			t.Errorf("%s thinking = %d, %v; want %d", id, thinking, err, want)
		}
	}
	var settingCount, toolsColumn int
	if err := database.QueryRow(`SELECT COUNT(*) FROM app_setting WHERE key='tools'`).Scan(&settingCount); err != nil || settingCount != 0 {
		t.Errorf("tools setting count = %d, %v", settingCount, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('chat') WHERE name='tools'`).Scan(&toolsColumn); err != nil || toolsColumn != 0 {
		t.Errorf("tools column count = %d, %v", toolsColumn, err)
	}
	var missingColumnErr error = database.QueryRow(`SELECT tools FROM chat`).Scan(new(int))
	if missingColumnErr == nil || !strings.Contains(strings.ToLower(missingColumnErr.Error()), "no such column") {
		t.Errorf("tools column query error = %v", missingColumnErr)
	}
}

func TestMarkdownMigrationConvertsHTMLOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "markdown.db")
	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for version, entry := range entries[:20] {
		script, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := legacy.Exec(string(script)); err != nil {
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
		if _, err := legacy.Exec(fmt.Sprintf("PRAGMA user_version=%d", version+1)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := legacy.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO ticket(id,title,description,status_id,board_id,reporter_id) VALUES('html','HTML','<p>Hello <strong>world</strong></p><ol><li data-list="bullet">Item</li></ol>','todo','board','local'), ('markdown','Markdown','**already markdown**','todo','board','local')`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var converted, unchanged string
	if err := database.QueryRow(`SELECT description FROM ticket WHERE id='html'`).Scan(&converted); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT description FROM ticket WHERE id='markdown'`).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(converted, "**world**") || !strings.Contains(converted, "- Item") {
		t.Fatalf("converted description = %q", converted)
	}
	if unchanged != "**already markdown**" {
		t.Fatalf("markdown changed to %q", unchanged)
	}
}

func TestWatchDetectsChangesFromAnotherConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Errorf("close first database: %v", err)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	changed := make(chan struct{}, 1)
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- Watch(ctx, first, 50*time.Millisecond, func() {
			select {
			case changed <- struct{}{}:
			default:
			}
		})
	}()

	time.Sleep(100 * time.Millisecond)
	second, err := Open(path)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if _, err := second.Exec(`UPDATE "user" SET name = ? WHERE id = ?`, "Changed", "local"); err != nil {
		if closeErr := second.Close(); closeErr != nil {
			t.Errorf("close second database: %v", closeErr)
		}
		cancel()
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		cancel()
		t.Fatal(err)
	}

	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("watcher did not detect external database change within 2s")
	}
	cancel()
	select {
	case err := <-watchDone:
		if err != nil {
			t.Fatalf("watch returned error after cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not return after cancellation")
	}
}

func TestTransactionWritesAfterReadWhileAnotherProcessWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	app, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	mcp, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mcp.Close() })
	if _, err := app.Exec(`CREATE TABLE counter (n INTEGER)`); err != nil {
		t.Fatal(err)
	}
	tx, err := mcp.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM counter`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	otherWrite := make(chan error, 1)
	go func() {
		_, err := app.Exec(`INSERT INTO counter (n) VALUES (1)`)
		otherWrite <- err
	}()
	time.Sleep(100 * time.Millisecond)
	if _, err := tx.Exec(`INSERT INTO counter (n) VALUES (?)`, n+1); err != nil {
		_ = tx.Rollback()
		t.Fatalf("write after read in transaction: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-otherWrite; err != nil {
		t.Fatalf("concurrent write: %v", err)
	}
}
