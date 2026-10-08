package api

import (
	"bytes"
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
	w := transferRequest(t, otherHandler, http.MethodPost, "/api/import", backup)
	if w.Code != http.StatusCreated {
		t.Fatalf("import status = %d: %s", w.Code, w.Body)
	}
	var imported testBoard
	if err := json.NewDecoder(w.Body).Decode(&imported); err != nil {
		t.Fatal(err)
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

func TestTransferInvalidBackups(t *testing.T) {
	_, h := newAPI(t)
	b := createBoard(t, h, "BAD", []string{"Todo"})
	createTicket(t, h, b.ID, boardStatuses(t, h, b.ID)[0].ID)
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := original
			candidate.Tickets = append([]transferTicket(nil), original.Tickets...)
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
