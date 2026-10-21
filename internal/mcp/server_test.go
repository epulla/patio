package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"patio/internal/api"
	"patio/internal/db"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"
)

func TestValidateAskUser(t *testing.T) {
	valid := askUserArgs{Questions: []askUserQuestion{{Header: "Scope", Question: "Choose scope", Options: []askUserOption{{Label: "Small"}, {Label: "Full"}}}}}
	if err := validateAskUser(valid); err != nil {
		t.Fatalf("valid questions rejected: %v", err)
	}
	for _, input := range []askUserArgs{{}, {Questions: []askUserQuestion{{Header: "Only", Question: "Pick", Options: []askUserOption{{Label: "One"}}}}}} {
		if err := validateAskUser(input); err == nil {
			t.Fatalf("invalid questions accepted: %+v", input)
		}
	}
}

func TestAskUserToolStoresQuestionAndNotifiesScheduledRun(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "ask-user.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server, err := NewServer(api.New(database, "ai", true, "dev", t.TempDir()), "dev", database, false)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "ask-user-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	chatID, scheduledChatID := "interactive-chat", "scheduled-chat"
	for _, id := range []string{chatID, scheduledChatID} {
		if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES(?,?,?,?)`, id, id, "now", "now"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO scheduled_task(id,title,prompt,schedule,created_at,updated_at) VALUES('task','Daily report','prompt','* * * * *','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE chat SET scheduled_task_id='task' WHERE id=?`, scheduledChatID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO scheduled_task_run(id,task_id,chat_id,scheduled_for,status,created_at) VALUES('run','task',?,'now','running','now')`, scheduledChatID); err != nil {
		t.Fatal(err)
	}
	questions := []map[string]any{{"header": "Scope", "question": "Which scope?", "options": []map[string]any{{"label": "Small"}, {"label": "Full"}}}}
	call := func(id string) *sdk.CallToolResult {
		return callTool(t, session, "ask_user", map[string]any{"chat_id": id, "questions": questions})
	}
	if result := call(chatID); result.IsError {
		t.Fatalf("valid chat ask_user failed: %#v", result.Content)
	}
	var count, answered int
	var role, content string
	if err := database.QueryRow(`SELECT role,content,answered FROM chat_message WHERE chat_id=?`, chatID).Scan(&role, &content, &answered); err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Questions []askUserQuestion `json:"questions"`
		ChatID    string            `json:"chat_id"`
	}
	if err := json.Unmarshal([]byte(content), &stored); err != nil || role != "question" || answered != 0 || stored.ChatID != "" || len(stored.Questions) != 1 {
		t.Fatalf("stored question role=%q answered=%d body=%q err=%v", role, answered, content, err)
	}
	for _, args := range []map[string]any{{"questions": questions}, {"chat_id": "missing-chat", "questions": questions}} {
		result := callTool(t, session, "ask_user", args)
		if !result.IsError {
			t.Fatalf("invalid chat_id accepted: %#v", result)
		}
		text, ok := result.Content[0].(*sdk.TextContent)
		if !ok || !strings.Contains(text.Text, "ask in plain text") {
			if ok {
				t.Fatalf("chat_id error does not guide plain-text fallback: %q", text.Text)
			}
			t.Fatalf("chat_id error does not guide plain-text fallback: %#v", result.Content)
		}
	}
	if result := call(scheduledChatID); result.IsError {
		t.Fatalf("scheduled ask_user failed: %#v", result.Content)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='question'`, scheduledChatID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("scheduled questions = %d, %v", count, err)
	}
	var kind, title, link string
	if err := database.QueryRow(`SELECT kind,title,link FROM notification`).Scan(&kind, &title, &link); err != nil {
		t.Fatal(err)
	}
	if kind != "chat" || title != "Daily report has a question" || link != "/ai/chats/"+scheduledChatID {
		t.Fatalf("scheduled question notification = %q %q %q", kind, title, link)
	}
}

