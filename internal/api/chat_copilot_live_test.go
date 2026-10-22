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
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "Use patio-list_boards to list boards, then reply ok"}, nil); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}

	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
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
				t.Fatalf("Patio MCP tool unexpectedly requested approval: %v", event)
			case "done":
				done = true
			case "error":
				t.Fatalf("Copilot turn failed: %v", event)
			}
		case <-deadline.C:
			t.Fatal("Copilot Patio tool turn timed out")
		}
	}
	var toolCalls, approvals int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='tool' AND content='patio-list_boards'`, chatID).Scan(&toolCalls); err != nil || toolCalls == 0 {
		t.Fatalf("Patio tool call rows = %d, %v", toolCalls, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_approval WHERE chat_id=?`, chatID).Scan(&approvals); err != nil || approvals != 0 {
		t.Fatalf("Patio approvals = %d, %v", approvals, err)
	}
	var activeTurns int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_turn WHERE chat_id=?`, chatID).Scan(&activeTurns); err != nil || activeTurns != 0 {
		t.Fatalf("active chat turns = %d, %v", activeTurns, err)
	}
}
