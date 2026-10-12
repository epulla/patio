package mcp

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed schema.md
var schemaDoc embed.FS

type queryArgs struct {
	Query string `json:"query" jsonschema:"One read-only SELECT, WITH, or EXPLAIN statement"`
}

type queryOutput struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated"`
}

func addQueryTools(s *sdk.Server, database *sql.DB) {
	s.AddResource(&sdk.Resource{URI: "patio://schema", Name: "Patio database schema", MIMEType: "text/markdown"}, func(context.Context, *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		data, err := schemaDoc.ReadFile("schema.md")
		if err != nil {
			return nil, err
		}
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: "patio://schema", MIMEType: "text/markdown", Text: string(data)}}}, nil
	})
	description := "Read-only SQLite query. Read patio://schema or SELECT sql FROM sqlite_schema first; sqlite_schema is authoritative and doc is fallback. Use for heavy reports and as fallback when other tools find nothing."
	sdk.AddTool[queryArgs, any](s, &sdk.Tool{Name: "query_db", Description: description}, func(ctx context.Context, _ *sdk.CallToolRequest, in queryArgs) (*sdk.CallToolResult, any, error) {
		if database == nil {
			return errorResult(fmt.Errorf("database unavailable")), nil, nil
		}
		out, err := runQuery(ctx, database, in.Query)
		if err != nil {
			return errorResult(err), nil, nil
		}
		data, err := json.Marshal(out)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(data)}}}, nil, nil
	})
}

func runQuery(parent context.Context, database *sql.DB, query string) (queryOutput, error) {
	var empty queryOutput
	statement := strings.TrimSpace(query)
	keyword := strings.Fields(statement)
	if len(keyword) == 0 {
		return empty, fmt.Errorf("query required")
	}
	first := strings.ToUpper(strings.TrimLeft(keyword[0], "("))
	if first != "SELECT" && first != "WITH" && first != "EXPLAIN" {
		return empty, fmt.Errorf("only SELECT, WITH, or EXPLAIN statements are allowed")
	}
	if !oneStatement(statement) {
		return empty, fmt.Errorf("exactly one statement is allowed")
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	var path string
	if err := database.QueryRowContext(ctx, "PRAGMA database_list").Scan(new(int), new(string), &path); err != nil {
		return empty, err
	}
	ro, err := sql.Open("sqlite", "file:"+filepath.Clean(path)+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return empty, err
	}
	defer ro.Close()
	ro.SetMaxOpenConns(1)
	conn, err := ro.Conn(ctx)
	if err != nil {
		return empty, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return empty, err
	}
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return empty, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return empty, err
	}
	out := queryOutput{Columns: columns, Rows: make([][]any, 0)}
	for rows.Next() {
		if len(out.Rows) == 500 {
			out.Truncated = true
			break
		}
		values := make([]any, len(columns))
		targets := make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return empty, err
		}
		for i, value := range values {
			if b, ok := value.([]byte); ok {
				values[i] = string(b)
			}
		}
		out.Rows = append(out.Rows, values)
	}
	if err := rows.Err(); err != nil {
		return empty, err
	}
	return out, nil
}

func oneStatement(s string) bool {
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				if i+1 < len(s) && s[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			continue
		}
		if c == '[' {
			quote = ']'
			continue
		}
		if c == ';' && strings.TrimSpace(s[i+1:]) != "" {
			return false
		}
	}
	return quote == 0
}