func TestMCPToolsAndAudit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	}()
	server, err := NewServer(api.New(database, "ai", true, "dev", filepath.Dir(dbPath)), "dev", database, false)
	if err != nil {
		t.Fatal(err)
	}
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

	expectedTools := map[string]bool{
		"list_boards": true, "get_board": true, "create_board": true, "create_ticket": true,
		"get_ticket": true, "update_ticket": true, "move_ticket": true, "search_tickets": true,
		"link_tickets": true, "unlink_tickets": true,
		"add_comment": true, "list_calendar": true, "create_calendar_item": true,
		"update_calendar_item": true, "delete_calendar_item": true,
		"list_scheduled_tasks": true, "get_scheduled_task": true, "create_scheduled_task": true, "update_scheduled_task": true, "delete_scheduled_task": true,
		"list_skills": true, "load_skill": true, "create_skill": true, "update_skill": true, "delete_skill": true,
		"list_mcp_servers": true, "create_mcp_server": true, "update_mcp_server": true, "check_mcp_server": true,
		"query_db": true, "list_connectors": true, "create_connector": true,
		"update_connector": true, "list_connector_models": true, "update_board": true, "create_column": true,
		"update_column": true, "reorder_columns": true, "reorder_boards": true, "delete_column": true, "create_ticket_type": true, "update_ticket_type": true, "list_comments": true, "get_ticket_history": true, "list_users": true,
		"get_settings_defaults": true, "update_settings_defaults": true,
		"ask_user": true, "create_notification": true,
	}
	count := 0
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		if tool != nil {
			if tool.Name == "approve" {
				t.Fatal("approve tool registered without flag")
			}
			if !expectedTools[tool.Name] {
				t.Fatalf("unexpected tool %q", tool.Name)
			}
			delete(expectedTools, tool.Name)
			count++
		}
	}
	if count != 49 {
		t.Fatalf("tool count = %d, want 49", count)
	}
	if len(expectedTools) != 0 {
		t.Fatalf("missing tools: %#v", expectedTools)
	}
	createdTask := callTool(t, session, "create_scheduled_task", map[string]any{"title": "MCP scheduled", "prompt": "do work", "model": "", "thinking": false, "schedule": "0 9 * * *", "enabled": true})
	if createdTask.IsError {
		t.Fatalf("create scheduled task: %s", createdTask.Content[0].(*sdk.TextContent).Text)
	}
	var task struct {
		ID        string `json:"id"`
		AutoAllow bool   `json:"auto_allow"`
	}
	decodeText(t, createdTask, &task)
	var tasks []struct {
		ID string `json:"id"`
	}
	decodeText(t, callTool(t, session, "list_scheduled_tasks", map[string]any{}), &tasks)
	if !task.AutoAllow || len(tasks) != 1 || tasks[0].ID != task.ID {
		t.Fatalf("scheduled task round trip: %#v %#v", task, tasks)
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
	var changeTitle, changeLink string
	if err := database.QueryRow(`SELECT title,link FROM notification WHERE kind='change' ORDER BY created_at DESC,rowid DESC LIMIT 1`).Scan(&changeTitle, &changeLink); err != nil {
		t.Fatal(err)
	}
	if changeTitle != "Martin moved AI-1 to Done" || changeLink != "/tickets/"+ticket.ID {
		t.Fatalf("move notification=%q %q", changeTitle, changeLink)
	}
	if title, body, link, ok := mcpChange(http.NotFoundHandler(), request{http.MethodDelete, "/api/tickets/AI-10", nil}, []byte(`{"id":"AI-10","code":"AI-10","title":"Example"}`), nil); !ok || title != "Martin deleted AI-10" || body != "[AI-10: Example](/tickets/AI-10)" || link != "/tickets/AI-10" {
		t.Fatalf("delete notification = %q %q %q %v", title, body, link, ok)
	}
	if title, _, _, ok := mcpChange(http.NotFoundHandler(), request{http.MethodDelete, "/api/tickets/8ac86c6e-cc4b-4a29-8ea1-132731001122", nil}, nil, nil); !ok || title != "Martin deleted a ticket" {
		t.Fatalf("UUID delete title = %q, want generic ticket title", title)
	}
	if comment := callTool(t, session, "add_comment", map[string]any{"ticket_id": ticket.ID, "content": "Looks good"}); comment.IsError {
		t.Fatalf("add comment: %#v", comment)
	}
	for _, key := range []string{ticket.ID, "AI-1"} {
		var comments, history []map[string]any
		decodeText(t, callTool(t, session, "list_comments", map[string]any{"id": key}), &comments)
		decodeText(t, callTool(t, session, "get_ticket_history", map[string]any{"id": key}), &history)
		if len(comments) != 1 || len(history) == 0 {
			t.Fatalf("by %q: comments=%d history=%d", key, len(comments), len(history))
		}
	}
	var commentTitle, commentBody string
	if err := database.QueryRow(`SELECT title,body FROM notification WHERE kind='change' ORDER BY created_at DESC,rowid DESC LIMIT 1`).Scan(&commentTitle, &commentBody); err != nil {
		t.Fatal(err)
	}
	if commentTitle != "Martin commented on AI-1" || commentBody != "[AI-1: MCP ticket](/tickets/"+ticket.ID+")" {
		t.Fatalf("comment notification = %q %q", commentTitle, commentBody)
	}
	var changesBefore int
	if err := database.QueryRow(`SELECT count(*) FROM notification WHERE kind='change'`).Scan(&changesBefore); err != nil {
		t.Fatal(err)
	}
	callTool(t, session, "list_boards", map[string]any{})
	var changesAfter int
	if err := database.QueryRow(`SELECT count(*) FROM notification WHERE kind='change'`).Scan(&changesAfter); err != nil {
		t.Fatal(err)
	}
	if changesAfter != changesBefore {
		t.Fatalf("read-only tool created change notification: before=%d after=%d", changesBefore, changesAfter)
	}
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
	linked := callTool(t, session, "link_tickets", map[string]any{"from": ticket.ID, "to": second.ID, "type": "blocks"})
	if linked.IsError {
		t.Fatalf("link tickets: %#v", linked)
	}
	var linkedTicket struct {
		Links []struct {
			Label string `json:"label"`
		} `json:"links"`
	}
	decodeText(t, callTool(t, session, "get_ticket", map[string]any{"id": ticket.ID}), &linkedTicket)
	if len(linkedTicket.Links) != 1 || linkedTicket.Links[0].Label != "blocks" {
		t.Fatalf("get_ticket links=%+v", linkedTicket.Links)
	}
	var linkID string
	if err := database.QueryRow("SELECT id FROM ticket_link WHERE from_ticket_id=? AND to_ticket_id=?", ticket.ID, second.ID).Scan(&linkID); err != nil {
		t.Fatal(err)
	}
	if result := callTool(t, session, "unlink_tickets", map[string]any{"link_id": linkID}); result.IsError {
		t.Fatalf("unlink tickets: %#v", result)
	}
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
	var sorted []struct {
		Title string `json:"title"`
	}
	decodeText(t, callTool(t, session, "search_tickets", map[string]any{"board_id": board.ID, "sort": "title", "dir": "desc", "created_from": "2000-01-01T00:00:00Z"}), &sorted)
	if len(sorted) != 2 || sorted[0].Title != "Second" || sorted[1].Title != "MCP ticket" {
		t.Fatalf("sorted search = %#v", sorted)
	}
	var limited []map[string]any
	decodeText(t, callTool(t, session, "search_tickets", map[string]any{"board_id": board.ID, "limit": 1}), &limited)
	if len(limited) != 1 || limited[0]["code"] == nil || limited[0]["description"] != nil {
		t.Fatalf("limited search = %#v, want 1 compact row", limited)
	}
	type compactBoard struct {
		Statuses []struct {
			Type          string           `json:"type"`
			HiddenTickets int              `json:"hidden_tickets"`
			Tickets       []map[string]any `json:"tickets"`
		} `json:"statuses"`
	}
	var hidden, included compactBoard
	decodeText(t, callTool(t, session, "get_board", map[string]any{"board_id": board.ID}), &hidden)
	if done := hidden.Statuses[1]; done.Type != "temporary" || done.HiddenTickets != 2 || len(done.Tickets) != 0 {
		t.Fatalf("default Done column = %#v, want 2 hidden tickets", done)
	}
	decodeText(t, callTool(t, session, "get_board", map[string]any{"board_id": board.ID, "include_done": true}), &included)
	if done := included.Statuses[1]; len(done.Tickets) != 2 || done.Tickets[0]["code"] == nil || done.Tickets[0]["description"] != nil {
		t.Fatalf("include_done Done column = %#v, want 2 compact rows", done)
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

func TestMCPServerToolsHideSecrets(t *testing.T) {
	keyring.MockInit()
	dbPath := filepath.Join(t.TempDir(), "mcp-servers.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server, err := NewServer(api.New(database, "ai", true, "dev", filepath.Dir(dbPath)), "dev", database, false)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	secrets := []string{"stdio-env-secret-723", "http-bearer-secret-941"}
	stdio := callTool(t, session, "create_mcp_server", map[string]any{
		"name": "Local stdio", "kind": "stdio", "command": "echo", "env": map[string]string{"TOKEN": secrets[0]},
	})
	var stdioServer struct {
		ID string `json:"id"`
	}
	decodeText(t, stdio, &stdioServer)
	httpServer := callTool(t, session, "create_mcp_server", map[string]any{
		"name": "Local http", "kind": "http", "url": "http://localhost:1234/mcp", "auth": "bearer", "bearer_token": secrets[1],
	})
	var httpResult struct {
		ID string `json:"id"`
	}
	decodeText(t, httpServer, &httpResult)
	updated := callTool(t, session, "update_mcp_server", map[string]any{"id": stdioServer.ID, "enabled": false})
	var updateResult struct {
		Enabled bool `json:"enabled"`
	}
	decodeText(t, updated, &updateResult)
	var mcpLink string
	if err := database.QueryRow(`SELECT link FROM notification WHERE kind='change' ORDER BY created_at DESC,rowid DESC LIMIT 1`).Scan(&mcpLink); err != nil || mcpLink != "/ai/mcp" {
		t.Fatalf("MCP server change link = %q, %v", mcpLink, err)
	}
	listed := callTool(t, session, "list_mcp_servers", map[string]any{})
	var list []struct {
		ID             string   `json:"id"`
		Enabled        bool     `json:"enabled"`
		EnvKeys        []string `json:"env_keys"`
		HasBearerToken bool     `json:"has_bearer_token"`
	}
	decodeText(t, listed, &list)
	if updateResult.Enabled || len(list) != 2 || list[0].Enabled || len(list[0].EnvKeys) != 1 {
		t.Fatalf("MCP server list/update = %#v %#v", list, updateResult)
	}
	if !list[1].HasBearerToken {
		t.Fatalf("HTTP server bearer token not recorded: %#v", list[1])
	}
	for _, result := range []*sdk.CallToolResult{stdio, httpServer, updated, listed} {
		text := result.Content[0].(*sdk.TextContent).Text
		for _, secret := range secrets {
			if strings.Contains(text, secret) {
				t.Fatalf("tool output leaked secret %q: %s", secret, text)
			}
		}
	}
}

func TestMCPConnectorTools(t *testing.T) {
	keyring.MockInit()
	dbPath := filepath.Join(t.TempDir(), "connectors.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server, err := NewServer(api.New(database, "ai", true, "dev", filepath.Dir(dbPath)), "dev", database, false)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	secret := "mcp-secret-value"
	created := callTool(t, session, "create_connector", map[string]any{"kind": "copilot", "name": "First", "secret": secret})
	var connector struct {
		ID string `json:"id"`
	}
	decodeText(t, created, &connector)
	callTool(t, session, "update_connector", map[string]any{"id": connector.ID, "name": "Updated"})
	var connectorLink string
	if err := database.QueryRow(`SELECT link FROM notification WHERE kind='change' ORDER BY created_at DESC,rowid DESC LIMIT 1`).Scan(&connectorLink); err != nil || connectorLink != "/ai/connectors" {
		t.Fatalf("connector change link = %q, %v", connectorLink, err)
	}
	listed := callTool(t, session, "list_connectors", map[string]any{})
	if listed.IsError {
		t.Fatalf("list connectors: %#v", listed)
	}
	text := listed.Content[0].(*sdk.TextContent).Text
	if strings.Contains(text, secret) || !strings.Contains(text, `"has_secret":true`) || !strings.Contains(text, `"name":"Updated"`) {
		t.Fatalf("connector listing leaks secret or misses update: %s", text)
	}
}

func TestMCPCreateNotification(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "notifications.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server, err := NewServer(api.New(database, "ai", true, "dev", filepath.Dir(dbPath)), "dev", database, false)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, link := range []string{"", "/tickets/abc", "https://example.com/report?x=1"} {
		var created struct {
			ID, Kind, Title, Body, Link string
		}
		decodeText(t, callTool(t, session, "create_notification", map[string]any{"title": "  Funds delivered  ", "body": "All **3** messages", "link": link}), &created)
		if created.ID == "" || created.Kind != "custom" || created.Title != "Funds delivered" || created.Body != "All **3** messages" || created.Link != link {
			t.Fatalf("created = %#v, link %q", created, link)
		}
	}
	for name, args := range map[string]map[string]any{
		"missing title":     {"title": "  "},
		"long title":        {"title": strings.Repeat("x", 201)},
		"long body":         {"title": "x", "body": strings.Repeat("x", 4001)},
		"http link":         {"title": "x", "link": "http://example.com"},
		"protocol-relative": {"title": "x", "link": "//evil.example"},
		"javascript link":   {"title": "x", "link": "javascript:alert(1)"},
		"relative link":     {"title": "x", "link": "tickets/abc"},
	} {
		if result := callTool(t, session, "create_notification", args); !result.IsError {
			t.Errorf("%s: want error, got %#v", name, result.Content)
		}
	}
	var custom, other int
	if err := database.QueryRow(`SELECT COUNT(*) FILTER (WHERE kind='custom'), COUNT(*) FILTER (WHERE kind<>'custom') FROM notification`).Scan(&custom, &other); err != nil || custom != 3 || other != 0 {
		t.Fatalf("custom = %d, other = %d, %v; want 3 custom and no change notification", custom, other, err)
	}
}

func TestMCPBoardStructure(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "board-structure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server, err := NewServer(api.New(database, "ai", true, "dev", t.TempDir()), "dev", database, false)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var board struct {
		ID string `json:"id"`
	}
	decodeText(t, callTool(t, session, "create_board", map[string]any{"name": "Board", "short_name": "BS", "statuses": []map[string]any{{"name": "Todo"}}}), &board)
	var updatedBoard struct {
		Name string `json:"name"`
	}
	decodeText(t, callTool(t, session, "update_board", map[string]any{"board_id": board.ID, "name": "Renamed", "short_name": "BS", "template": ""}), &updatedBoard)
	if updatedBoard.Name != "Renamed" {
		t.Fatalf("updated board = %#v", updatedBoard)
	}
	var original struct {
		ID string `json:"id"`
	}
	var data struct {
		Statuses []struct {
			ID string `json:"id"`
		} `json:"statuses"`
	}
	decodeText(t, callTool(t, session, "get_board", map[string]any{"board_id": board.ID}), &data)
	original.ID = data.Statuses[0].ID
	var column struct {
		ID string `json:"id"`
	}
	decodeText(t, callTool(t, session, "create_column", map[string]any{"board_id": board.ID, "name": "Empty", "type": "classic"}), &column)
	var updatedColumn struct {
		Name string `json:"name"`
	}
	decodeText(t, callTool(t, session, "update_column", map[string]any{"id": column.ID, "name": "Updated empty", "type": "classic"}), &updatedColumn)
	if updatedColumn.Name != "Updated empty" {
		t.Fatalf("updated column = %#v", updatedColumn)
	}
	order := []string{column.ID, original.ID}
	if result := callTool(t, session, "reorder_columns", map[string]any{"board_id": board.ID, "status_ids": order}); result.IsError {
		t.Fatalf("reorder columns: %#v", result)
	}
	if result := callTool(t, session, "reorder_boards", map[string]any{"board_ids": []string{board.ID}}); result.IsError {
		t.Fatalf("reorder boards: %#v", result)
	}
	var ticketType struct {
		ID string `json:"id"`
	}
	decodeText(t, callTool(t, session, "create_ticket_type", map[string]any{"board_id": board.ID, "name": "Bug", "color": "red"}), &ticketType)
	var updatedType struct {
		Name string `json:"name"`
	}
	decodeText(t, callTool(t, session, "update_ticket_type", map[string]any{"id": ticketType.ID, "name": "Issue", "color": "blue"}), &updatedType)
	if updatedType.Name != "Issue" {
		t.Fatalf("updated ticket type = %#v", updatedType)
	}
	deleted := callTool(t, session, "delete_column", map[string]any{"id": column.ID})
	if deleted.IsError || deleted.Content[0].(*sdk.TextContent).Text != "deleted" {
		t.Fatalf("delete empty column: %s", deleted.Content[0].(*sdk.TextContent).Text)
	}
	var ticket struct {
		ID string `json:"id"`
	}
	decodeText(t, callTool(t, session, "create_ticket", map[string]any{"board_id": board.ID, "title": "Keep", "status_id": original.ID}), &ticket)
	blocked := callTool(t, session, "delete_column", map[string]any{"id": original.ID})
	if !blocked.IsError || !strings.Contains(blocked.Content[0].(*sdk.TextContent).Text, "status has tickets") {
		t.Fatalf("delete non-empty column = %#v", blocked)
	}
}

func TestMCPSkillTools(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, skill := range []struct {
		id, name, description, body string
		enabled                     int
	}{
		{"skill-enabled", "pirate-voice", "Use when the user asks for a pirate greeting.", "Always answer in pirate speak and end with 'Arrr!'", 1},
		{"skill-disabled", "hidden-skill", "Disabled skill description.", "Hidden body", 0},
	} {
		if _, err := database.Exec(`INSERT INTO skill(id,name,description,body,enabled,created_at,updated_at) VALUES(?,?,?,?,?,'now','now')`, skill.id, skill.name, skill.description, skill.body, skill.enabled); err != nil {
			t.Fatal(err)
		}
	}
	server, err := NewServer(api.New(database, "ai", true, "dev", t.TempDir()), "dev", database, false)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	}()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var loadDescription string
	for _, tool := range tools.Tools {
		if tool.Name == "load_skill" {
			loadDescription = tool.Description
		}
	}
	wantDescription := "Load a skill's instructions or a memory's details by name. Enabled skills and memories are listed in the chat instructions."
	if loadDescription != wantDescription {
		t.Fatalf("load_skill description = %q, want %q", loadDescription, wantDescription)
	}
	listed := callTool(t, session, "list_skills", map[string]any{})
	if listed.IsError || len(listed.Content) != 1 || listed.Content[0].(*sdk.TextContent).Text != `[{"name":"pirate-voice","description":"Use when the user asks for a pirate greeting."}]` {
		t.Fatalf("list_skills result = %#v", listed)
	}
	loaded := callTool(t, session, "load_skill", map[string]any{"name": "pirate-voice"})
	if loaded.IsError || len(loaded.Content) != 1 || loaded.Content[0].(*sdk.TextContent).Text != "# pirate-voice\n\nAlways answer in pirate speak and end with 'Arrr!'" {
		t.Fatalf("load_skill result = %#v", loaded)
	}
	for _, name := range []string{"hidden-skill", "missing-skill"} {
		result := callTool(t, session, "load_skill", map[string]any{"name": name})
		if !result.IsError || len(result.Content) != 1 || result.Content[0].(*sdk.TextContent).Text != "skill not found: "+name {
			t.Fatalf("load_skill(%q) = %#v, want tool error", name, result)
		}
	}
	created := callTool(t, session, "create_skill", map[string]any{"name": "repeatable", "description": "Repeat this task", "body": "Steps"})
	if created.IsError {
		t.Fatalf("create_skill: %#v", created)
	}
	duplicate := callTool(t, session, "create_skill", map[string]any{"name": "repeatable", "description": "Again", "body": "Steps"})
	if !duplicate.IsError || !strings.Contains(duplicate.Content[0].(*sdk.TextContent).Text, "update_skill") {
		t.Fatalf("duplicate create result = %#v", duplicate)
	}
	updated := callTool(t, session, "update_skill", map[string]any{"name": "repeatable", "body": "Updated steps"})
	var skill struct {
		Body        string `json:"body"`
		Description string `json:"description"`
	}
	decodeText(t, updated, &skill)
	if skill.Body != "Updated steps" || skill.Description != "Repeat this task" {
		t.Fatalf("updated skill = %#v", skill)
	}
	var skillChange, skillLink, skillID string
	if err := database.QueryRow(`SELECT id FROM skill WHERE name='repeatable'`).Scan(&skillID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT title,link FROM notification WHERE kind='change' ORDER BY created_at DESC,rowid DESC LIMIT 1`).Scan(&skillChange, &skillLink); err != nil || skillChange != "Martin updated skill repeatable" || skillLink != "/ai/skills/"+skillID {
		t.Fatalf("skill change notification = %q %q, %v", skillChange, skillLink, err)
	}
	deleted := callTool(t, session, "delete_skill", map[string]any{"name": "repeatable"})
	if deleted.IsError || deleted.Content[0].(*sdk.TextContent).Text != "deleted" {
		t.Fatalf("delete_skill = %#v", deleted)
	}
	missing := callTool(t, session, "delete_skill", map[string]any{"name": "repeatable"})
	if !missing.IsError || missing.Content[0].(*sdk.TextContent).Text != "skill not found: repeatable" {
		t.Fatalf("unknown delete = %#v", missing)
	}
}

func TestMCPInstructions(t *testing.T) {
	for _, inApp := range []bool{false, true} {
		t.Run(fmt.Sprintf("in-app-%t", inApp), func(t *testing.T) {
			server, err := NewServer(nil, "dev", nil, inApp)
			if err != nil {
				t.Fatal(err)
			}
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
			got := session.InitializeResult().Instructions
			if !strings.Contains(got, "call create_skill") || !strings.Contains(got, "Your changes are labeled AI") {
				t.Fatalf("instructions missing hints: %q", got)
			}
			if strings.Contains(got, "You are Martin") == inApp || strings.Contains(got, "/tickets/<ticket id>") == inApp {
				t.Fatalf("instructions persona mismatch: %q", got)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	}()
	server, err := NewServer(api.New(database, "ai", true, "dev", filepath.Dir(dbPath)), "dev", database, false)
	if err != nil {
		t.Fatal(err)
	}
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
