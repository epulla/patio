package db

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	closeOnError := func(err error) (*sql.DB, error) {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("%w; close database: %v", err, closeErr)
		}
		return nil, err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return closeOnError(fmt.Errorf("read migrations: %w", err))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for version, entry := range entries {
		var applied int
		if err := db.QueryRow("PRAGMA user_version").Scan(&applied); err != nil {
			return closeOnError(fmt.Errorf("check migration %s: %w", entry.Name(), err))
		}
		if applied >= version+1 {
			continue
		}
		script, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return closeOnError(fmt.Errorf("read migration %s: %w", entry.Name(), err))
		}
		tx, err := db.Begin()
		if err != nil {
			return closeOnError(fmt.Errorf("begin migration: %w", err))
		}
		if _, err = tx.Exec(string(script)); err == nil {
			_, err = tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version+1))
		}
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				return closeOnError(fmt.Errorf("apply migration %s: %w; rollback: %v", entry.Name(), err, rollbackErr))
			}
			return closeOnError(fmt.Errorf("apply migration %s: %w", entry.Name(), err))
		}
		if err = tx.Commit(); err != nil {
			return closeOnError(fmt.Errorf("commit migration %s: %w", entry.Name(), err))
		}
	}
	if _, err := db.Exec(`INSERT INTO "user" (id, name, email, email_verified, role) VALUES ('local', 'Local', 'local@localhost', 1, 'admin') ON CONFLICT(id) DO NOTHING`); err != nil {
		return closeOnError(fmt.Errorf("seed local user: %w", err))
	}
	return db, nil
}
