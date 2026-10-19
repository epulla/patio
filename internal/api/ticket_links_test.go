package api

import (
	"net/http"
	"testing"
)

func TestTicketLinkCreateInverseDuplicateSelfAndHistory(t *testing.T) {
	db, h := newAPI(t)
	b := createBoard(t, h, "LNK", nil)
	a := createTicket(t, h, b.ID, "")
	z := createTicket(t, h, b.ID, "")
	var created map[string]string
	if code := requestJSON(t, h, http.MethodPost, "/api/tickets/"+a.ID+"/links", map[string]string{"to_ticket_id": z.ID, "type": "blocks"}, &created); code != http.StatusCreated {
		t.Fatalf("create status %d", code)
	}
	var links []ticketLink
	if code := requestJSON(t, h, http.MethodGet, "/api/tickets/"+z.ID+"/links", nil, &links); code != 200 || len(links) != 1 || links[0].Label != "is blocked by" {
		t.Fatalf("inverse links status=%d links=%+v", code, links)
	}
	if code := requestJSON(t, h, http.MethodPost, "/api/tickets/"+a.ID+"/links", map[string]string{"to_ticket_id": z.ID, "type": "blocks"}, nil); code != http.StatusConflict {
		t.Fatalf("duplicate status %d", code)
	}
	if code := requestJSON(t, h, http.MethodPost, "/api/tickets/"+a.ID+"/links", map[string]string{"to_ticket_id": a.ID, "type": "blocks"}, nil); code != http.StatusBadRequest {
		t.Fatalf("self status %d", code)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM ticket_history WHERE ticket_id IN (?,?) AND content LIKE 'Linked:%'", a.ID, z.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("history count=%d err=%v", count, err)
	}
}

func TestTicketLinkDeleteAuditsBothAndTicketDeleteCascades(t *testing.T) {
	db, h := newAPI(t)
	b := createBoard(t, h, "LNK", nil)
	a := createTicket(t, h, b.ID, "")
	z := createTicket(t, h, b.ID, "")
	var created map[string]string
	requestJSON(t, h, http.MethodPost, "/api/tickets/"+a.ID+"/links", map[string]string{"code": z.Code, "type": "relates"}, &created)
	if code := requestJSON(t, h, http.MethodDelete, "/api/ticket-links/"+created["id"], nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete status %d", code)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM ticket_history WHERE ticket_id IN (?,?) AND content LIKE 'Unlinked:%'", a.ID, z.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("unlink history=%d err=%v", count, err)
	}
	requestJSON(t, h, http.MethodPost, "/api/tickets/"+a.ID+"/links", map[string]string{"to_ticket_id": z.ID, "type": "duplicates"}, &created)
	if _, err := db.Exec("DELETE FROM ticket WHERE id=?", a.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM ticket_link").Scan(&count); err != nil || count != 0 {
		t.Fatalf("remaining links=%d err=%v", count, err)
	}
}
