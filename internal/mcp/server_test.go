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
	server := NewServer(api.New(database, "ai", true, "dev"), "dev")
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
	if count != 13 {
		t.Fatalf("tool count = %d, want 13", count)
	}

	boardResult := callTool(t, session, "create_board", map[string]any{
		"name": "AI Board", "short_name": "AI", "statuses": []map[string]any{
			{"name": "Backlog"}, {"name": "Done", "hide_after_days": 2},
		},
	})
	var board struct {
		ID string `json:"id"`
	}
	decodeText(t, boardResult, &board)
	var boardData struct {
		Statuses []struct {
			Name          string `json:"name"`
			Type          string `json:"type"`
			HideAfterDays *int   `json:"hide_after_days"`
		} `json:"statuses"`
	}
	decodeText(t, callTool(t, session, "get_board", map[string]any{"board_id": board.ID}), &boardData)
	if len(boardData.Statuses) != 2 || boardData.Statuses[0].Name != "Backlog" || boardData.Statuses[0].Type != "classic" {
		t.Fatalf("Backlog status = %#v, want classic column", boardData.Statuses)
	}
	doneStatus := boardData.Statuses[1]
	if doneStatus.Name != "Done" || doneStatus.Type != "temporary" || doneStatus.HideAfterDays == nil || *doneStatus.HideAfterDays != 2 {
		t.Fatalf("Done status = %#v, want temporary column hiding after 2 days", doneStatus)
	}
	defaultDaysBoard := callTool(t, session, "create_board", map[string]any{
		"name": "Temporary Board", "short_name": "TMP",
		"statuses": []map[string]any{{"name": "Done", "type": "temporary"}},
	})
	var temporaryBoard struct {
		ID string `json:"id"`
	}
	decodeText(t, defaultDaysBoard, &temporaryBoard)
	decodeText(t, callTool(t, session, "get_board", map[string]any{"board_id": temporaryBoard.ID}), &boardData)
	if len(boardData.Statuses) != 1 || boardData.Statuses[0].Type != "temporary" || boardData.Statuses[0].HideAfterDays == nil || *boardData.Statuses[0].HideAfterDays != 1 {
		t.Fatalf("temporary status = %#v, want 1-day temporary column", boardData.Statuses)
	}
	invalidType := callTool(t, session, "create_board", map[string]any{
		"name": "Invalid Board", "short_name": "BAD",
		"statuses": []map[string]any{{"name": "Done", "type": "other"}},
	})
	if !invalidType.IsError {
		t.Fatal("invalid column type did not return tool error")
	}
	ticketResult := callTool(t, session, "create_ticket", map[string]any{"board_id": board.ID, "title": "MCP ticket", "due_date": "2026-10-10"})
	var ticket struct {
		ID string `json:"id"`
	}
	decodeText(t, ticketResult, &ticket)
	calendarResult := callTool(t, session, "create_calendar_item", map[string]any{
		"kind": "reminder", "title": "Ticket reminder", "starts_at": "2026-10-10T12:00:00Z", "ticket_id": ticket.ID,
	})
	var calendarItem struct {
		ID            string `json:"id"`
		Title         string `json:"title"`
		TicketID      string `json:"ticket_id"`
		CreatedByIsAI bool   `json:"created_by_is_ai"`
	}
	decodeText(t, calendarResult, &calendarItem)
	if calendarItem.TicketID != ticket.ID || !calendarItem.CreatedByIsAI {
		t.Fatalf("created calendar item = %#v, want linked ticket and created_by_is_ai", calendarItem)
	}
	listCalendar := func() struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
		Tickets []struct {
			ID      string `json:"id"`
			DueDate string `json:"due_date"`
		} `json:"tickets"`
	} {
		var result struct {
			Items []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"items"`
			Tickets []struct {
				ID      string `json:"id"`
				DueDate string `json:"due_date"`
			} `json:"tickets"`
		}
		decodeText(t, callTool(t, session, "list_calendar", map[string]any{"from": "2026-10-01", "to": "2026-10-31"}), &result)
		return result
	}
	listed := listCalendar()
	if len(listed.Items) != 1 || listed.Items[0].ID != calendarItem.ID || len(listed.Tickets) != 1 || listed.Tickets[0].ID != ticket.ID || listed.Tickets[0].DueDate != "2026-10-10" {
		t.Fatalf("calendar listing = %#v, want linked item and ticket due 2026-10-10", listed)
	}
	callTool(t, session, "update_calendar_item", map[string]any{"id": calendarItem.ID, "title": "Updated reminder"})
	updated := listCalendar()
	if len(updated.Items) != 1 || updated.Items[0].Title != "Updated reminder" {
		t.Fatalf("updated calendar items = %#v, want updated title", updated.Items)
	}
	deleteResult := callTool(t, session, "delete_calendar_item", map[string]any{"id": calendarItem.ID})
	deleteText, ok := deleteResult.Content[0].(*sdk.TextContent)
	if deleteResult.IsError || !ok || deleteText.Text == "" {
		t.Fatalf("delete result = %#v, want non-empty success text", deleteResult)
	}
	if remaining := listCalendar(); len(remaining.Items) != 0 {
		t.Fatalf("calendar items after delete = %#v, want none", remaining.Items)
	}
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

func TestMCPInstructions(t *testing.T) {
	server := NewServer(nil, "dev")
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	}()
	want := "You act as Martin, your AI companion for your everyday and weekly tasks. You should be supportive, give recommendations, have a friendly and funny tone (but also you should be concise, direct, terse, not too verbose), make some jokes sometimes. Your changes are labeled AI in the app"
	if got := session.InitializeResult().Instructions; got != want {
		t.Fatalf("instructions = %q, want %q", got, want)
	}
}

func TestCheck(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	}()
	server := NewServer(api.New(database, "ai", true, "dev"), "dev")
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	tools, _, err := Check(context.Background(), clientTransport)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if tool.Name == "list_boards" {
			return
		}
	}
	t.Fatalf("tools do not include list_boards: %#v", tools)
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
