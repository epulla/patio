package reminder

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"my-tickets/internal/db"
)

func TestDue(t *testing.T) {
	now := time.Date(2026, time.July, 2, 0, 15, 0, 0, time.FixedZone("PDT", -7*60*60))
	database, err := db.Open(filepath.Join(t.TempDir(), "reminders.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if _, err := database.Exec(`INSERT INTO board (id, name, short_name) VALUES ('active-board', 'Active', 'active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO board (id, name, short_name, deleted_at) VALUES ('deleted-board', 'Deleted', 'deleted', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO "user" (id, name, email, email_verified, role) VALUES ('reminder-user', 'Reminder User', 'reminder@localhost', 1, 'user')`); err != nil {
		t.Fatal(err)
	}

	timedPast := now.UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	timedFuture := now.UTC().Add(time.Minute).Format(time.RFC3339Nano)
	tests := []struct {
		id        string
		kind      string
		startsAt  string
		allDay    int
		deletedAt sql.NullString
		boardID   sql.NullString
		wantDue   bool
	}{
		{id: "future-timed", kind: "reminder", startsAt: timedFuture, boardID: sql.NullString{String: "active-board", Valid: true}},
		{id: "event", kind: "event", startsAt: timedPast, boardID: sql.NullString{String: "active-board", Valid: true}},
		{id: "deleted", kind: "reminder", startsAt: timedPast, deletedAt: sql.NullString{String: "2026-01-01T00:00:00Z", Valid: true}, boardID: sql.NullString{String: "active-board", Valid: true}},
		{id: "all-day-today", kind: "reminder", startsAt: now.Format("2006-01-02"), allDay: 1, boardID: sql.NullString{String: "active-board", Valid: true}, wantDue: true},
		{id: "past-timed", kind: "reminder", startsAt: timedPast, boardID: sql.NullString{String: "active-board", Valid: true}, wantDue: true},
		{id: "deleted-board-reminder", kind: "reminder", startsAt: timedPast, boardID: sql.NullString{String: "deleted-board", Valid: true}},
	}
	for _, test := range tests {
		if _, err := database.Exec(`
			INSERT INTO calendar_item (id, kind, title, description, starts_at, all_day, board_id, created_by, deleted_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'reminder-user', ?)
		`, test.id, test.kind, test.id, "details", test.startsAt, test.allDay, test.boardID, test.deletedAt); err != nil {
			t.Fatalf("insert %s: %v", test.id, err)
		}
	}

	got, err := Due(context.Background(), database, now)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, test := range tests {
		if test.wantDue {
			want = append(want, test.id)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("Due() returned %d items, want %d: %+v", len(got), len(want), got)
	}
	for i, item := range got {
		if item.ID != want[i] {
			t.Errorf("Due()[%d].ID = %q, want %q", i, item.ID, want[i])
		}
		if item.Title != item.ID || item.Description != "details" {
			t.Errorf("Due()[%d] = %+v, want title %q and description details", i, item, item.ID)
		}
	}
	got, err = Due(context.Background(), database, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("second Due() returned items: %+v", got)
	}
}

func TestDueClaimErrorRollsBack(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "reminders.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	now := time.Now().UTC()
	if _, err := database.Exec(`
		INSERT INTO calendar_item (id, kind, title, starts_at, created_by)
		VALUES ('due', 'reminder', 'Due', ?, 'local')
	`, now.Add(-time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER fail_claim BEFORE UPDATE OF notified_at ON calendar_item BEGIN SELECT RAISE(ABORT, 'claim failed'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := Due(context.Background(), database, now); err == nil {
		t.Fatal("Due() succeeded despite claim failure")
	}
	var notified sql.NullString
	if err := database.QueryRow(`SELECT notified_at FROM calendar_item WHERE id = 'due'`).Scan(&notified); err != nil {
		t.Fatalf("read reminder: %v", err)
	}
	if notified.Valid {
		t.Errorf("notified_at = %q after failed claim, want NULL", notified.String)
	}
}
