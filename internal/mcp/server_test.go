package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"my-tickets/internal/api"
	"my-tickets/internal/db"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolsAndAudit(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	}()
	server := NewServer(api.New(database, "ai", true))
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	}()

	count := 0
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		if tool != nil {
			count++
		}
	}
	if count != 9 {
		t.Fatalf("tool count = %d, want 9", count)
	}

	boardResult := callTool(t, session, "create_board", map[string]any{"name": "AI Board", "short_name": "AI", "statuses": []string{"Backlog", "Done"}})
	var board struct {
		ID string `json:"id"`
	}
	decodeText(t, boardResult, &board)
	ticketResult := callTool(t, session, "create_ticket", map[string]any{"board_id": board.ID, "title": "MCP ticket"})
	var ticket struct {
		ID string `json:"id"`
	}
	decodeText(t, ticketResult, &ticket)
	callTool(t, session, "move_ticket", map[string]any{"id": ticket.ID, "status": "done"})
	getResult := callTool(t, session, "get_ticket", map[string]any{"id": ticket.ID})
	var got struct {
		StatusID string `json:"status_id"`
	}
	decodeText(t, getResult, &got)
	var doneID string
	if err := database.QueryRow(`SELECT id FROM status WHERE board_id=? AND name=?`, board.ID, "Done").Scan(&doneID); err != nil {
		t.Fatal(err)
	}
	if got.StatusID != doneID {
		t.Fatalf("status_id = %q, want %q", got.StatusID, doneID)
	}
	var second struct {
		ID string `json:"id"`
	}
	decodeText(t, callTool(t, session, "create_ticket", map[string]any{"board_id": board.ID, "title": "Second"}), &second)
	callTool(t, session, "move_ticket", map[string]any{"id": second.ID, "status": "Done"})
	var order string
	if err := database.QueryRow(`SELECT ticket_ids FROM ticket_order WHERE status_id=?`, doneID).Scan(&order); err != nil {
		t.Fatal(err)
	}
	if want := `["` + ticket.ID + `","` + second.ID + `"]`; order != want {
		t.Fatalf("done order = %s, want %s", order, want)
	}
	searchResult := callTool(t, session, "search_tickets", map[string]any{"query": "MCP ticket"})
	var found []any
	decodeText(t, searchResult, &found)
	if len(found) != 1 {
		t.Fatalf("search results = %d, want 1", len(found))
	}

	var transitions, histories int
	if err := database.QueryRow(`SELECT COUNT(*) FROM ticket_status_transition WHERE ticket_id=? AND user_id='ai'`, ticket.ID).Scan(&transitions); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM ticket_history WHERE ticket_id=? AND user_id='ai' AND content LIKE 'Status changed%'`, ticket.ID).Scan(&histories); err != nil {
		t.Fatal(err)
	}
	if transitions != 2 || histories != 1 {
		t.Fatalf("audit rows = transitions %d, histories %d", transitions, histories)
	}

	failure := callTool(t, session, "move_ticket", map[string]any{"id": ticket.ID, "status": "missing"})
	if !failure.IsError {
		t.Fatal("unknown status did not return tool error")
	}
}

func callTool(t *testing.T, session *sdk.ClientSession, name string, args any) *sdk.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func decodeText(t *testing.T, result *sdk.CallToolResult, out any) {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool error: %#v", result.Content)
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("content type %T", result.Content[0])
	}
	if err := json.Unmarshal([]byte(text.Text), out); err != nil {
		t.Fatal(err)
	}
}
