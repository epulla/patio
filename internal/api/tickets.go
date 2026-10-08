package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func scanTicket(row interface{ Scan(...any) error }) (ticket, error) {
	var t ticket
	var d, st, ty, as sql.NullString
	e := row.Scan(&t.ID, &t.Title, &t.Description, &t.Number, &t.Code, &t.Created, &t.Updated, &d, &st, &t.StatusID, &t.BoardID, &ty, &t.ReporterID, &as)
	if d.Valid {
		t.DueDate = &d.String
	}
	if st.Valid {
		t.StatusUpdated = &st.String
	}
	if ty.Valid {
		t.TicketTypeID = &ty.String
	}
	if as.Valid {
		t.AssigneeID = &as.String
	}
	return t, e
}
func (s *Server) ticketBy(q interface{ QueryRow(string, ...any) *sql.Row }, id string) (ticket, error) {
	return scanTicket(q.QueryRow(`SELECT id,title,description,number,code,created_at,updated_at,due_date,status_updated_at,status_id,board_id,ticket_type_id,reporter_id,assignee_id FROM ticket WHERE id=?`, id))
}
func ticketInput(m map[string]json.RawMessage, k string) (string, bool, bool) {
	v, ok := m[k]
	if !ok {
		return "", false, false
	}
	if string(v) == "null" {
		return "", true, true
	}
	var x string
	if json.Unmarshal(v, &x) != nil {
		return "", true, false
	}
	return x, true, true
}
func (s *Server) createTicket(w http.ResponseWriter, r *http.Request) {
	var m map[string]json.RawMessage
	if decode(r, &m) != nil {
		errw(w, 400, "invalid JSON")
		return
	}
	title, ok, _ := ticketInput(m, "title")
	if !ok || !required(title) {
		errw(w, 400, "title required")
		return
	}
	desc, _, _ := ticketInput(m, "description")
	bid := idParam(r, "id")
	sid, has, _ := ticketInput(m, "status_id")
	tt, _, ttset := ticketInput(m, "ticket_type_id")
	due, _, dset := ticketInput(m, "due_date")
	ass, _, aset := ticketInput(m, "assignee_id")
	id, t := uid(), now()
	var out ticket
	e := txrun(s.db, func(tx *sql.Tx) error {
		var raw string
		if e := tx.QueryRow(`SELECT status_ids FROM status_order WHERE board_id=?`, bid).Scan(&raw); e != nil {
			return e
		}
		ids, e := parseList(raw)
		if e != nil {
			return e
		}
		if len(ids) == 0 {
			return errors.New("board has no statuses")
		}
		if !has {
			sid = ids[0]
		}
		if !contains(ids, sid) {
			return errors.New("status does not belong to board")
		}
		if ttset && tt != "" {
			var x string
			if e := tx.QueryRow(`SELECT id FROM ticket_type WHERE id=? AND board_id=? AND deleted_at IS NULL`, tt, bid).Scan(&x); e != nil {
				return errors.New("ticket type does not belong to board")
			}
		}
		if aset && ass != "" {
			var x string
			if e := tx.QueryRow(`SELECT id FROM "user" WHERE id=? AND deleted_at IS NULL`, ass).Scan(&x); e != nil {
				return errors.New("assignee not found")
			}
		}
		var num int
		if e := tx.QueryRow(`UPDATE board_counter SET last_number=last_number+1 WHERE board_id=? RETURNING last_number`, bid).Scan(&num); e != nil {
			return e
		}
		short, e := mustShort(tx, bid)
		if e != nil {
			return e
		}
		code := fmt.Sprintf("%s-%d", short, num)
		if _, e := tx.Exec(`INSERT INTO ticket(id,title,description,number,code,created_at,updated_at,status_updated_at,status_id,board_id,ticket_type_id,reporter_id,assignee_id,due_date) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, title, desc, num, code, t, t, t, sid, bid, nullIf(tt, ttset), s.userID, nullIf(ass, aset), nullIf(due, dset)); e != nil {
			return e
		}
		var order string
		if e := tx.QueryRow(`SELECT ticket_ids FROM ticket_order WHERE status_id=?`, sid).Scan(&order); e != nil {
			return e
		}
		a, e := parseList(order)
		if e != nil {
			return e
		}
		a = append(a, id)
		if _, e := tx.Exec(`UPDATE ticket_order SET ticket_ids=? WHERE status_id=?`, listJSON(a), sid); e != nil {
			return e
		}
		if e := s.historyTx(tx, id, "Ticket created", t); e != nil {
			return e
		}
		if e := s.transitionTx(tx, id, bid, "", sid, t); e != nil {
			return e
		}
		out = ticket{id, title, desc, num, code, t, t, nullable(due, dset), &t, &sid, &bid, nullable(tt, ttset), ptr(s.userID), nullable(ass, aset)}
		return nil
	})
	if e != nil {
		if strings.Contains(e.Error(), "does not belong") || strings.Contains(e.Error(), "no statuses") {
			errw(w, 400, e.Error())
		} else if e == sql.ErrNoRows {
			errw(w, 404, "board not found")
		} else {
			errw(w, 500, e.Error())
		}
		return
	}
	write(w, 201, out)
}
func nullIf(s string, set bool) any {
	if !set || s == "" {
		return nil
	}
	return s
}
func nullable(s string, set bool) *string {
	if !set || s == "" {
		return nil
	}
	return &s
}
func ptr(s string) *string { return &s }
func mustShort(tx *sql.Tx, bid string) (string, error) {
	var x string
	e := tx.QueryRow(`SELECT short_name FROM board WHERE id=?`, bid).Scan(&x)
	return x, e
}
func (s *Server) historyTx(tx *sql.Tx, tid, msg, t string) error {
	_, e := tx.Exec(`INSERT INTO ticket_history(id,content,created_at,ticket_id,user_id) VALUES(?,?,?,?,?)`, uid(), msg, t, tid, s.userID)
	return e
}
func (s *Server) transitionTx(tx *sql.Tx, tid, bid, from, to, t string) error {
	var f any = from
	if from == "" {
		f = nil
	}
	_, e := tx.Exec(`INSERT INTO ticket_status_transition(id,ticket_id,board_id,from_status_id,to_status_id,user_id,moved_at) VALUES(?,?,?,?,?,?,?)`, uid(), tid, bid, f, to, s.userID, t)
	return e
}
func (s *Server) searchTickets(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		errw(w, http.StatusBadRequest, "q required")
		return
	}
	q = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(q)
	query := `SELECT t.id,t.title,t.description,t.number,t.code,t.created_at,t.updated_at,t.due_date,t.status_updated_at,t.status_id,t.board_id,t.ticket_type_id,t.reporter_id,t.assignee_id
		FROM ticket t JOIN board b ON b.id=t.board_id
		WHERE b.deleted_at IS NULL AND (t.title LIKE '%' || ? || '%' ESCAPE '\' OR t.code LIKE '%' || ? || '%' ESCAPE '\')`
	args := []any{q, q}
	if boardID := strings.TrimSpace(r.URL.Query().Get("board_id")); boardID != "" {
		query += " AND t.board_id=?"
		args = append(args, boardID)
	}
	query += " ORDER BY b.created_at,t.number"
	rows, e := s.db.Query(query, args...)
	if e != nil {
		errw(w, http.StatusInternalServerError, e.Error())
		return
	}
	defer rows.Close()
	out := []ticket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, t)
	}
	if e = rows.Err(); e != nil {
		errw(w, http.StatusInternalServerError, e.Error())
		return
	}
	write(w, http.StatusOK, out)
}
func (s *Server) getTicket(w http.ResponseWriter, r *http.Request) {
	key := idParam(r, "idOrCode")
	row := s.db.QueryRow(`SELECT id,title,description,number,code,created_at,updated_at,due_date,status_updated_at,status_id,board_id,ticket_type_id,reporter_id,assignee_id FROM ticket WHERE id=? OR code=? OR id IN (SELECT ticket_id FROM old_ticket_code WHERE old_ticket_code=?)`, key, key, key)
	t, e := scanTicket(row)
	if e == sql.ErrNoRows {
		errw(w, 404, "ticket not found")
		return
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	write(w, 200, t)
}

func (s *Server) updateTicket(w http.ResponseWriter, r *http.Request) {
	var m map[string]json.RawMessage
	if decode(r, &m) != nil {
		errw(w, 400, "invalid JSON")
		return
	}
	id := idParam(r, "id")
	var out ticket
	e := txrun(s.db, func(tx *sql.Tx) error {
		out, e := scanTicket(tx.QueryRow(`SELECT id,title,description,number,code,created_at,updated_at,due_date,status_updated_at,status_id,board_id,ticket_type_id,reporter_id,assignee_id FROM ticket WHERE id=?`, id))
		if e != nil {
			return e
		}
		bid := out.BoardID
		updates := []string{}
		args := []any{}
		for _, k := range []string{"title", "description"} {
			if v, ok := m[k]; ok {
				var x string
				if json.Unmarshal(v, &x) != nil || (!required(x) && k == "title") {
					return errors.New("invalid " + k)
				}
				updates = append(updates, k+"=?")
				args = append(args, x)
				if e = s.historyTx(tx, id, "Updated "+k, now()); e != nil {
					return e
				}
			}
		}
		for _, x := range []struct {
			k, col string
			dst    **string
		}{{"due_date", "due_date", &out.DueDate}, {"assignee_id", "assignee_id", &out.AssigneeID}, {"ticket_type_id", "ticket_type_id", &out.TicketTypeID}} {
			if v, ok := m[x.k]; ok {
				if string(v) == "null" {
					updates = append(updates, x.col+"=NULL")
					*x.dst = nil
				} else {
					var z string
					if json.Unmarshal(v, &z) != nil {
						return errors.New("invalid " + x.k)
					}
					if x.k == "assignee_id" {
						var q string
						if tx.QueryRow(`SELECT id FROM "user" WHERE id=? AND deleted_at IS NULL`, z).Scan(&q) != nil {
							return errors.New("assignee not found")
						}
					}
					if x.k == "ticket_type_id" {
						var q string
						if tx.QueryRow(`SELECT id FROM ticket_type WHERE id=? AND board_id=? AND deleted_at IS NULL`, z, bid).Scan(&q) != nil {
							return errors.New("ticket type does not belong to board")
						}
					}
					updates = append(updates, x.col+"=?")
					args = append(args, z)
					*x.dst = &z
				}
				if e = s.historyTx(tx, id, "Updated "+x.k, now()); e != nil {
					return e
				}
			}
		}
		if v, ok := m["status_id"]; ok {
			var z string
			if json.Unmarshal(v, &z) != nil {
				return errors.New("invalid status_id")
			}
			if out.StatusID == nil || z != *out.StatusID {
				var index int
				index, e = lenMust(tx, z)
				if e == nil {
					e = s.moveTx(tx, out, id, z, index, true)
				}
				if e != nil {
					return e
				}
				out.StatusID = &z
			}
		}
		if len(updates) > 0 {
			args = append(args, now(), id)
			if _, e = tx.Exec(`UPDATE ticket SET `+strings.Join(updates, ",")+`,updated_at=? WHERE id=?`, args...); e != nil {
				return e
			}
		}
		out, e = scanTicket(tx.QueryRow(`SELECT id,title,description,number,code,created_at,updated_at,due_date,status_updated_at,status_id,board_id,ticket_type_id,reporter_id,assignee_id FROM ticket WHERE id=?`, id))
		return e
	})
	if e != nil {
		if e == sql.ErrNoRows {
			errw(w, 404, "ticket not found")
		} else if strings.Contains(e.Error(), "invalid") || strings.Contains(e.Error(), "belong") || strings.Contains(e.Error(), "assignee") {
			errw(w, 400, e.Error())
		} else {
			errw(w, 500, e.Error())
		}
		return
	}
	write(w, 200, out)
}
func lenMust(tx *sql.Tx, sid string) (int, error) {
	var x string
	if e := tx.QueryRow(`SELECT ticket_ids FROM ticket_order WHERE status_id=?`, sid).Scan(&x); e != nil {
		return 0, e
	}
	a, e := parseList(x)
	return len(a), e
}
func (s *Server) moveTx(tx *sql.Tx, t ticket, id, target string, index int, audit bool) error {
	if t.StatusID != nil && *t.StatusID == target {
		return reorderTx(tx, target, id, index)
	}
	var board string
	if e := tx.QueryRow(`SELECT board_id FROM status WHERE id=? AND deleted_at IS NULL`, target).Scan(&board); e != nil {
		return errors.New("status does not belong to board")
	}
	if board != *t.BoardID {
		return errors.New("status does not belong to board")
	}
	var old, newraw string
	if e := tx.QueryRow(`SELECT ticket_ids FROM ticket_order WHERE status_id=?`, *t.StatusID).Scan(&old); e != nil {
		return e
	}
	if e := tx.QueryRow(`SELECT ticket_ids FROM ticket_order WHERE status_id=?`, target).Scan(&newraw); e != nil {
		return e
	}
	oa, e := parseList(old)
	if e != nil {
		return e
	}
	na, e := parseList(newraw)
	if e != nil {
		return e
	}
	oa = remove(oa, id)
	if index < 0 {
		index = 0
	}
	if index > len(na) {
		index = len(na)
	}
	na = append(na, "")
	copy(na[index+1:], na[index:])
	na[index] = id
	tm := now()
	if _, e := tx.Exec(`UPDATE ticket_order SET ticket_ids=? WHERE status_id=?`, listJSON(oa), *t.StatusID); e != nil {
		return e
	}
	if _, e := tx.Exec(`UPDATE ticket_order SET ticket_ids=? WHERE status_id=?`, listJSON(na), target); e != nil {
		return e
	}
	if _, e := tx.Exec(`UPDATE ticket SET status_id=?,status_updated_at=?,updated_at=? WHERE id=?`, target, tm, tm, id); e != nil {
		return e
	}
	if audit {
		var fromName, toName string
		if e := tx.QueryRow(`SELECT name FROM status WHERE id=?`, *t.StatusID).Scan(&fromName); e != nil {
			return e
		}
		if e := tx.QueryRow(`SELECT name FROM status WHERE id=?`, target).Scan(&toName); e != nil {
			return e
		}
		if e := s.historyTx(tx, id, fmt.Sprintf("Status changed from %s to %s", fromName, toName), tm); e != nil {
			return e
		}
		return s.transitionTx(tx, id, *t.BoardID, *t.StatusID, target, tm)
	}
	return nil
}
func reorderTx(tx *sql.Tx, sid, id string, index int) error {
	var raw string
	if e := tx.QueryRow(`SELECT ticket_ids FROM ticket_order WHERE status_id=?`, sid).Scan(&raw); e != nil {
		return e
	}
	a, e := parseList(raw)
	if e != nil {
		return e
	}
	if !contains(a, id) {
		return errors.New("ticket not in status")
	}
	a = remove(a, id)
	if index < 0 {
		index = 0
	}
	if index > len(a) {
		index = len(a)
	}
	a = append(a, "")
	copy(a[index+1:], a[index:])
	a[index] = id
	_, e = tx.Exec(`UPDATE ticket_order SET ticket_ids=? WHERE status_id=?`, listJSON(a), sid)
	return e
}
func (s *Server) moveTicket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		StatusID string `json:"status_id"`
		Index    int    `json:"index"`
	}
	if decode(r, &in) != nil || !required(in.StatusID) {
		errw(w, 400, "status_id and index required")
		return
	}
	id := idParam(r, "id")
	var out ticket
	e := txrun(s.db, func(tx *sql.Tx) error {
		var e error
		out, e = scanTicket(tx.QueryRow(`SELECT id,title,description,number,code,created_at,updated_at,due_date,status_updated_at,status_id,board_id,ticket_type_id,reporter_id,assignee_id FROM ticket WHERE id=?`, id))
		if e != nil {
			return e
		}
		if e = s.moveTx(tx, out, id, in.StatusID, in.Index, true); e != nil {
			return e
		}
		out, e = scanTicket(tx.QueryRow(`SELECT id,title,description,number,code,created_at,updated_at,due_date,status_updated_at,status_id,board_id,ticket_type_id,reporter_id,assignee_id FROM ticket WHERE id=?`, id))
		return e
	})
	if e != nil {
		if e == sql.ErrNoRows {
			errw(w, 404, "ticket not found")
		} else if strings.Contains(e.Error(), "status") || strings.Contains(e.Error(), "ticket not") {
			errw(w, 400, e.Error())
		} else {
			errw(w, 500, e.Error())
		}
		return
	}
	write(w, 200, out)
}
func (s *Server) deleteTicket(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	e := txrun(s.db, func(tx *sql.Tx) error {
		var sid, raw string
		if e := tx.QueryRow(`SELECT status_id FROM ticket WHERE id=?`, id).Scan(&sid); e != nil {
			return e
		}
		if e := tx.QueryRow(`SELECT ticket_ids FROM ticket_order WHERE status_id=?`, sid).Scan(&raw); e != nil {
			return e
		}
		a, e := parseList(raw)
		if e != nil {
			return e
		}
		a = remove(a, id)
		if _, e := tx.Exec(`UPDATE ticket_order SET ticket_ids=? WHERE status_id=?`, listJSON(a), sid); e != nil {
			return e
		}
		_, e = tx.Exec(`DELETE FROM ticket WHERE id=?`, id)
		return e
	})
	if e == sql.ErrNoRows {
		errw(w, 404, "ticket not found")
	} else if e != nil {
		errw(w, 500, e.Error())
	} else {
		noContent(w)
	}
}
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT h.id,h.content,h.created_at,h.user_id,u.name FROM ticket_history h JOIN "user" u ON u.id=h.user_id WHERE h.ticket_id=? ORDER BY h.created_at,h.rowid`, idParam(r, "id"))
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	defer rows.Close()
	a := []history{}
	for rows.Next() {
		var x history
		if e = rows.Scan(&x.ID, &x.Content, &x.Created, &x.UserID, &x.UserName); e != nil {
			errw(w, 500, e.Error())
			return
		}
		a = append(a, x)
	}
	if e = rows.Err(); e != nil {
		errw(w, 500, e.Error())
		return
	}
	write(w, 200, a)
}
func (s *Server) transitions(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT x.id,x.from_status_id,fs.name,x.to_status_id,ts.name,x.user_id,u.name,x.moved_at FROM ticket_status_transition x LEFT JOIN status fs ON fs.id=x.from_status_id JOIN status ts ON ts.id=x.to_status_id JOIN "user" u ON u.id=x.user_id WHERE x.ticket_id=? ORDER BY x.moved_at,x.rowid`, idParam(r, "id"))
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	defer rows.Close()
	a := []transition{}
	for rows.Next() {
		var x transition
		var fid, fn, tid, tn, uid, un sql.NullString
		if e = rows.Scan(&x.ID, &fid, &fn, &tid, &tn, &uid, &un, &x.Moved); e != nil {
			errw(w, 500, e.Error())
			return
		}
		if fid.Valid {
			x.FromID = &fid.String
		}
		if fn.Valid {
			x.FromName = &fn.String
		}
		if tid.Valid {
			x.ToID = &tid.String
		}
		if tn.Valid {
			x.ToName = &tn.String
		}
		if uid.Valid {
			x.UserID = &uid.String
		}
		if un.Valid {
			x.UserName = &un.String
		}
		a = append(a, x)
	}
	if e = rows.Err(); e != nil {
		errw(w, 500, e.Error())
		return
	}
	write(w, 200, a)
}
func (s *Server) comments(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT c.id,c.content,c.created_at,c.updated_at,c.user_id,u.name FROM ticket_comment c JOIN "user" u ON u.id=c.user_id WHERE c.ticket_id=? ORDER BY c.created_at,c.rowid`, idParam(r, "id"))
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	defer rows.Close()
	a := []comment{}
	for rows.Next() {
		var x comment
		if e = rows.Scan(&x.ID, &x.Content, &x.Created, &x.Updated, &x.UserID, &x.UserName); e != nil {
			errw(w, 500, e.Error())
			return
		}
		a = append(a, x)
	}
	if e = rows.Err(); e != nil {
		errw(w, 500, e.Error())
		return
	}
	write(w, 200, a)
}
func (s *Server) addComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content string `json:"content"`
	}
	if decode(r, &in) != nil || !required(in.Content) {
		errw(w, 400, "content required")
		return
	}
	id, tid, t := uid(), idParam(r, "id"), now()
	var c comment
	e := txrun(s.db, func(tx *sql.Tx) error {
		var x string
		if e := tx.QueryRow(`SELECT id FROM ticket WHERE id=?`, tid).Scan(&x); e != nil {
			return e
		}
		if _, e := tx.Exec(`INSERT INTO ticket_comment(id,content,created_at,updated_at,ticket_id,user_id) VALUES(?,?,?,?,?,?)`, id, in.Content, t, t, tid, s.userID); e != nil {
			return e
		}
		c = comment{id, in.Content, t, t, s.userID, ""}
		return tx.QueryRow(`SELECT name FROM "user" WHERE id=?`, s.userID).Scan(&c.UserName)
	})
	if e == sql.ErrNoRows {
		errw(w, 404, "ticket not found")
	} else if e != nil {
		errw(w, 500, e.Error())
	} else {
		write(w, 201, c)
	}
}
