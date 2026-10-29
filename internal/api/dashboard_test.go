package api

import (
	"net/http"
	"testing"
	"time"
)

func TestDashboardCountsDoneCancelledAndDueTickets(t *testing.T) {
	database, handler := newAPI(t)
	var board testBoard
	statuses := []any{"To Do", map[string]any{"name": "Done", "type": "temporary"}, map[string]any{"name": "Cancelled", "type": "temporary"}}
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards", map[string]any{"name": "Dash", "short_name": "DASH", "statuses": statuses}, &board); code != http.StatusCreated {
		t.Fatalf("create board = %d", code)
	}
	ids := boardStatuses(t, handler, board.ID)
	today := time.Now()
	ticketDue := func(due time.Time) testTicket {
		var out testTicket
		if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+board.ID+"/tickets", map[string]any{"title": "t", "due_date": due.Format("2006-01-02")}, &out); code != http.StatusCreated {
			t.Fatalf("create ticket = %d", code)
		}
		return out
	}
	done, cancelled := ticketDue(today.AddDate(0, 0, -3)), ticketDue(today.AddDate(0, 0, 2))
	overdue, soon := ticketDue(today.AddDate(0, 0, -1)), ticketDue(today.AddDate(0, 0, 3))
	ticketDue(today.AddDate(0, 0, 30))
	for id, status := range map[string]string{done.ID: ids[1].ID, cancelled.ID: ids[2].ID} {
		if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+id+"/move", map[string]any{"status_id": status, "index": 0}, nil); code != http.StatusOK {
			t.Fatalf("move = %d", code)
		}
	}
	var got struct {
		Days                     []dashboardDay `json:"days"`
		Created, Done, Cancelled int
		Boards                   []dashboardBoard `json:"boards"`
		Overdue                  []ticket         `json:"overdue"`
		DueSoon                  []ticket         `json:"due_soon"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/dashboard?days=7&board_id="+board.ID, nil, &got); code != http.StatusOK {
		t.Fatalf("dashboard = %d", code)
	}
	last := got.Days[len(got.Days)-1]
	if len(got.Days) != 7 || last.Date != today.Format("2006-01-02") || last.Done != 1 || last.Cancelled != 1 {
		t.Fatalf("days = %#v", got.Days)
	}
	if got.Created != 5 || got.Done != 1 || got.Cancelled != 1 || len(got.Boards) != 1 || got.Boards[0].Created != 5 || got.Boards[0].Done != 1 {
		t.Fatalf("totals = %d/%d/%d, boards %#v", got.Created, got.Done, got.Cancelled, got.Boards)
	}
	if len(got.Overdue) != 1 || got.Overdue[0].ID != overdue.ID || len(got.DueSoon) != 1 || got.DueSoon[0].ID != soon.ID {
		t.Fatalf("overdue %#v, due soon %#v", got.Overdue, got.DueSoon)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/dashboard?days=0", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("bad days = %d", code)
	}
	previousDate := time.Date(today.Year(), today.Month(), today.Day()-10, 12, 0, 0, 0, time.Local).UTC().Format("2006-01-02T15:04:05.000Z")
	if _, err := database.Exec(`UPDATE ticket SET created_at=? WHERE id=?`, previousDate, done.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE ticket_status_transition SET moved_at=? WHERE ticket_id=? AND to_status_id=?`, previousDate, done.ID, ids[1].ID); err != nil {
		t.Fatal(err)
	}
	var previous struct {
		Done    int `json:"done"`
		Created int `json:"created"`
	}
	var response struct {
		Previous struct {
			Done    int `json:"done"`
			Created int `json:"created"`
		} `json:"previous"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/dashboard?days=7&board_id="+board.ID, nil, &response); code != http.StatusOK {
		t.Fatalf("dashboard = %d", code)
	}
	previous = response.Previous
	if previous.Done != 1 || previous.Created != 1 {
		t.Fatalf("previous counts = %#v", previous)
	}
}

func TestStatusCountsAsClassification(t *testing.T) {
	if got := statusCountsAs("classic", "Doing", `{"counts_as":"done"}`); got != "done" {
		t.Fatalf("classic override = %q", got)
	}
	if got := statusCountsAs("temporary", "Done", `{"counts_as":"none"}`); got != "none" {
		t.Fatalf("temporary override = %q", got)
	}
	for _, name := range []string{"Cancelado", "Descartado"} {
		if got := statusCountsAs("temporary", name, `{}`); got != "cancelled" {
			t.Fatalf("%s classification = %q", name, got)
		}
	}
}
