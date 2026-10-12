package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"
	connectorcli "patio/internal/connector"
)

func TestClaudeLiveExternalMCP(t *testing.T) {
	if os.Getenv("PATIO_CLAUDE_LIVE") != "1" {
		t.Skip("set PATIO_CLAUDE_LIVE=1 for logged-in Claude checks")
	}
	binary := os.Getenv("PATIO_BINARY")
	if binary == "" {
		t.Fatal("PATIO_BINARY is required")
	}
	claudePath, err := connectorcli.Find("claude")
	if err != nil {
		t.Fatal(err)
	}
	keyring.MockInit()
	database, _, root := chatAPI(t)
	dbPath := filepath.Join(root, "test.db")
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	events := make(chan map[string]any, 64)
	server := New(database, "local", false, "live", root, Options{
		DBPath:        dbPath,
		mcpExecutable: binary,
		findClaude:    func() (string, error) { return claudePath, nil },
		Emit:          func(_ string, event any) { events <- event.(map[string]any) },
	})
	defer server.Close()
	if code := requestJSON(t, server, http.MethodPost, "/api/mcp-servers", map[string]any{
		"name": "tickets-copy", "kind": "stdio", "command": binary, "args": []string{"mcp", "-d", dbPath}, "enabled": true,
	}, nil); code != http.StatusCreated {
		t.Fatalf("create tickets-copy MCP server = %d", code)
	}
	var external mcpServer
	if err := database.QueryRow(mcpServerSelect+` WHERE name='tickets-copy'`).Scan(&external.ID, &external.Name, &external.Kind, &external.Command, &external.argsJSON, &external.envKeysJSON, &external.URL, &external.Auth, &external.Enabled, &external.CreatedAt); err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal([]byte(external.argsJSON), &args); err != nil {
		t.Fatal(err)
	}
	external.Args = args
	spec, err := server.mcpServerCheckSpec(context.Background(), external)
	if err != nil {
		t.Fatal(err)
	}
	toolCtx, cancelTool := context.WithTimeout(context.Background(), 10*time.Second)
	session, cleanup, err := connectMCP(toolCtx, spec)
	if err == nil {
		_, err = session.CallTool(toolCtx, &mcp.CallToolParams{Name: "list_boards", Arguments: map[string]any{}})
		cleanup()
	}
	cancelTool()
	if err != nil {
		t.Fatalf("tickets-copy live tool preflight: %v", err)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{
		"text": "Use the tickets-copy list_boards tool and reply with the number of boards.",
	}, nil); code != http.StatusAccepted {
		t.Fatalf("send live prompt = %d", code)
	}
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	toolUsed := false
	var seen []string
	for {
		select {
		case event := <-events:
			seen = append(seen, eventType(event))
			switch event["type"] {
			case "tool":
				var message ChatMessage
				if raw, ok := event["message"]; ok {
					message, _ = raw.(ChatMessage)
				}
				if strings.Contains(message.Content, "mcp__tickets-copy__list_boards") {
					toolUsed = true
				}
			case "approval":
				continue
			case "done":
				if !toolUsed {
					var content string
					if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='tool' AND content LIKE '%mcp__tickets-copy__list_boards%'`, chatID).Scan(&content); err != nil {
						t.Fatalf("tickets-copy tool call not recorded: %v", err)
					}
				}
				return
			case "error":
				if event["message"] != nil {
					t.Fatalf("live chat error: %v", event["message"])
				}
			}
		case <-deadline.C:
			rows, err := database.Query(`SELECT role,content FROM chat_message WHERE chat_id=? ORDER BY rowid`, chatID)
			if err != nil {
				t.Fatal(err)
			}
			var messages []string
			for rows.Next() {
				var role, content string
				if err := rows.Scan(&role, &content); err != nil {
					t.Fatal(err)
				}
				messages = append(messages, role+": "+content)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
			t.Fatalf("live Claude external MCP prompt timed out; events=%v messages=%v", seen, messages)
		case <-poll.C:
			var approvalID string
			err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&approvalID)
			if err == nil {
				if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": true}, nil); code != http.StatusOK {
					t.Fatalf("allow external MCP approval = %d", code)
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("load pending live approval: %v", err)
			}
		}
	}
}

func TestClaudeLiveSkillLoad(t *testing.T) {
	if os.Getenv("PATIO_CLAUDE_LIVE") != "1" {
		t.Skip("set PATIO_CLAUDE_LIVE=1 for logged-in Claude checks")
	}
	binary := os.Getenv("PATIO_BINARY")
	if binary == "" {
		t.Fatal("PATIO_BINARY is required")
	}
	claudePath, err := connectorcli.Find("claude")
	if err != nil {
		t.Fatal(err)
	}
	keyring.MockInit()
	database, _, root := chatAPI(t)
	dbPath := filepath.Join(root, "test.db")
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	events := make(chan map[string]any, 64)
	server := New(database, "local", false, "live", root, Options{
		DBPath:        dbPath,
		mcpExecutable: binary,
		findClaude:    func() (string, error) { return claudePath, nil },
		Emit:          func(_ string, event any) { events <- event.(map[string]any) },
	})
	defer server.Close()
	if code := requestJSON(t, server, http.MethodPost, "/api/skills", map[string]string{
		"name": "pirate-voice", "description": "Use when the user asks for a pirate greeting.",
		"body": "Always answer in pirate speak and end with 'Arrr!'",
	}, nil); code != http.StatusCreated {
		t.Fatalf("create pirate skill = %d", code)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{
		"text": "Give me a pirate greeting.",
	}, nil); code != http.StatusAccepted {
		t.Fatalf("send live prompt = %d", code)
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	toolUsed := false
	for {
		select {
		case event := <-events:
			switch eventType(event) {
			case "tool":
				var message ChatMessage
				if raw, ok := event["message"]; ok {
					message, _ = raw.(ChatMessage)
				}
				if strings.Contains(message.Content, "mcp__patio__load_skill") {
					toolUsed = true
				}
			case "done":
				var message ChatMessage
				if raw, ok := event["message"]; ok {
					message, _ = raw.(ChatMessage)
				}
				if !toolUsed {
					t.Fatalf("Claude did not call mcp__patio__load_skill; reply=%q", message.Content)
				}
				if !strings.HasSuffix(strings.TrimSpace(message.Content), "Arrr!") {
					t.Fatalf("live Claude reply = %q, want suffix Arrr!", message.Content)
				}
				return
			case "error":
				t.Fatalf("live chat error: %v", event["message"])
			}
		case <-deadline.C:
			var messages []string
			rows, err := database.Query(`SELECT role,content FROM chat_message WHERE chat_id=? ORDER BY rowid`, chatID)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var role, content string
				if err := rows.Scan(&role, &content); err != nil {
					t.Fatal(err)
				}
				messages = append(messages, role+": "+content)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
			rows, err = database.Query(`SELECT tool_name,status FROM chat_approval WHERE chat_id=? ORDER BY rowid`, chatID)
			if err != nil {
				t.Fatal(err)
			}
			var approvals []string
			for rows.Next() {
				var toolName, status string
				if err := rows.Scan(&toolName, &status); err != nil {
					t.Fatal(err)
				}
				approvals = append(approvals, toolName+": "+status)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
			t.Fatalf("live Claude skill prompt timed out; messages=%v approvals=%v", messages, approvals)
		}
	}
}

func eventType(event map[string]any) string {
	value, _ := event["type"].(string)
	return value
}
