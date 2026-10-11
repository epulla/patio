package db

import (
	"context"
	"path/filepath"
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
	tables := []string{"user", "board", "status", "ticket_type", "ticket", "ticket_history", "ticket_comment", "status_order", "ticket_order", "ticket_type_order", "board_counter", "old_ticket_code", "ticket_status_transition", "calendar_item", "connector", "allowed_folder", "chat", "chat_message", "chat_approval", "mcp_server", "skill"}
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
	if migrations != 14 {
		t.Fatalf("migration count = %d, want 14", migrations)
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
	var tools int
	if err := second.QueryRow(`SELECT tools FROM chat WHERE id='migration-test'`).Scan(&tools); err != nil {
		t.Fatalf("read chat tools default: %v", err)
	}
	if tools != 1 {
		t.Fatalf("new chat tools = %d, want 1", tools)
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
