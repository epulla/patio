package mcp

import (
	"context"
	"database/sql"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"patio/internal/db"
)

func TestQueryAndSchema(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	addQueryTools(server, database)
	st, ct := sdk.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, query := range []string{"SELECT 42 AS answer", "WITH x AS (SELECT 42 AS answer) SELECT answer FROM x"} {
		result := callTool(t, session, "query_db", map[string]any{"query": query})
		var got queryOutput
		decodeText(t, result, &got)
		if len(got.Rows) != 1 || got.Rows[0][0] != float64(42) {
			t.Fatalf("%s: %#v", query, got)
		}
	}
	result := callTool(t, session, "query_db", map[string]any{"query": "WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<501) SELECT x FROM n"})
	var limited queryOutput
	decodeText(t, result, &limited)
	if len(limited.Rows) != 500 || !limited.Truncated {
		t.Fatalf("limit: %d rows truncated=%v", len(limited.Rows), limited.Truncated)
	}
	before := int64(0)
	if err := database.QueryRow(`SELECT COUNT(*) FROM board`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"UPDATE board SET name='x'", "DELETE FROM board", "INSERT INTO board(id,name,short_name) VALUES('x','x','x')", "DROP TABLE board", "ATTACH DATABASE ':memory:' AS x", "PRAGMA user_version", "SELECT 1; SELECT 2", "WITH x AS (SELECT 1) DELETE FROM board"} {
		if _, err := runQuery(context.Background(), database, query); err == nil {
			t.Errorf("accepted %q", query)
		}
	}
	var after int64
	if err := database.QueryRow(`SELECT COUNT(*) FROM board`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("board count changed: %d -> %d", before, after)
	}
	resource, err := session.ReadResource(context.Background(), &sdk.ReadResourceParams{URI: "patio://schema"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resource.Contents) != 1 || !strings.Contains(resource.Contents[0].Text, "erDiagram") {
		t.Fatal("schema resource unreadable")
	}
}

func TestSchemaMatchesDatabase(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "schema.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	doc, err := schemaDoc.ReadFile("schema.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range schemaTables(t, database) {
		match := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(table) + `\s*\{([^}]*)\}`).FindSubmatch(doc)
		if match == nil {
			t.Errorf("schema.md missing table %s", table)
			continue
		}
		rows, err := database.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var col string
			if err := rows.Scan(&col); err != nil {
				t.Fatal(err)
			}
			if !regexp.MustCompile(`\b` + regexp.QuoteMeta(col) + `\b`).Match(match[1]) {
				t.Errorf("schema.md missing column %s.%s", table, col)
			}
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func schemaTables(t *testing.T, database *sql.DB) []string {
	t.Helper()
	rows, err := database.Query(`SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}
