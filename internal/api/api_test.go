package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"my-tickets/internal/db"
)

type testBoard struct {
	ID        string `json:"id"`
	ShortName string `json:"short_name"`
}
type testStatus struct {
	ID string `json:"id"`
}
type testTicket struct {
	ID         string  `json:"id"`
	Code       string  `json:"code"`
	Title      string  `json:"title"`
	ReporterID *string `json:"reporter_id"`
}
type testCalendarItem struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	Title         string  `json:"title"`
	StartsAt      string  `json:"starts_at"`
	BoardID       *string `json:"board_id"`
	TicketID      *string `json:"ticket_id"`
	CreatedBy     string  `json:"created_by"`
	CreatedByIsAI bool    `json:"created_by_is_ai"`
}
type testCalendarResponse struct {
	Items   []testCalendarItem `json:"items"`
	Tickets []testTicket       `json:"tickets"`
}
type testData struct {
	Statuses []struct {
		ID      string       `json:"id"`
		Tickets []testTicket `json:"tickets"`
	} `json:"statuses"`
}

func newAPI(t *testing.T) (*sql.DB, http.Handler) {
	return newAPIAs(t, "local")
}

func newAPIAs(t *testing.T, userID string) (*sql.DB, http.Handler) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	return database, New(database, userID, false)
}

func requestJSON(t *testing.T, handler http.Handler, method, path string, body any, out any) int {
	t.Helper()
	var input *bytes.Reader
	if body == nil {
		input = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		input = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, path, input)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if out != nil && res.Body.Len() > 0 {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return res.Code
}

func createBoard(t *testing.T, handler http.Handler, short string, statuses []string) testBoard {
	t.Helper()
	var out testBoard
	body := map[string]any{"name": short, "short_name": short}
	if statuses != nil {
		body["statuses"] = statuses
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards", body, &out); code != http.StatusCreated {
		t.Fatalf("create board status = %d", code)
	}
	return out
}

func boardStatuses(t *testing.T, handler http.Handler, boardID string) []testStatus {
	t.Helper()
	var data testData
	if code := requestJSON(t, handler, http.MethodGet, "/api/boards/"+boardID+"/data", nil, &data); code != http.StatusOK {
		t.Fatalf("board data status = %d", code)
	}
	out := make([]testStatus, len(data.Statuses))
	for i := range data.Statuses {
		out[i].ID = data.Statuses[i].ID
	}
	return out
}

func createTicket(t *testing.T, handler http.Handler, boardID string, statusID string) testTicket {
	t.Helper()
	var out testTicket
	body := map[string]any{"title": "Ticket"}
	if statusID != "" {
		body["status_id"] = statusID
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+boardID+"/tickets", body, &out); code != http.StatusCreated {
		t.Fatalf("create ticket status = %d", code)
	}
	return out
}

func TestTicketNumberingIndependentPerBoard(t *testing.T) {
	database, handler := newAPI(t)
	a := createBoard(t, handler, "AAA", nil)
	b := createBoard(t, handler, "BBB", nil)
	one := createTicket(t, handler, a.ID, "")
	two := createTicket(t, handler, a.ID, "")
	other := createTicket(t, handler, b.ID, "")
	if one.Code != "AAA-1" || two.Code != "AAA-2" || other.Code != "BBB-1" {
		t.Fatalf("codes = %q, %q, %q", one.Code, two.Code, other.Code)
	}
	var aLast, bLast int
	if err := database.QueryRow("SELECT last_number FROM board_counter WHERE board_id=?", a.ID).Scan(&aLast); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT last_number FROM board_counter WHERE board_id=?", b.ID).Scan(&bLast); err != nil {
		t.Fatal(err)
	}
	if aLast != 2 || bLast != 1 {
		t.Fatalf("counters = %d, %d", aLast, bLast)
	}
}

