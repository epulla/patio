package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func transferRequest(t *testing.T, h http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func exportJSON(t *testing.T, h http.Handler, boardID string) []byte {
	t.Helper()
	w := transferRequest(t, h, http.MethodGet, "/api/boards/"+boardID+"/export?format=json", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export status = %d: %s", w.Code, w.Body)
	}
	return w.Body.Bytes()
}

func TestBoardJSONRoundTripAndCollision(t *testing.T) {
	database, h := newAPI(t)
	b := createBoard(t, h, "IMP", []string{"Todo", "Done"})
	statuses := boardStatuses(t, h, b.ID)
	first := createTicket(t, h, b.ID, statuses[0].ID)
	createTicket(t, h, b.ID, statuses[1].ID)
	backup := exportJSON(t, h, b.ID)
	md := transferRequest(t, h, http.MethodGet, "/api/tickets/"+first.ID+"/export?format=md", nil)
	if md.Code != http.StatusOK || !strings.Contains(md.Body.String(), "Created in Todo (Local)") {
		t.Fatalf("markdown status transition = %q", md.Body.String())
	}
	other, otherHandler := newAPI(t)
	if _, err := other.Exec(`INSERT INTO board(id,name,short_name,created_at,updated_at) VALUES('existing','Existing','OLD','2000','2000')`); err != nil {
		t.Fatal(err)
	}
	w := transferRequest(t, otherHandler, http.MethodPost, "/api/import", backup)
	if w.Code != http.StatusCreated {
		t.Fatalf("import status = %d: %s", w.Code, w.Body)
	}
	var imported testBoard
	if err := json.NewDecoder(w.Body).Decode(&imported); err != nil {
		t.Fatal(err)
	}
	var listed []testBoard
	if code := requestJSON(t, otherHandler, http.MethodGet, "/api/boards", nil, &listed); code != http.StatusOK {
		t.Fatalf("GET /api/boards status = %d", code)
	}
	if len(listed) != 2 || listed[0].ID != imported.ID || listed[1].ID != "existing" {
		t.Fatalf("imported board order = %#v", listed)
	}
	for _, table := range []string{"status", "ticket_type", "ticket", "ticket_history", "ticket_status_transition", "ticket_comment"} {
		var count int
		if err := other.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if table == "status" && count != 2 || table == "ticket" && count != 2 || table == "ticket_history" && count != 2 || table == "ticket_status_transition" && count != 2 {
			t.Errorf("%s count = %d", table, count)
		}
	}
	var data testData
	if code := requestJSON(t, otherHandler, http.MethodGet, "/api/boards/"+imported.ID+"/data", nil, &data); code != http.StatusOK {
		t.Fatal(code)
	}
	if len(data.Statuses) != 2 || len(data.Statuses[0].Tickets) != 1 || data.Statuses[0].Tickets[0].Code != "IMP-1" {
		t.Fatalf("imported order = %#v", data)
	}
	collision := transferRequest(t, h, http.MethodPost, "/api/import", backup)
	if collision.Code != http.StatusCreated {
		t.Fatalf("collision import status = %d", collision.Code)
	}
	var collided testBoard
	if err := json.NewDecoder(collision.Body).Decode(&collided); err != nil {
		t.Fatal(err)
	}
	if collided.ShortName != "IMP2" {
		t.Fatalf("collision short name = %q", collided.ShortName)
	}
	var code string
	if err := database.QueryRow("SELECT code FROM ticket WHERE board_id=? ORDER BY number LIMIT 1", collided.ID).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != "IMP2-1" {
		t.Fatalf("collision code = %q", code)
	}
}

func TestBoardJSONImportDefaultsHideAfterDays(t *testing.T) {
	_, sourceHandler := newAPI(t)
	board := createBoard(t, sourceHandler, "DAYS", []string{"Temporary"})
	status := boardStatuses(t, sourceHandler, board.ID)[0]
	if code := requestJSON(t, sourceHandler, http.MethodPut, "/api/statuses/"+status.ID, map[string]any{"name": "Temporary", "type": "temporary", "hide_after_days": 3}, nil); code != http.StatusOK {
		t.Fatalf("update temporary status = %d", code)
	}
	var backup map[string]json.RawMessage
	if err := json.Unmarshal(exportJSON(t, sourceHandler, board.ID), &backup); err != nil {
		t.Fatal(err)
	}
	var statuses []map[string]json.RawMessage
	if err := json.Unmarshal(backup["statuses"], &statuses); err != nil {
		t.Fatal(err)
	}
	var days int
	if err := json.Unmarshal(statuses[0]["hide_after_days"], &days); err != nil || days != 3 {
		t.Fatalf("export hide_after_days = %d, err = %v", days, err)
	}
	delete(statuses[0], "hide_after_days")
	statusJSON, err := json.Marshal(statuses)
	if err != nil {
		t.Fatal(err)
	}
	backup["statuses"] = statusJSON
	legacy, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	_, destinationHandler := newAPI(t)
	w := transferRequest(t, destinationHandler, http.MethodPost, "/api/import", legacy)
	if w.Code != http.StatusCreated {
		t.Fatalf("legacy import status = %d: %s", w.Code, w.Body)
	}
	var imported testBoard
	if err := json.NewDecoder(w.Body).Decode(&imported); err != nil {
		t.Fatal(err)
	}
	var data testData
	if code := requestJSON(t, destinationHandler, http.MethodGet, "/api/boards/"+imported.ID+"/data", nil, &data); code != http.StatusOK {
		t.Fatalf("imported board data status = %d", code)
	}
	if len(data.Statuses) != 1 || data.Statuses[0].HideAfterDays != 1 {
		t.Fatalf("imported hide_after_days = %#v, want 1", data.Statuses)
	}
	statuses[0]["hide_after_days"] = json.RawMessage("0")
	statusJSON, err = json.Marshal(statuses)
	if err != nil {
		t.Fatal(err)
	}
	backup["statuses"] = statusJSON
	invalid, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	if w := transferRequest(t, destinationHandler, http.MethodPost, "/api/import", invalid); w.Code != http.StatusBadRequest {
		t.Fatalf("import hide_after_days=0 status = %d", w.Code)
	}
}

func TestBoardJSONCalendarItemRoundTripAndLegacyImport(t *testing.T) {
	source, sourceHandler := newAPI(t)
	board := createBoard(t, sourceHandler, "CALIMP", []string{"Todo"})
	ticket := createTicket(t, sourceHandler, board.ID, boardStatuses(t, sourceHandler, board.ID)[0].ID)
	var event, reminder testCalendarItem
	if code := requestJSON(t, sourceHandler, http.MethodPost, "/api/calendar-items", map[string]any{"kind": "event", "title": "Board event", "starts_at": "2026-04-12", "all_day": true, "board_id": board.ID}, &event); code != http.StatusCreated {
		t.Fatalf("create event status = %d", code)
	}
	if code := requestJSON(t, sourceHandler, http.MethodPost, "/api/calendar-items", map[string]any{"kind": "reminder", "title": "Ticket reminder", "starts_at": "2026-04-12T12:00:00Z", "ticket_id": ticket.ID}, &reminder); code != http.StatusCreated {
		t.Fatalf("create reminder status = %d", code)
	}
	const notifiedAt = "2026-04-12T12:00:00.000Z"
	if _, err := source.Exec(`UPDATE calendar_item SET notified_at=? WHERE id=?`, notifiedAt, reminder.ID); err != nil {
		t.Fatal(err)
	}
	backup := exportJSON(t, sourceHandler, board.ID)
	var exported boardBackup
	if err := json.Unmarshal(backup, &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.CalendarItems) != 2 {
		t.Fatalf("exported calendar items = %d", len(exported.CalendarItems))
	}
	var creatorIncluded bool
	for _, user := range exported.Users {
		if user.ID == "local" {
			creatorIncluded = true
		}
	}
	if !creatorIncluded {
		t.Fatal("calendar item creator missing from user references")
	}

	destination, destinationHandler := newAPI(t)
	w := transferRequest(t, destinationHandler, http.MethodPost, "/api/import", backup)
	if w.Code != http.StatusCreated {
		t.Fatalf("import status = %d: %s", w.Code, w.Body)
	}
	var imported testBoard
	if err := json.NewDecoder(w.Body).Decode(&imported); err != nil {
		t.Fatal(err)
	}
	var importedTicketID string
	if err := destination.QueryRow(`SELECT id FROM ticket WHERE board_id=?`, imported.ID).Scan(&importedTicketID); err != nil {
		t.Fatal(err)
	}
	rows, err := destination.Query(`SELECT id,board_id,ticket_id,notified_at FROM calendar_item WHERE board_id=? ORDER BY starts_at,rowid`, imported.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, boardID string
		var ticketID, importedNotifiedAt sql.NullString
		if err := rows.Scan(&id, &boardID, &ticketID, &importedNotifiedAt); err != nil {
			t.Fatal(err)
		}
		count++
		if boardID != imported.ID {
			t.Errorf("calendar item board_id = %q, want %q", boardID, imported.ID)
		}
		if id == event.ID || id == reminder.ID {
			t.Errorf("calendar item id was not remapped: %q", id)
		}
		if ticketID.Valid && ticketID.String != importedTicketID {
			t.Errorf("ticket_id = %q, want imported ticket %q", ticketID.String, importedTicketID)
		}
		if importedNotifiedAt.Valid && importedNotifiedAt.String != notifiedAt {
			t.Errorf("notified_at = %q, want %q", importedNotifiedAt.String, notifiedAt)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("imported calendar items = %d", count)
	}
	var linkedTicketID string
	if err := destination.QueryRow(`SELECT ticket_id FROM calendar_item WHERE board_id=? AND ticket_id IS NOT NULL`, imported.ID).Scan(&linkedTicketID); err != nil {
		t.Fatal(err)
	}
	if linkedTicketID != importedTicketID || linkedTicketID == ticket.ID {
		t.Fatalf("imported ticket link = %q; imported ticket = %q, source ticket = %q", linkedTicketID, importedTicketID, ticket.ID)
	}

	exported.CalendarItems = nil
	legacyBackup, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	_, legacyHandler := newAPI(t)
	legacy := transferRequest(t, legacyHandler, http.MethodPost, "/api/import", legacyBackup)
	if legacy.Code != http.StatusCreated {
		t.Fatalf("legacy import status = %d: %s", legacy.Code, legacy.Body)
	}
}

func TestMarkdownExportLabelsAIActor(t *testing.T) {
	database, h := newAPIAs(t, "ai")
	b := createBoard(t, h, "MARTIN", []string{"Todo", "Done"})
	statuses := boardStatuses(t, h, b.ID)
	ticket := createTicket(t, h, b.ID, statuses[0].ID)
	if code := requestJSON(t, h, http.MethodPut, "/api/tickets/"+ticket.ID+"/move", map[string]any{"status_id": statuses[1].ID, "index": 0}, nil); code != http.StatusOK {
		t.Fatalf("move status = %d", code)
	}
	w := transferRequest(t, h, http.MethodGet, "/api/tickets/"+ticket.ID+"/export?format=md", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Martin (AI)") {
		t.Fatalf("markdown export = %d: %s", w.Code, w.Body)
	}
	backup := exportJSON(t, h, b.ID)
	var exported boardBackup
	if err := json.Unmarshal(backup, &exported); err != nil {
		t.Fatal(err)
	}
	var foundAI bool
	for _, user := range exported.Users {
		if user.ID == "ai" && user.IsAI {
			foundAI = true
		}
	}
	if !foundAI {
		t.Fatalf("AI user missing from backup: %#v", exported.Users)
	}
	var storedAI bool
	if err := database.QueryRow(`SELECT is_ai FROM "user" WHERE id='ai'`).Scan(&storedAI); err != nil {
		t.Fatal(err)
	}
	if !storedAI {
		t.Fatal("AI user is not marked as AI")
	}
}

func TestTransferInvalidBackups(t *testing.T) {
	database, h := newAPI(t)
	b := createBoard(t, h, "BAD", []string{"Todo"})
	ticket := createTicket(t, h, b.ID, boardStatuses(t, h, b.ID)[0].ID)
	var item testCalendarItem
	if code := requestJSON(t, h, http.MethodPost, "/api/calendar-items", map[string]any{"kind": "reminder", "title": "Ticket reminder", "starts_at": "2026-04-12T12:00:00Z", "ticket_id": ticket.ID}, &item); code != http.StatusCreated {
		t.Fatalf("create calendar item status = %d", code)
	}
	backup := exportJSON(t, h, b.ID)
	var original boardBackup
	if err := json.Unmarshal(backup, &original); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*boardBackup)
	}{
		{"format", func(x *boardBackup) { x.Format = "other" }},
		{"dangling status", func(x *boardBackup) { x.Tickets[0].StatusID = "missing" }},
		{"dangling calendar ticket", func(x *boardBackup) { missing := "missing"; x.CalendarItems[0].TicketID = &missing }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := original
			candidate.Tickets = append([]transferTicket(nil), original.Tickets...)
			candidate.CalendarItems = append([]transferCalendarItem(nil), original.CalendarItems...)
			test.mutate(&candidate)
			body, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if w := transferRequest(t, h, http.MethodPost, "/api/import", body); w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d", w.Code)
			}
		})
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM calendar_item WHERE ticket_id=?`, ticket.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("source calendar item count = %d", count)
	}
}

func TestCSVExportHeaderAndFormulaSafety(t *testing.T) {
	_, h := newAPI(t)
	b := createBoard(t, h, "CSV", []string{"Todo"})
	status := boardStatuses(t, h, b.ID)[0]
	var ticket testTicket
	if code := requestJSON(t, h, http.MethodPost, "/api/boards/"+b.ID+"/tickets", map[string]any{"title": "=SUM(A1)", "status_id": status.ID}, &ticket); code != http.StatusCreated {
		t.Fatal(code)
	}
	w := transferRequest(t, h, http.MethodGet, "/api/boards/"+b.ID+"/export?format=csv", nil)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	data, err := io.ReadAll(w.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "\ufeffCode,Title,Status") || !strings.Contains(text, "'=SUM(A1)") {
		t.Fatalf("CSV = %q", text)
	}
}
