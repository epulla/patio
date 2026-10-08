package db

import (
	"path/filepath"
	"testing"
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
