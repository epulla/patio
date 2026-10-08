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
	tables := []string{"user", "board", "status", "ticket_type", "ticket", "ticket_history", "ticket_comment", "status_order", "ticket_order", "ticket_type_order", "board_counter", "old_ticket_code", "ticket_status_transition"}
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
	if migrations != 3 {
		t.Fatalf("migration count = %d, want 3", migrations)
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
