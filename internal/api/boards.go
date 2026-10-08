package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	write(w, 200, map[string]bool{"show_hidden": s.showHidden})
}
func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT id,name,last_name,email FROM "user" WHERE deleted_at IS NULL ORDER BY name`)
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	defer rows.Close()
	out := []user{}
	for rows.Next() {
		var u user
		if e = rows.Scan(&u.ID, &u.Name, &u.LastName, &u.Email); e != nil {
			errw(w, 500, e.Error())
			return
		}
		out = append(out, u)
	}
	if e = rows.Err(); e != nil {
		errw(w, 500, e.Error())
		return
	}
	write(w, 200, out)
}
func scanBoard(row interface{ Scan(...any) error }) (board, error) {
	var b board
	e := row.Scan(&b.ID, &b.Name, &b.Short, &b.Template, &b.Created, &b.Updated)
	return b, e
}
func (s *Server) boardBy(q interface{ QueryRow(string, ...any) *sql.Row }, id string) (board, error) {
	return scanBoard(q.QueryRow(`SELECT id,name,short_name,template,created_at,updated_at FROM board WHERE id=? AND deleted_at IS NULL`, id))
}
func (s *Server) boards(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT id,name,short_name,template,created_at,updated_at FROM board WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	defer rows.Close()
	a := []board{}
	for rows.Next() {
		b, e := scanBoard(rows)
		if e != nil {
			errw(w, 500, e.Error())
			return
		}
		a = append(a, b)
	}
	if e = rows.Err(); e != nil {
		errw(w, 500, e.Error())
		return
	}
	write(w, 200, a)
}
func getString(m map[string]json.RawMessage, k string) (string, bool) {
	v, ok := m[k]
	if !ok {
		return "", false
	}
	var x string
	if json.Unmarshal(v, &x) != nil {
		return "", false
	}
	return x, true
}
func (s *Server) createBoard(w http.ResponseWriter, r *http.Request) {
	var m map[string]json.RawMessage
	if decode(r, &m) != nil {
		errw(w, 400, "invalid JSON")
		return
	}
	name, _ := getString(m, "name")
	short, _ := getString(m, "short_name")
	short = shortName(short)
	template, _ := getString(m, "template")
	if !required(name) || !validShort(short) {
		errw(w, 400, "name and valid short_name required")
		return
	}
	statuses := []string{"To Do", "In Progress", "Done"}
	if raw, ok := m["statuses"]; ok {
		if json.Unmarshal(raw, &statuses) != nil || len(statuses) == 0 {
			errw(w, 400, "statuses must be non-empty strings")
			return
		}
	}
	id, t := uid(), now()
	var b board
	e := txrun(s.db, func(tx *sql.Tx) error {
		if _, e := tx.Exec(`INSERT INTO board(id,name,short_name,template,created_at,updated_at) VALUES(?,?,?,?,?,?)`, id, name, short, template, t, t); e != nil {
			return e
		}
		if _, e := tx.Exec(`INSERT INTO board_counter(last_number,board_id) VALUES(0,?)`, id); e != nil {
			return e
		}
		if _, e := tx.Exec(`INSERT INTO status_order(id,status_ids,board_id) VALUES(?,?,?)`, uid(), "[]", id); e != nil {
			return e
		}
		if _, e := tx.Exec(`INSERT INTO ticket_type_order(id,ticket_type_ids,board_id) VALUES(?,?,?)`, uid(), "[]", id); e != nil {
			return e
		}
		ids := []string{}
		for _, n := range statuses {
			if !required(n) {
				return errors.New("status name required")
			}
			sid := uid()
			ids = append(ids, sid)
			if _, e := tx.Exec(`INSERT INTO status(id,name,type,board_id) VALUES(?,?,?,?)`, sid, n, "classic", id); e != nil {
				return e
			}
			if _, e := tx.Exec(`INSERT INTO ticket_order(id,status_id) VALUES(?,?)`, uid(), sid); e != nil {
				return e
			}
		}
		_, e := tx.Exec(`UPDATE status_order SET status_ids=? WHERE board_id=?`, listJSON(ids), id)
		if e != nil {
			return e
		}
		b = board{id, name, short, template, t, t}
		return nil
	})
	if e != nil {
		if strings.Contains(e.Error(), "UNIQUE") {
			errw(w, 409, "short_name already exists")
		} else {
			errw(w, 500, e.Error())
		}
		return
	}
	write(w, 201, b)
}
func (s *Server) board(w http.ResponseWriter, r *http.Request) {
	b, e := s.boardBy(s.db, idParam(r, "id"))
	if e == sql.ErrNoRows {
		errw(w, 404, "board not found")
		return
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	write(w, 200, b)
}

type boardInput struct {
	Name     string `json:"name"`
	Short    string `json:"short_name"`
	Template string `json:"template"`
}

func (s *Server) updateBoard(w http.ResponseWriter, r *http.Request) {
	var in boardInput
	if decode(r, &in) != nil || !required(in.Name) || !validShort(shortName(in.Short)) {
		errw(w, 400, "name and valid short_name required")
		return
	}
	id := idParam(r, "id")
	in.Short = shortName(in.Short)
	var b board
	e := txrun(s.db, func(tx *sql.Tx) error {
		var old string
		if e := tx.QueryRow(`SELECT short_name FROM board WHERE id=? AND deleted_at IS NULL`, id).Scan(&old); e != nil {
			return e
		}
		t := now()
		if e := tx.QueryRow(`SELECT id,name,short_name,template,created_at,updated_at FROM board WHERE id=?`, id).Scan(&b.ID, &b.Name, &b.Short, &b.Template, &b.Created, &b.Updated); e != nil {
			return e
		}
		if old != in.Short {
			rows, e := tx.Query(`SELECT id,code FROM ticket WHERE board_id=?`, id)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var tid, code string
				if e = rows.Scan(&tid, &code); e != nil {
					return e
				}
				if _, e = tx.Exec(`INSERT INTO old_ticket_code(old_ticket_code,ticket_id) VALUES(?,?)`, code, tid); e != nil {
					return e
				}
				var n int
				if e = tx.QueryRow(`SELECT number FROM ticket WHERE id=?`, tid).Scan(&n); e != nil {
					return e
				}
				if _, e = tx.Exec(`UPDATE ticket SET code=?,updated_at=? WHERE id=?`, fmt.Sprintf("%s-%d", in.Short, n), t, tid); e != nil {
					return e
				}
			}
			if e = rows.Err(); e != nil {
				return e
			}
		}
		_, e := tx.Exec(`UPDATE board SET name=?,short_name=?,template=?,updated_at=? WHERE id=?`, in.Name, in.Short, in.Template, t, id)
		if e != nil {
			return e
		}
		return tx.QueryRow(`SELECT id,name,short_name,template,created_at,updated_at FROM board WHERE id=?`, id).Scan(&b.ID, &b.Name, &b.Short, &b.Template, &b.Created, &b.Updated)
	})
	if e != nil {
		if e == sql.ErrNoRows {
			errw(w, 404, "board not found")
		} else if strings.Contains(e.Error(), "UNIQUE") {
			errw(w, 409, "short_name already exists")
		} else {
			errw(w, 500, e.Error())
		}
		return
	}
	write(w, 200, b)
}
func (s *Server) deleteBoard(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	res, e := s.db.Exec(`UPDATE board SET deleted_at=?,updated_at=? WHERE id=? AND deleted_at IS NULL`, now(), now(), id)
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	n, e := res.RowsAffected()
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	if n == 0 {
		errw(w, 404, "board not found")
		return
	}
	noContent(w)
}

func (s *Server) boardData(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	b, e := s.boardBy(s.db, id)
	if e == sql.ErrNoRows {
		errw(w, 404, "board not found")
		return
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	var order string
	if e = s.db.QueryRow(`SELECT status_ids FROM status_order WHERE board_id=?`, id).Scan(&order); e != nil {
		errw(w, 500, e.Error())
		return
	}
	ids, _ := parseList(order)
	var typesOrder string
	if e = s.db.QueryRow(`SELECT ticket_type_ids FROM ticket_type_order WHERE board_id=?`, id).Scan(&typesOrder); e != nil {
		errw(w, 500, e.Error())
		return
	}
	typeids, _ := parseList(typesOrder)
	sts := []status{}
	tickets := map[string][]ticket{}
	for _, sid := range ids {
		var st status
		if e = s.db.QueryRow(`SELECT id,name,type,board_id FROM status WHERE id=? AND deleted_at IS NULL`, sid).Scan(&st.ID, &st.Name, &st.Type, &st.BoardID); e != nil {
			if e == sql.ErrNoRows {
				continue
			}
			errw(w, 500, e.Error())
			return
		}
		var to string
		if e = s.db.QueryRow(`SELECT ticket_ids FROM ticket_order WHERE status_id=?`, sid).Scan(&to); e != nil {
			if e == sql.ErrNoRows {
				to = "[]"
			} else {
				errw(w, 500, e.Error())
				return
			}
		}
		tids, e := parseList(to)
		if e != nil {
			errw(w, 500, e.Error())
			return
		}
		for _, tid := range tids {
			tt, e := s.ticketBy(s.db, tid)
			if e == sql.ErrNoRows {
				continue
			}
			if e != nil {
				errw(w, 500, e.Error())
				return
			}
			if e == nil {
				if st.Type != "temporary" || recent(tt.StatusUpdated) {
					if q := r.URL.Query().Get("q"); q == "" || strings.Contains(strings.ToLower(tt.Title), strings.ToLower(q)) || strings.Contains(strings.ToLower(tt.Code), strings.ToLower(q)) {
						tickets[sid] = append(tickets[sid], tt)
					}
				}
			}
		}
		sts = append(sts, st)
	}
	ts := []ticketType{}
	for _, tid := range typeids {
		var x ticketType
		e = s.db.QueryRow(`SELECT id,name,color,board_id FROM ticket_type WHERE id=? AND deleted_at IS NULL`, tid).Scan(&x.ID, &x.Name, &x.Color, &x.BoardID)
		if e == sql.ErrNoRows {
			continue
		}
		if e != nil {
			errw(w, 500, e.Error())
			return
		}
		ts = append(ts, x)
	}
	type dataStatus struct {
		status
		Tickets []ticket `json:"tickets"`
	}
	out := struct {
		Board       board        `json:"board"`
		Statuses    []dataStatus `json:"statuses"`
		TicketTypes []ticketType `json:"ticket_types"`
	}{Board: b, TicketTypes: ts}
	for _, x := range sts {
		column := tickets[x.ID]
		if column == nil {
			column = []ticket{}
		}
		out.Statuses = append(out.Statuses, dataStatus{status: x, Tickets: column})
	}
	write(w, 200, out)
}
func recent(v *string) bool {
	if v == nil {
		return false
	}
	t, e := time.Parse("2006-01-02T15:04:05.000Z", *v)
	return e == nil && time.Since(t) < 24*time.Hour
}

func (s *Server) createStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if decode(r, &in) != nil || !required(in.Name) || (in.Type != "classic" && in.Type != "temporary") {
		errw(w, 400, "name and valid type required")
		return
	}
	id, bid := uid(), idParam(r, "id")
	var out status
	e := txrun(s.db, func(tx *sql.Tx) error {
		var x string
		if e := tx.QueryRow(`SELECT id FROM board WHERE id=? AND deleted_at IS NULL`, bid).Scan(&x); e != nil {
			return e
		}
		if e := tx.QueryRow(`SELECT status_ids FROM status_order WHERE board_id=?`, bid).Scan(&x); e != nil {
			return e
		}
		a, _ := parseList(x)
		a = append(a, id)
		if _, e := tx.Exec(`INSERT INTO status(id,name,type,board_id) VALUES(?,?,?,?)`, id, in.Name, in.Type, bid); e != nil {
			return e
		}
		if _, e := tx.Exec(`INSERT INTO ticket_order(id,status_id) VALUES(?,?)`, uid(), id); e != nil {
			return e
		}
		_, e := tx.Exec(`UPDATE status_order SET status_ids=? WHERE board_id=?`, listJSON(a), bid)
		out = status{id, in.Name, in.Type, bid}
		return e
	})
	if e != nil {
		if e == sql.ErrNoRows {
			errw(w, 404, "board not found")
		} else {
			errw(w, 500, e.Error())
		}
		return
	}
	write(w, 201, out)
}
func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if decode(r, &in) != nil || !required(in.Name) || (in.Type != "classic" && in.Type != "temporary") {
		errw(w, 400, "name and valid type required")
		return
	}
	var x status
	e := s.db.QueryRow(`UPDATE status SET name=?,type=?,updated_at=? WHERE id=? AND deleted_at IS NULL RETURNING id,name,type,board_id`, in.Name, in.Type, now(), idParam(r, "id")).Scan(&x.ID, &x.Name, &x.Type, &x.BoardID)
	if e == sql.ErrNoRows {
		errw(w, 404, "status not found")
	} else if e != nil {
		errw(w, 500, e.Error())
	} else {
		write(w, 200, x)
	}
}
func (s *Server) deleteStatus(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	var n int
	if e := s.db.QueryRow(`SELECT COUNT(*) FROM ticket WHERE status_id=?`, id).Scan(&n); e != nil {
		errw(w, 500, e.Error())
		return
	}
	if n > 0 {
		errw(w, 409, "status has tickets")
		return
	}
	res, e := s.db.Exec(`UPDATE status SET deleted_at=? WHERE id=? AND deleted_at IS NULL`, now(), id)
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	n2, e := res.RowsAffected()
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	if n2 == 0 {
		errw(w, 404, "status not found")
		return
	}
	noContent(w)
}
func (s *Server) statusOrder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"status_ids"`
	}
	if decode(r, &in) != nil {
		errw(w, 400, "invalid JSON")
		return
	}
	bid := idParam(r, "id")
	var raw string
	if e := s.db.QueryRow(`SELECT status_ids FROM status_order WHERE board_id=?`, bid).Scan(&raw); e != nil {
		errw(w, 404, "board not found")
		return
	}
	old, e := parseList(raw)
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	if len(in.IDs) != len(old) {
		errw(w, 400, "status_ids must match board statuses")
		return
	}
	for _, id := range in.IDs {
		if !contains(old, id) {
			errw(w, 400, "status_ids must match board statuses")
			return
		}
	}
	if _, e := s.db.Exec(`UPDATE status_order SET status_ids=? WHERE board_id=?`, listJSON(in.IDs), bid); e != nil {
		errw(w, 500, e.Error())
		return
	}
	noContent(w)
}