func TestMoveAcrossColumnsAuditsAndOrders(t *testing.T) {
	database, handler := newAPI(t)
	b := createBoard(t, handler, "MOVE", []string{"To Do", "In Progress"})
	statuses := boardStatuses(t, handler, b.ID)
	first := createTicket(t, handler, b.ID, statuses[0].ID)
	second := createTicket(t, handler, b.ID, statuses[0].ID)
	var beforeHistory int
	if err := database.QueryRow("SELECT COUNT(*) FROM ticket_history WHERE ticket_id=?", second.ID).Scan(&beforeHistory); err != nil {
		t.Fatal(err)
	}
	var moved testTicket
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+second.ID+"/move", map[string]any{"status_id": statuses[1].ID, "index": 0}, &moved); code != http.StatusOK {
		t.Fatalf("move status = %d", code)
	}
	var transitions, histories int
	if err := database.QueryRow("SELECT COUNT(*) FROM ticket_status_transition WHERE ticket_id=? AND from_status_id IS NOT NULL", second.ID).Scan(&transitions); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM ticket_history WHERE ticket_id=?", second.ID).Scan(&histories); err != nil {
		t.Fatal(err)
	}
	if transitions != 1 || histories-beforeHistory != 1 {
		t.Fatalf("move audit counts = transition %d, history delta %d", transitions, histories-beforeHistory)
	}
	var history string
	if err := database.QueryRow("SELECT content FROM ticket_history WHERE ticket_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1", second.ID).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if history != "Status changed from To Do to In Progress" {
		t.Fatalf("move history = %q", history)
	}
	assertOrder(t, database, statuses[0].ID, []string{first.ID})
	assertOrder(t, database, statuses[1].ID, []string{second.ID})
}

func TestActingUserIsConfigurable(t *testing.T) {
	database, handler := newAPIAs(t, "ai")
	b := createBoard(t, handler, "AI", []string{"Todo", "Done"})
	statuses := boardStatuses(t, handler, b.ID)
	ticket := createTicket(t, handler, b.ID, statuses[0].ID)
	if ticket.ReporterID == nil || *ticket.ReporterID != "ai" {
		t.Fatalf("reporter = %#v", ticket.ReporterID)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+ticket.ID+"/move", map[string]any{"status_id": statuses[1].ID, "index": 0}, nil); code != http.StatusOK {
		t.Fatalf("move status = %d", code)
	}
	var users []string
	rows, err := database.Query("SELECT user_id FROM ticket_status_transition WHERE ticket_id=? ORDER BY rowid", ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			t.Fatal(err)
		}
		users = append(users, userID)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(users, []string{"ai", "ai"}) {
		t.Fatalf("transition users = %#v", users)
	}
}

