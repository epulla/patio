package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

type ticketLink struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Direction     string `json:"direction"`
	Label         string `json:"label"`
	OtherTicketID string `json:"other_ticket_id"`
	OtherCode     string `json:"other_code"`
	OtherTitle    string `json:"other_title"`
	BoardID       string `json:"board_id"`
	Status        string `json:"status"`
}

func linkLabel(typ string, inverse bool) string {
	if typ == "relates" {
		return "relates to"
	}
	if typ == "blocks" {
		if inverse {
			return "is blocked by"
		}
		return "blocks"
	}
	if inverse {
		return "is duplicated by"
	}
	return "duplicates"
}

func (s *Server) ticketLinks(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	if id == "" {
		id = idParam(r, "idOrCode")
	}
	var exists string
	if s.db.QueryRow("SELECT id FROM ticket WHERE id=? OR code=? OR id IN (SELECT ticket_id FROM old_ticket_code WHERE old_ticket_code=?)", id, id, id).Scan(&exists) != nil {
		errw(w, 404, "ticket not found")
		return
	}
	rows, err := s.db.Query(`SELECT l.id,l.type,l.from_ticket_id=t.id,o.id,o.code,o.title,o.board_id,st.name FROM ticket_link l JOIN ticket t ON (t.id=l.from_ticket_id OR t.id=l.to_ticket_id) JOIN ticket o ON o.id=CASE WHEN t.id=l.from_ticket_id THEN l.to_ticket_id ELSE l.from_ticket_id END JOIN status st ON st.id=o.status_id WHERE t.id=? ORDER BY l.created_at,l.rowid`, exists)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []ticketLink{}
	for rows.Next() {
		var x ticketLink
		var direct bool
		if err = rows.Scan(&x.ID, &x.Type, &direct, &x.OtherTicketID, &x.OtherCode, &x.OtherTitle, &x.BoardID, &x.Status); err != nil {
			errw(w, 500, err.Error())
			return
		}
		x.Direction = "outgoing"
		if !direct {
			x.Direction = "incoming"
		}
		x.Label = linkLabel(x.Type, !direct)
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, items)
}

func (s *Server) addTicketLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ToTicketID string `json:"to_ticket_id"`
		Code       string `json:"code"`
		Type       string `json:"type"`
	}
	if decode(r, &body) != nil {
		errw(w, 400, "invalid JSON")
		return
	}
	target := body.ToTicketID
	if target == "" {
		target = body.Code
	}
	if body.Type != "blocks" && body.Type != "relates" && body.Type != "duplicates" {
		errw(w, 400, "unknown link type")
		return
	}
	var id string
	err := txrun(s.db, func(tx *sql.Tx) error {
		var from, to string
		key := idParam(r, "id")
		if e := tx.QueryRow("SELECT id FROM ticket WHERE id=? OR code=? OR id IN (SELECT ticket_id FROM old_ticket_code WHERE old_ticket_code=?)", key, key, key).Scan(&from); e != nil {
			return e
		}
		if e := tx.QueryRow("SELECT id FROM ticket WHERE id=? OR code=? OR id IN (SELECT ticket_id FROM old_ticket_code WHERE old_ticket_code=?)", target, target, target).Scan(&to); e != nil {
			return e
		}
		if from == to {
			return errors.New("self link")
		}
		id = uid()
		stamp := now()
		if _, e := tx.Exec("INSERT INTO ticket_link(id,from_ticket_id,to_ticket_id,type,created_at) VALUES(?,?,?,?,?)", id, from, to, body.Type, stamp); e != nil {
			return e
		}
		var fromCode, toCode string
		if e := tx.QueryRow("SELECT code FROM ticket WHERE id=?", from).Scan(&fromCode); e != nil {
			return e
		}
		if e := tx.QueryRow("SELECT code FROM ticket WHERE id=?", to).Scan(&toCode); e != nil {
			return e
		}
		if e := s.historyTx(tx, from, "Linked: "+linkLabel(body.Type, false)+" "+toCode, stamp); e != nil {
			return e
		}
		return s.historyTx(tx, to, "Linked: "+linkLabel(body.Type, true)+" "+fromCode, stamp)
	})
	if err == sql.ErrNoRows {
		errw(w, 404, "ticket not found")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "self link") {
			errw(w, 400, err.Error())
		} else if strings.Contains(err.Error(), "UNIQUE") {
			errw(w, 409, "link already exists")
		} else {
			errw(w, 500, err.Error())
		}
		return
	}
	write(w, 201, map[string]string{"id": id})
}

func (s *Server) deleteTicketLink(w http.ResponseWriter, r *http.Request) {
	err := txrun(s.db, func(tx *sql.Tx) error {
		var from, to, typ, fromCode, toCode string
		if e := tx.QueryRow("SELECT from_ticket_id,to_ticket_id,type FROM ticket_link WHERE id=?", idParam(r, "id")).Scan(&from, &to, &typ); e != nil {
			return e
		}
		if e := tx.QueryRow("SELECT code FROM ticket WHERE id=?", from).Scan(&fromCode); e != nil {
			return e
		}
		if e := tx.QueryRow("SELECT code FROM ticket WHERE id=?", to).Scan(&toCode); e != nil {
			return e
		}
		if _, e := tx.Exec("DELETE FROM ticket_link WHERE id=?", idParam(r, "id")); e != nil {
			return e
		}
		stamp := now()
		if e := s.historyTx(tx, from, "Unlinked: "+linkLabel(typ, false)+" "+toCode, stamp); e != nil {
			return e
		}
		return s.historyTx(tx, to, "Unlinked: "+linkLabel(typ, true)+" "+fromCode, stamp)
	})
	if err == sql.ErrNoRows {
		errw(w, 404, "ticket link not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}