func (s *Server) createType(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if decode(r, &in) != nil || !required(in.Name) {
		errw(w, 400, "name required")
		return
	}
	id, bid := uid(), idParam(r, "id")
	var o ticketType
	e := txrun(s.db, func(tx *sql.Tx) error {
		var x string
		if e := tx.QueryRow(`SELECT id FROM board WHERE id=? AND deleted_at IS NULL`, bid).Scan(&x); e != nil {
			return e
		}
		if e := tx.QueryRow(`SELECT ticket_type_ids FROM ticket_type_order WHERE board_id=?`, bid).Scan(&x); e != nil {
			return e
		}
		a, _ := parseList(x)
		a = append(a, id)
		if _, e := tx.Exec(`INSERT INTO ticket_type(id,name,color,board_id) VALUES(?,?,?,?)`, id, in.Name, in.Color, bid); e != nil {
			return e
		}
		_, e := tx.Exec(`UPDATE ticket_type_order SET ticket_type_ids=? WHERE board_id=?`, listJSON(a), bid)
		o = ticketType{id, in.Name, in.Color, bid}
		return e
	})
	if e == sql.ErrNoRows {
		errw(w, 404, "board not found")
	} else if e != nil {
		errw(w, 500, e.Error())
	} else {
		write(w, 201, o)
	}
}
func (s *Server) updateType(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if decode(r, &in) != nil || !required(in.Name) {
		errw(w, 400, "name required")
		return
	}
	var o ticketType
	e := s.db.QueryRow(`UPDATE ticket_type SET name=?,color=?,updated_at=? WHERE id=? AND deleted_at IS NULL RETURNING id,name,color,board_id`, in.Name, in.Color, now(), idParam(r, "id")).Scan(&o.ID, &o.Name, &o.Color, &o.BoardID)
	if e == sql.ErrNoRows {
		errw(w, 404, "ticket type not found")
	} else if e != nil {
		errw(w, 500, e.Error())
	} else {
		write(w, 200, o)
	}
}
func (s *Server) deleteType(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	e := txrun(s.db, func(tx *sql.Tx) error {
		res, e := tx.Exec(`UPDATE ticket_type SET deleted_at=? WHERE id=? AND deleted_at IS NULL`, now(), id)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		_, e = tx.Exec(`UPDATE ticket SET ticket_type_id=NULL WHERE ticket_type_id=?`, id)
		return e
	})
	if e != nil {
		if e == sql.ErrNoRows {
			errw(w, 404, "ticket type not found")
			return
		}
		errw(w, 500, e.Error())
		return
	}
	noContent(w)
}
