package api

import (
	"net/http"
	"testing"
)

func TestCalendarShowsTicketDueDates(t *testing.T) {
	_, handler, _ := chatAPI(t)
	board := createBoard(t, handler, "DUE", nil)
	var noDue, due ticket
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+board.ID+"/tickets", map[string]any{"title": "no due"}, &noDue); code != http.StatusCreated || noDue.DueDate != nil {
		t.Fatalf("create without due date = %d, %#v", code, noDue.DueDate)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+board.ID+"/tickets", map[string]any{"title": "due", "due_date": "2026-10-20"}, &due); code != http.StatusCreated {
		t.Fatalf("create with due date = %d", code)
	}
	var cal struct {
		Tickets []ticket `json:"tickets"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/calendar?from=2026-10-01&to=2026-10-31", nil, &cal); code != http.StatusOK || len(cal.Tickets) != 1 || cal.Tickets[0].ID != due.ID {
		t.Fatalf("calendar = %d, %#v", code, cal.Tickets)
	}
}
