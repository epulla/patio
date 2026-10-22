package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCopilotLivePermission(t *testing.T) {
	if os.Getenv("PATIO_COPILOT_LIVE") != "1" {
		t.Skip("set PATIO_COPILOT_LIVE=1 for logged-in Copilot permission check")
	}
	binary := os.Getenv("PATIO_BINARY")
	if binary == "" {
		t.Fatal("PATIO_BINARY is required for Patio MCP")
	}
	database, _, root := chatAPI(t)
	connectorID := createCopilotTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	events := make(chan any, 64)
	server := New(database, "local", false, "live", root, Options{
		DBPath:        filepath.Join(root, "test.db"),
		mcpExecutable: binary,
		findCopilot:   func() (string, error) { return "/Users/epulla/.local/bin/copilot", nil },
		Emit:          func(_ string, event any) { events <- event },
	})
	server.chats.approvalPollInterval = 100 * time.Millisecond
	defer server.Close()
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "Run the shell command `true` with bash, then reply ok"}, nil); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}

	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	approvalID := ""
	done := false
	for !done {
		select {
		case raw := <-events:
			event, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch event["type"] {
			case "approval":
				if approvalID != "" {
					continue
				}
				if err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&approvalID); err != nil {
					t.Fatalf("approval event without pending row: %v", err)
				}
				if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": true}, nil); code != http.StatusOK {
					t.Fatalf("allow approval = %d", code)
				}
			case "done":
				done = true
			case "error":
				t.Fatalf("Copilot turn failed: %v", event)
			}
		case <-deadline.C:
			t.Fatalf("Copilot permission turn timed out; approval id=%q", approvalID)
		}
	}
	if approvalID == "" {
		t.Fatal("Copilot completed without requesting shell approval")
	}
	var status, toolName string
	if err := database.QueryRow(`SELECT status,tool_name FROM chat_approval WHERE id=? AND chat_id=?`, approvalID, chatID).Scan(&status, &toolName); err != nil || status != "allowed" || toolName == "" {
		t.Fatalf("approval = (%q,%q), %v", status, toolName, err)
	}
	var activeTurns int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_turn WHERE chat_id=?`, chatID).Scan(&activeTurns); err != nil || activeTurns != 0 {
		t.Fatalf("active chat turns = %d, %v", activeTurns, err)
	}
}
