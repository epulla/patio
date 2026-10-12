package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

type calendarItem struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	StartsAt      string  `json:"starts_at"`
	EndsAt        *string `json:"ends_at"`
	AllDay        bool    `json:"all_day"`
	BoardID       *string `json:"board_id"`
	TicketID      *string `json:"ticket_id"`
	CreatedBy     string  `json:"created_by"`
	CreatedByIsAI bool    `json:"created_by_is_ai"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	DeletedAt     *string `json:"deleted_at"`
	NotifiedAt    *string `json:"notified_at"`
}

func validDate(v string) bool {
	t, err := time.Parse("2006-01-02", v)
	return err == nil && t.Format("2006-01-02") == v
}

func parseCalendarTime(v string, allDay bool) (string, time.Time, error) {
	if allDay {
		if !validDate(v) {
			return "", time.Time{}, errors.New("all-day dates must be YYYY-MM-DD")
		}
		t, err := time.Parse("2006-01-02", v)
		return v, t, err
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return "", time.Time{}, errors.New("timed values must be RFC3339")
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z"), t, nil
}

func scanCalendarItem(row interface{ Scan(...any) error }) (calendarItem, error) {
	var x calendarItem
	var ends, boardID, ticketID, deleted, notified sql.NullString
	var allDay int
	err := row.Scan(&x.ID, &x.Kind, &x.Title, &x.Description, &x.StartsAt, &ends, &allDay, &boardID, &ticketID, &x.CreatedBy, &x.CreatedByIsAI, &x.CreatedAt, &x.UpdatedAt, &deleted, &notified)
	if ends.Valid {
		x.EndsAt = &ends.String
	}
	if boardID.Valid {
		x.BoardID = &boardID.String
	}
	if ticketID.Valid {
		x.TicketID = &ticketID.String
	}
	if deleted.Valid {
		x.DeletedAt = &deleted.String
	}
	if notified.Valid {
		x.NotifiedAt = &notified.String
	}
	x.AllDay = allDay != 0
	return x, err
}

const calendarItemSelect = `SELECT c.id,c.kind,c.title,c.description,c.starts_at,c.ends_at,c.all_day,c.board_id,c.ticket_id,c.created_by,u.is_ai,c.created_at,c.updated_at,c.deleted_at,c.notified_at FROM calendar_item c JOIN "user" u ON u.id=c.created_by`

type scheduledCalendarItem struct {
	TaskID string  `json:"task_id"`
	Title  string  `json:"title"`
	At     string  `json:"at"`
	RunID  *string `json:"run_id"`
	ChatID *string `json:"chat_id"`
	Status *string `json:"status"`
	Late   bool    `json:"late"`
}

func (s *Server) scheduledCalendar(from, to string) ([]scheduledCalendarItem, error) {
	fromDate, err := time.ParseInLocation("2006-01-02", from, time.Local)
	if err != nil {
		return nil, err
	}
	toDate, err := time.ParseInLocation("2006-01-02", to, time.Local)
	if err != nil {
		return nil, err
	}
	end := toDate.AddDate(0, 0, 1)
	inRange := func(t time.Time) bool { return !t.Before(fromDate) && t.Before(end) }
	layout := "2006-01-02T15:04:05.000Z"
	out := []scheduledCalendarItem{}
	rows, err := s.db.Query(`SELECT r.id,r.task_id,t.title,r.scheduled_for,r.chat_id,r.status,r.late FROM scheduled_task_run r JOIN scheduled_task t ON t.id=r.task_id WHERE r.scheduled_for>=? AND r.scheduled_for<? ORDER BY r.scheduled_for,r.rowid`, fromDate.UTC().Format(layout), end.UTC().Format(layout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var x scheduledCalendarItem
		if err := rows.Scan(&x.RunID, &x.TaskID, &x.Title, &x.At, &x.ChatID, &x.Status, &x.Late); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tasks, err := s.db.Query(`SELECT id,title,schedule,next_run_at FROM scheduled_task WHERE enabled=1 AND next_run_at IS NOT NULL ORDER BY created_at,rowid`)
	if err != nil {
		return nil, err
	}
	defer tasks.Close()
	for tasks.Next() {
		var id, title, schedule, next string
		if err := tasks.Scan(&id, &title, &schedule, &next); err != nil {
			return nil, err
		}
		at, err := time.Parse(layout, next)
		if err != nil {
			return nil, err
		}
		var parsed cron.Schedule
		if schedule != "" {
			if parsed, err = cron.ParseStandard(schedule); err != nil {
				return nil, err
			}
			if at.Before(fromDate) {
				at = parsed.Next(fromDate.Add(-time.Second))
			}
		}
		// burnedout: assumed 500 occurrence cap per task
		for n := 0; n < 500 && at.Before(end); n++ {
			if inRange(at) {
				out = append(out, scheduledCalendarItem{TaskID: id, Title: title, At: at.UTC().Format(layout)})
			}
			if parsed == nil {
				break
			}
			at = parsed.Next(at.In(time.Local))
		}
	}
	if err := tasks.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out, nil
}

func (s *Server) calendar(w http.ResponseWriter, r *http.Request) {
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if !validDate(from) || !validDate(to) || from > to {
		errw(w, http.StatusBadRequest, "from and to must be valid YYYY-MM-DD dates, with from not after to")
		return
	}
	boardID := strings.TrimSpace(r.URL.Query().Get("board_id"))
	query := calendarItemSelect + ` LEFT JOIN board b ON b.id=c.board_id WHERE c.deleted_at IS NULL AND b.deleted_at IS NULL AND substr(c.starts_at,1,10)<=? AND COALESCE(substr(c.ends_at,1,10),substr(c.starts_at,1,10))>=?`
	args := []any{to, from}
	if boardID != "" {
		query += ` AND (c.board_id=? OR c.ticket_id IN (SELECT id FROM ticket WHERE board_id=?))`
		args = append(args, boardID, boardID)
	}
	// Timed item dates are interpreted in UTC for inclusive range matching.
	query += ` ORDER BY c.starts_at,c.rowid`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := []calendarItem{}
	for rows.Next() {
		x, err := scanCalendarItem(rows)
		if err != nil {
			rows.Close()
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err = rows.Close(); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	ticketQuery := `SELECT t.id,t.title,t.description,t.number,t.code,t.created_at,t.updated_at,t.due_date,t.status_updated_at,t.status_id,t.board_id,t.ticket_type_id,t.reporter_id,t.assignee_id FROM ticket t JOIN board b ON b.id=t.board_id WHERE substr(t.due_date,1,10)>=? AND substr(t.due_date,1,10)<=? AND b.deleted_at IS NULL`
	ticketArgs := []any{from, to}
	if boardID != "" {
		ticketQuery += ` AND t.board_id=?`
		ticketArgs = append(ticketArgs, boardID)
	}
	ticketQuery += ` ORDER BY t.due_date,t.number`
	ticketRows, err := s.db.Query(ticketQuery, ticketArgs...)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	tickets := []ticket{}
	for ticketRows.Next() {
		x, err := scanTicket(ticketRows)
		if err != nil {
			ticketRows.Close()
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		tickets = append(tickets, x)
	}
	if err = ticketRows.Err(); err != nil {
		ticketRows.Close()
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err = ticketRows.Close(); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	scheduled := []scheduledCalendarItem{}
	if boardID == "" {
		scheduled, err = s.scheduledCalendar(from, to)
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	write(w, http.StatusOK, map[string]any{"items": items, "tickets": tickets, "scheduled": scheduled})
}

type calendarItemFields struct {
	kind, title, description, startsAt string
	endsAt, boardID, ticketID          *string
	allDay                             bool
}

func calendarFields(m map[string]json.RawMessage, current *calendarItemFields) (calendarItemFields, error) {
	f := calendarItemFields{}
	if current != nil {
		f = *current
	}
	allowed := map[string]bool{"kind": true, "title": true, "description": true, "starts_at": true, "ends_at": true, "all_day": true, "board_id": true, "ticket_id": true}
	for key, raw := range m {
		if !allowed[key] {
			return f, errors.New("invalid " + key)
		}
		switch key {
		case "kind", "title", "description", "starts_at":
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return f, errors.New("invalid " + key)
			}
			switch key {
			case "kind":
				f.kind = value
			case "title":
				f.title = value
			case "description":
				f.description = value
			case "starts_at":
				f.startsAt = value
			}
		case "ends_at", "board_id", "ticket_id":
			if string(raw) == "null" {
				if key == "ends_at" {
					f.endsAt = nil
				} else if key == "board_id" {
					f.boardID = nil
				} else {
					f.ticketID = nil
				}
				continue
			}
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return f, errors.New("invalid " + key)
			}
			if value == "" {
				return f, errors.New("invalid " + key)
			}
			p := &value
			if key == "ends_at" {
				f.endsAt = p
			} else if key == "board_id" {
				f.boardID = p
			} else {
				f.ticketID = p
			}
		case "all_day":
			if json.Unmarshal(raw, &f.allDay) != nil {
				return f, errors.New("invalid all_day")
			}
		}
	}
	if f.kind != "event" && f.kind != "reminder" {
		return f, errors.New("kind must be event or reminder")
	}
	if !required(f.title) {
		return f, errors.New("title required")
	}
	starts, startTime, err := parseCalendarTime(f.startsAt, f.allDay)
	if err != nil {
		return f, err
	}
	f.startsAt = starts
	if f.endsAt != nil {
		ends, endTime, err := parseCalendarTime(*f.endsAt, f.allDay)
		if err != nil {
			return f, err
		}
		if endTime.Before(startTime) {
			return f, errors.New("ends_at must not be before starts_at")
		}
		f.endsAt = &ends
	}
	return f, nil
}

func readCalendarFields(tx *sql.Tx, id string) (calendarItemFields, error) {
	var f calendarItemFields
	var ends, boardID, ticketID sql.NullString
	var allDay int
	err := tx.QueryRow(`SELECT kind,title,description,starts_at,ends_at,all_day,board_id,ticket_id FROM calendar_item WHERE id=? AND deleted_at IS NULL`, id).Scan(&f.kind, &f.title, &f.description, &f.startsAt, &ends, &allDay, &boardID, &ticketID)
	if ends.Valid {
		f.endsAt = &ends.String
	}
	if boardID.Valid {
		f.boardID = &boardID.String
	}
	if ticketID.Valid {
		f.ticketID = &ticketID.String
	}
	f.allDay = allDay != 0
	return f, err
}

func calendarReferences(tx *sql.Tx, f *calendarItemFields) error {
	if f.ticketID != nil {
		var ticketBoard string
		if err := tx.QueryRow(`SELECT board_id FROM ticket WHERE id=?`, *f.ticketID).Scan(&ticketBoard); err != nil {
			if err == sql.ErrNoRows {
				return errors.New("ticket not found")
			}
			return err
		}
		if f.boardID == nil {
			f.boardID = &ticketBoard
		}
	}
	if f.boardID != nil {
		var id string
		if err := tx.QueryRow(`SELECT id FROM board WHERE id=?`, *f.boardID).Scan(&id); err != nil {
			if err == sql.ErrNoRows {
				return errors.New("board not found")
			}
			return err
		}
	}
	return nil
}

func (s *Server) createCalendarItem(w http.ResponseWriter, r *http.Request) {
	var m map[string]json.RawMessage
	if decode(r, &m) != nil {
		errw(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	f, err := calendarFields(m, nil)
	if err != nil {
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	id, created := uid(), now()
	var out calendarItem
	err = txrun(s.db, func(tx *sql.Tx) error {
		if err := calendarReferences(tx, &f); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO calendar_item(id,kind,title,description,starts_at,ends_at,all_day,board_id,ticket_id,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, f.kind, f.title, f.description, f.startsAt, f.endsAt, f.allDay, f.boardID, f.ticketID, s.userID, created, created)
		if err != nil {
			return err
		}
		out, err = scanCalendarItem(tx.QueryRow(calendarItemSelect+` WHERE c.id=?`, id))
		return err
	})
	if err != nil {
		calendarWriteError(w, err)
		return
	}
	write(w, http.StatusCreated, out)
}

func (s *Server) updateCalendarItem(w http.ResponseWriter, r *http.Request) {
	var m map[string]json.RawMessage
	if decode(r, &m) != nil {
		errw(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	id := idParam(r, "id")
	var out calendarItem
	err := txrun(s.db, func(tx *sql.Tx) error {
		current, err := readCalendarFields(tx, id)
		if err != nil {
			return err
		}
		f, err := calendarFields(m, &current)
		if err != nil {
			return err
		}
		if err = calendarReferences(tx, &f); err != nil {
			return err
		}
		updates := []string{}
		args := []any{}
		fields := []struct {
			key, column string
			value       any
		}{{"kind", "kind", f.kind}, {"title", "title", f.title}, {"description", "description", f.description}, {"starts_at", "starts_at", f.startsAt}, {"ends_at", "ends_at", f.endsAt}, {"all_day", "all_day", f.allDay}, {"board_id", "board_id", f.boardID}, {"ticket_id", "ticket_id", f.ticketID}}
		for _, field := range fields {
			if _, ok := m[field.key]; ok {
				updates = append(updates, field.column+"=?")
				args = append(args, field.value)
			}
		}
		if _, ticketChanged := m["ticket_id"]; ticketChanged {
			if _, boardChanged := m["board_id"]; !boardChanged && current.boardID == nil && f.boardID != nil {
				updates = append(updates, "board_id=?")
				args = append(args, f.boardID)
			}
		}
		if len(updates) > 0 {
			args = append(args, now(), id)
			if _, err = tx.Exec(`UPDATE calendar_item SET `+strings.Join(updates, ",")+`,updated_at=? WHERE id=? AND deleted_at IS NULL`, args...); err != nil {
				return err
			}
		}
		out, err = scanCalendarItem(tx.QueryRow(calendarItemSelect+` WHERE c.id=? AND c.deleted_at IS NULL`, id))
		return err
	})
	if err != nil {
		calendarWriteError(w, err)
		return
	}
	write(w, http.StatusOK, out)
}

func (s *Server) deleteCalendarItem(w http.ResponseWriter, r *http.Request) {
	err := txrun(s.db, func(tx *sql.Tx) error {
		timestamp := now()
		result, err := tx.Exec(`UPDATE calendar_item SET deleted_at=?,updated_at=? WHERE id=? AND deleted_at IS NULL`, timestamp, timestamp, idParam(r, "id"))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err == sql.ErrNoRows {
		errw(w, http.StatusNotFound, "calendar item not found")
	} else if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
	} else {
		noContent(w)
	}
}

func calendarWriteError(w http.ResponseWriter, err error) {
	if err == sql.ErrNoRows {
		errw(w, http.StatusNotFound, "calendar item not found")
	} else if strings.Contains(err.Error(), "kind must") || strings.Contains(err.Error(), "title required") || strings.Contains(err.Error(), "YYYY-MM-DD") || strings.Contains(err.Error(), "RFC3339") || strings.Contains(err.Error(), "ends_at") || strings.Contains(err.Error(), "invalid ") || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		errw(w, http.StatusBadRequest, err.Error())
	} else {
		errw(w, http.StatusInternalServerError, err.Error())
	}
}