func TestAIPersonaInTicketAuditResponses(t *testing.T) {
	_, handler := newAPIAs(t, "ai")
	b := createBoard(t, handler, "PERSONA", []string{"Todo", "Done"})
	statuses := boardStatuses(t, handler, b.ID)
	ticket := createTicket(t, handler, b.ID, statuses[0].ID)
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+ticket.ID+"/move", map[string]any{"status_id": statuses[1].ID, "index": 0}, nil); code != http.StatusOK {
		t.Fatalf("move status = %d", code)
	}
	var added struct {
		UserName string `json:"user_name"`
		UserIsAI bool   `json:"user_is_ai"`
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/tickets/"+ticket.ID+"/comments", map[string]string{"content": "AI comment"}, &added); code != http.StatusCreated {
		t.Fatalf("add comment status = %d", code)
	}
	if added.UserName != "Martin" || !added.UserIsAI {
		t.Fatalf("added comment actor = %#v", added)
	}
	for _, endpoint := range []string{"history", "transitions", "comments"} {
		t.Run(endpoint, func(t *testing.T) {
			var actors []struct {
				UserName string `json:"user_name"`
				UserIsAI bool   `json:"user_is_ai"`
			}
			if code := requestJSON(t, handler, http.MethodGet, "/api/tickets/"+ticket.ID+"/"+endpoint, nil, &actors); code != http.StatusOK {
				t.Fatalf("%s status = %d", endpoint, code)
			}
			if len(actors) == 0 {
				t.Fatalf("%s returned no audit entries", endpoint)
			}
			for _, actor := range actors {
				if actor.UserName != "Martin" || !actor.UserIsAI {
					t.Errorf("%s actor = %#v", endpoint, actor)
				}
			}
		})
	}
	var users []struct {
		Name string `json:"name"`
		IsAI bool   `json:"is_ai"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/users", nil, &users); code != http.StatusOK {
		t.Fatalf("users status = %d", code)
	}
	foundMartin := false
	for _, user := range users {
		if user.Name == "Martin" && user.IsAI {
			foundMartin = true
		}
	}
	if !foundMartin {
		t.Fatalf("Martin AI user missing: %#v", users)
	}
}

func TestSearchTickets(t *testing.T) {
	_, handler := newAPI(t)
	a := createBoard(t, handler, "SEA", []string{"Todo"})
	b := createBoard(t, handler, "OTH", []string{"Todo"})
	var first, second testTicket
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+a.ID+"/tickets", map[string]any{"title": "Partial Match"}, &first); code != http.StatusCreated {
		t.Fatalf("first ticket status = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+b.ID+"/tickets", map[string]any{"title": "Other"}, &second); code != http.StatusCreated {
		t.Fatalf("second ticket status = %d", code)
	}
	for _, test := range []struct {
		path string
		want int
	}{{"/api/tickets/search?q=partial", 1}, {"/api/tickets/search?q=" + first.Code, 1}, {"/api/tickets/search?q=match&board_id=" + b.ID, 0}} {
		var got []testTicket
		if code := requestJSON(t, handler, http.MethodGet, test.path, nil, &got); code != http.StatusOK || len(got) != test.want {
			t.Fatalf("search %q = status %d, %#v", test.path, code, got)
		}
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/tickets/search?q=%20", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("empty search status = %d", code)
	}
}

func TestSameColumnMoveOnlyReorders(t *testing.T) {
	database, handler := newAPI(t)
	b := createBoard(t, handler, "SAME", []string{"Todo"})
	statusID := boardStatuses(t, handler, b.ID)[0].ID
	first := createTicket(t, handler, b.ID, statusID)
	second := createTicket(t, handler, b.ID, statusID)
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+first.ID+"/move", map[string]any{"status_id": statusID, "index": 1}, nil); code != http.StatusOK {
		t.Fatalf("move status = %d", code)
	}
	var transitions int
	if err := database.QueryRow("SELECT COUNT(*) FROM ticket_status_transition WHERE ticket_id=?", first.ID).Scan(&transitions); err != nil {
		t.Fatal(err)
	}
	if transitions != 1 {
		t.Fatalf("same-column transitions = %d, want creation only", transitions)
	}
	assertOrder(t, database, statusID, []string{second.ID, first.ID})
}

func TestStatusOrderRejectsForeignOrMissingIDs(t *testing.T) {
	_, handler := newAPI(t)
	a := createBoard(t, handler, "ORD", []string{"Todo"})
	b := createBoard(t, handler, "EXT", []string{"Todo"})
	foreign := boardStatuses(t, handler, b.ID)[0].ID
	tests := []struct {
		name string
		ids  []string
	}{{"foreign", []string{foreign}}, {"missing", []string{"missing"}}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if code := requestJSON(t, handler, http.MethodPut, "/api/boards/"+a.ID+"/status-order", map[string]any{"status_ids": test.ids}, nil); code != http.StatusBadRequest {
				t.Fatalf("status = %d", code)
			}
		})
	}
}

func TestShortNameChangeKeepsOldCodeLookup(t *testing.T) {
	_, handler := newAPI(t)
	b := createBoard(t, handler, "OLD", []string{"Todo"})
	old := createTicket(t, handler, b.ID, "")
	if code := requestJSON(t, handler, http.MethodPut, "/api/boards/"+b.ID, map[string]any{"name": "Renamed", "short_name": "NEW", "template": ""}, nil); code != http.StatusOK {
		t.Fatalf("update board status = %d", code)
	}
	var got testTicket
	if code := requestJSON(t, handler, http.MethodGet, "/api/tickets/"+old.Code, nil, &got); code != http.StatusOK {
		t.Fatalf("old code status = %d", code)
	}
	if got.ID != old.ID || got.Code != "NEW-1" {
		t.Fatalf("lookup = %#v", got)
	}
}

func TestInvalidShortNameReturnsBadRequest(t *testing.T) {
	_, handler := newAPI(t)
	for _, short := range []string{"", "bad-name", "12345678901"} {
		t.Run(fmt.Sprintf("%q", short), func(t *testing.T) {
			if code := requestJSON(t, handler, http.MethodPost, "/api/boards", map[string]any{"name": "Bad", "short_name": short}, nil); code != http.StatusBadRequest {
				t.Fatalf("status = %d", code)
			}
		})
	}
}

func TestCalendarRangeBoardFilterAndSoftDelete(t *testing.T) {
	database, handler := newAPI(t)
	a := createBoard(t, handler, "CAL", []string{"Todo"})
	b := createBoard(t, handler, "OTH", []string{"Todo"})
	ticketA := createTicket(t, handler, a.ID, "")
	ticketB := createTicket(t, handler, b.ID, "")
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+ticketA.ID, map[string]any{"due_date": "2026-04-12"}, nil); code != http.StatusOK {
		t.Fatalf("set due date status = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+ticketB.ID, map[string]any{"due_date": "2026-04-13"}, nil); code != http.StatusOK {
		t.Fatalf("set other due date status = %d", code)
	}
	var within, outside, linked, boardItem testCalendarItem
	for _, item := range []struct {
		out  *testCalendarItem
		body map[string]any
	}{{&within, map[string]any{"kind": "event", "title": "In range", "starts_at": "2026-04-12", "all_day": true, "board_id": a.ID}}, {&outside, map[string]any{"kind": "event", "title": "Outside", "starts_at": "2026-04-14", "all_day": true, "board_id": a.ID}}, {&linked, map[string]any{"kind": "reminder", "title": "Ticket link", "starts_at": "2026-04-12T12:00:00-05:00", "ticket_id": ticketA.ID}}, {&boardItem, map[string]any{"kind": "event", "title": "Board item", "starts_at": "2026-04-10", "ends_at": "2026-04-12", "all_day": true, "board_id": a.ID}}} {
		if code := requestJSON(t, handler, http.MethodPost, "/api/calendar-items", item.body, item.out); code != http.StatusCreated {
			t.Fatalf("create calendar item status = %d", code)
		}
	}
	if linked.BoardID == nil || *linked.BoardID != a.ID || linked.CreatedBy != "local" || linked.CreatedByIsAI {
		t.Fatalf("linked item actor/board = %#v", linked)
	}
	if _, err := database.Exec(`UPDATE calendar_item SET board_id=NULL WHERE id=?`, linked.ID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/calendar-items/"+within.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete calendar item status = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/calendar-items/"+within.ID, nil, nil); code != http.StatusNotFound {
		t.Fatalf("delete already deleted status = %d", code)
	}
	for _, test := range []struct {
		path        string
		itemCount   int
		ticketCount int
	}{{"/api/calendar?from=2026-04-12&to=2026-04-12", 2, 1}, {"/api/calendar?from=2026-04-12&to=2026-04-12&board_id=" + a.ID, 2, 1}, {"/api/calendar?from=2026-04-12&to=2026-04-12&board_id=" + b.ID, 0, 0}} {
		var got testCalendarResponse
		if code := requestJSON(t, handler, http.MethodGet, test.path, nil, &got); code != http.StatusOK {
			t.Fatalf("calendar %q status = %d", test.path, code)
		}
		if len(got.Items) != test.itemCount || len(got.Tickets) != test.ticketCount {
			t.Errorf("calendar %q counts = %d items, %d tickets; want %d, %d", test.path, len(got.Items), len(got.Tickets), test.itemCount, test.ticketCount)
		}
		if test.path == "/api/calendar?from=2026-04-12&to=2026-04-12&board_id="+a.ID && !containsCalendarItem(got.Items, linked.ID) {
			t.Errorf("board filter missed ticket-linked item: %#v", got.Items)
		}
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/boards/"+a.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete board status = %d", code)
	}
	var got testCalendarResponse
	requestJSON(t, handler, http.MethodGet, "/api/calendar?from=2026-04-12&to=2026-04-12", nil, &got)
	if len(got.Items) != 1 || got.Items[0].ID != linked.ID || len(got.Tickets) != 0 {
		t.Fatalf("after board delete = %#v", got)
	}
}

func containsCalendarItem(items []testCalendarItem, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func TestCalendarItemPartialUpdateAndAIActor(t *testing.T) {
	_, handler := newAPIAs(t, "ai")
	var created testCalendarItem
	body := map[string]any{"kind": "event", "title": "Before", "starts_at": "2026-04-12", "all_day": true}
	if code := requestJSON(t, handler, http.MethodPost, "/api/calendar-items", body, &created); code != http.StatusCreated {
		t.Fatalf("create status = %d", code)
	}
	if created.CreatedBy != "ai" || !created.CreatedByIsAI {
		t.Fatalf("created actor = %#v", created)
	}
	var updated testCalendarItem
	if code := requestJSON(t, handler, http.MethodPut, "/api/calendar-items/"+created.ID, map[string]any{"title": "After"}, &updated); code != http.StatusOK {
		t.Fatalf("update status = %d", code)
	}
	if updated.Title != "After" || updated.Kind != "event" || updated.StartsAt != "2026-04-12" {
		t.Fatalf("partial update = %#v", updated)
	}
}

func TestCalendarItemValidation(t *testing.T) {
	_, handler := newAPI(t)
	for _, test := range []struct {
		name string
		body map[string]any
	}{{"bad kind", map[string]any{"kind": "other", "title": "Event", "starts_at": "2026-04-12", "all_day": true}}, {"empty title", map[string]any{"kind": "event", "title": "  ", "starts_at": "2026-04-12", "all_day": true}}, {"all-day timestamp", map[string]any{"kind": "event", "title": "Event", "starts_at": "2026-04-12T10:00:00Z", "all_day": true}}, {"timed date-only", map[string]any{"kind": "event", "title": "Event", "starts_at": "2026-04-12"}}, {"ends before starts", map[string]any{"kind": "event", "title": "Event", "starts_at": "2026-04-12T10:00:00Z", "ends_at": "2026-04-12T09:00:00Z"}}} {
		t.Run(test.name, func(t *testing.T) {
			if code := requestJSON(t, handler, http.MethodPost, "/api/calendar-items", test.body, nil); code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", code)
			}
		})
	}
}

func TestDueDateValidationOnTicketCreateAndUpdate(t *testing.T) {
	_, handler := newAPI(t)
	b := createBoard(t, handler, "DUE", []string{"Todo"})
	for _, dueDate := range []string{"2026-02-30", "2026-02-01T00:00:00Z"} {
		if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+b.ID+"/tickets", map[string]any{"title": "Bad due", "due_date": dueDate}, nil); code != http.StatusBadRequest {
			t.Errorf("create due_date %q status = %d", dueDate, code)
		}
	}
	ticket := createTicket(t, handler, b.ID, "")
	for _, dueDate := range []string{"2026-02-30", "2026-02-01T00:00:00Z"} {
		if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+ticket.ID, map[string]any{"due_date": dueDate}, nil); code != http.StatusBadRequest {
			t.Errorf("update due_date %q status = %d", dueDate, code)
		}
	}
}

func TestDeleteStatusWithTicketsConflicts(t *testing.T) {
	_, handler := newAPI(t)
	b := createBoard(t, handler, "DEL", []string{"Todo"})
	statusID := boardStatuses(t, handler, b.ID)[0].ID
	createTicket(t, handler, b.ID, statusID)
	if code := requestJSON(t, handler, http.MethodDelete, "/api/statuses/"+statusID, nil, nil); code != http.StatusConflict {
		t.Fatalf("status = %d", code)
	}
}

func TestBoardDataRejectsCorruptTicketOrder(t *testing.T) {
	database, handler := newAPI(t)
	b := createBoard(t, handler, "CORRUPT", []string{"Todo"})
	statusID := boardStatuses(t, handler, b.ID)[0].ID
	if _, err := database.Exec("UPDATE ticket_order SET ticket_ids=? WHERE status_id=?", "not json", statusID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/boards/"+b.ID+"/data", nil, nil); code != http.StatusInternalServerError {
		t.Fatalf("status = %d", code)
	}
}

func TestBoardDataRejectsCorruptTemporaryStatusTimestamp(t *testing.T) {
	database, handler := newAPI(t)
	b := createBoard(t, handler, "TEMP", []string{"Todo"})
	var status testStatus
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+b.ID+"/statuses", map[string]any{"name": "Temporary", "type": "temporary"}, &status); code != http.StatusCreated {
		t.Fatalf("create temporary status = %d", code)
	}
	ticket := createTicket(t, handler, b.ID, status.ID)
	if _, err := database.Exec("UPDATE ticket SET status_updated_at=? WHERE id=?", "not a timestamp", ticket.ID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/boards/"+b.ID+"/data", nil, nil); code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", code)
	}
}

func assertOrder(t *testing.T, database *sql.DB, statusID string, want []string) {
	t.Helper()
	var raw string
	if err := database.QueryRow("SELECT ticket_ids FROM ticket_order WHERE status_id=?", statusID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %#v, want %#v", got, want)
	}
}
