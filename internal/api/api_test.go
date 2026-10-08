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
	ID   string `json:"id"`
	Code string `json:"code"`
}
type testData struct {
	Statuses []struct {
		ID      string       `json:"id"`
		Tickets []testTicket `json:"tickets"`
	} `json:"statuses"`
}

func newAPI(t *testing.T) (*sql.DB, http.Handler) {
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
	return database, New(database, false)
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
	if err := database.QueryRow("SELECT content FROM ticket_history WHERE ticket_id=? ORDER BY created_at DESC LIMIT 1", second.ID).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if history != "Status changed from To Do to In Progress" {
		t.Fatalf("move history = %q", history)
	}
	assertOrder(t, database, statuses[0].ID, []string{first.ID})
	assertOrder(t, database, statuses[1].ID, []string{second.ID})
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

func TestDeleteStatusWithTicketsConflicts(t *testing.T) {
	_, handler := newAPI(t)
	b := createBoard(t, handler, "DEL", []string{"Todo"})
	statusID := boardStatuses(t, handler, b.ID)[0].ID
	createTicket(t, handler, b.ID, statusID)
	if code := requestJSON(t, handler, http.MethodDelete, "/api/statuses/"+statusID, nil, nil); code != http.StatusConflict {
		t.Fatalf("status = %d", code)
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
