package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate"
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
			if version+1 == 21 {
				err = convertLegacyMarkdown(tx)
			}
		}
		if err == nil {
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

var htmlTag = regexp.MustCompile(`<\s*[a-zA-Z][^>]*>`)

func convertLegacyMarkdown(tx *sql.Tx) error {
	for _, tableColumn := range [][2]string{{"ticket", "description"}, {"ticket_comment", "content"}} {
		query := fmt.Sprintf(`SELECT rowid, %s FROM %s WHERE instr(%s, '<') > 0`, tableColumn[1], tableColumn[0], tableColumn[1])
		rows, err := tx.Query(query)
		if err != nil {
			return err
		}
		type item struct {
			id   int64
			text string
		}
		var items []item
		for rows.Next() {
			var v item
			if err := rows.Scan(&v.id, &v.text); err != nil {
				rows.Close()
				return err
			}
			if htmlTag.MatchString(v.text) {
				items = append(items, v)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, v := range items {
			html := v.text
			if strings.Contains(html, `data-list="bullet"`) {
				html = strings.ReplaceAll(strings.ReplaceAll(html, "<ol", "<ul"), "</ol>", "</ul>")
			}
			converted, err := htmltomarkdown.ConvertString(html)
			if err != nil {
				return fmt.Errorf("convert %s row %d: %w", tableColumn[0], v.id, err)
			}
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET %s=? WHERE rowid=?`, tableColumn[0], tableColumn[1]), converted, v.id); err != nil {
				return err
			}
		}
	}
	return nil
}

// Watch calls changed when another connection commits. data_version is per connection, so it pins one.
func Watch(ctx context.Context, database *sql.DB, every time.Duration, changed func()) (err error) {
	conn, err := database.Conn(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("get watcher connection: %w", err)
	}
	defer func() { err = errors.Join(err, conn.Close()) }()

	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&version); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("read initial data version: %w", err)
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			var current int
			if err := conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&current); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("read data version: %w", err)
			}
			if current != version {
				version = current
				changed()
			}
		}
	}
}
