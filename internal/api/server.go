package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	db         *sql.DB
	userID     string
	showHidden bool
}

func New(db *sql.DB, userID string, showHidden bool) http.Handler {
	s := &Server{db: db, userID: userID, showHidden: showHidden}
	m := http.NewServeMux()
	m.HandleFunc("GET /api/config", s.config)
	m.HandleFunc("GET /api/users", s.users)
	m.HandleFunc("GET /api/boards", s.boards)
	m.HandleFunc("POST /api/boards", s.createBoard)
	m.HandleFunc("GET /api/boards/{id}", s.board)
	m.HandleFunc("PUT /api/boards/{id}", s.updateBoard)
	m.HandleFunc("DELETE /api/boards/{id}", s.deleteBoard)
	m.HandleFunc("GET /api/boards/{id}/data", s.boardData)
	m.HandleFunc("POST /api/boards/{id}/statuses", s.createStatus)
	m.HandleFunc("PUT /api/statuses/{id}", s.updateStatus)
	m.HandleFunc("DELETE /api/statuses/{id}", s.deleteStatus)
	m.HandleFunc("PUT /api/boards/{id}/status-order", s.statusOrder)
	m.HandleFunc("POST /api/boards/{id}/ticket-types", s.createType)
	m.HandleFunc("PUT /api/ticket-types/{id}", s.updateType)
	m.HandleFunc("DELETE /api/ticket-types/{id}", s.deleteType)
	m.HandleFunc("POST /api/boards/{id}/tickets", s.createTicket)
	m.HandleFunc("GET /api/tickets/search", s.searchTickets)
	m.HandleFunc("GET /api/tickets/{idOrCode}", s.getTicket)
	m.HandleFunc("PUT /api/tickets/{id}", s.updateTicket)
	m.HandleFunc("GET /api/calendar", s.calendar)
	m.HandleFunc("POST /api/calendar-items", s.createCalendarItem)
	m.HandleFunc("PUT /api/calendar-items/{id}", s.updateCalendarItem)
	m.HandleFunc("DELETE /api/calendar-items/{id}", s.deleteCalendarItem)
	m.HandleFunc("DELETE /api/tickets/{id}", s.deleteTicket)
	m.HandleFunc("PUT /api/tickets/{id}/move", s.moveTicket)
	m.HandleFunc("GET /api/tickets/{id}/history", s.history)
	m.HandleFunc("GET /api/tickets/{id}/transitions", s.transitions)
	m.HandleFunc("GET /api/tickets/{id}/comments", s.comments)
	m.HandleFunc("POST /api/tickets/{id}/comments", s.addComment)
	m.HandleFunc("GET /api/boards/{id}/export", s.exportBoard)
	m.HandleFunc("GET /api/tickets/{id}/export", s.exportTicket)
	m.HandleFunc("POST /api/import", s.importBoard)
	return m
}

type board struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Short    string `json:"short_name"`
	Template string `json:"template"`
	Created  string `json:"created_at"`
	Updated  string `json:"updated_at"`
}
type status struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	BoardID       string `json:"board_id"`
	HideAfterDays int    `json:"hide_after_days"`
}

func statusFields(raw string) (map[string]json.RawMessage, int, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		return nil, 0, errors.New("invalid status additional_fields")
	}
	days := 1
	if value, ok := fields["hide_after_days"]; ok && json.Unmarshal(value, &days) != nil {
		return nil, 0, errors.New("invalid hide_after_days")
	}
	if days < 1 {
		return nil, 0, errors.New("hide_after_days must be at least 1")
	}
	return fields, days, nil
}

func setStatusHideAfterDays(fields map[string]json.RawMessage, days int) (string, error) {
	value, err := json.Marshal(days)
	if err != nil {
		return "", err
	}
	fields["hide_after_days"] = value
	encoded, err := json.Marshal(fields)
	return string(encoded), err
}

func scanStatus(row interface{ Scan(...any) error }) (status, error) {
	var st status
	var raw string
	if err := row.Scan(&st.ID, &st.Name, &st.Type, &st.BoardID, &raw); err != nil {
		return st, err
	}
	_, days, err := statusFields(raw)
	st.HideAfterDays = days
	return st, err
}

type ticketType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	BoardID string `json:"board_id"`
}
type ticket struct {
	ID, Title, Description                                                          string
	Number                                                                          int
	Code, Created, Updated                                                          string
	DueDate, StatusUpdated, StatusID, BoardID, TicketTypeID, ReporterID, AssigneeID *string
}

func (t ticket) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID            string  `json:"id"`
		Title         string  `json:"title"`
		Description   string  `json:"description"`
		Number        int     `json:"number"`
		Code          string  `json:"code"`
		Created       string  `json:"created_at"`
		Updated       string  `json:"updated_at"`
		DueDate       *string `json:"due_date"`
		StatusUpdated *string `json:"status_updated_at"`
		StatusID      *string `json:"status_id"`
		BoardID       *string `json:"board_id"`
		TicketTypeID  *string `json:"ticket_type_id"`
		ReporterID    *string `json:"reporter_id"`
		AssigneeID    *string `json:"assignee_id"`
	}{t.ID, t.Title, t.Description, t.Number, t.Code, t.Created, t.Updated, t.DueDate, t.StatusUpdated, t.StatusID, t.BoardID, t.TicketTypeID, t.ReporterID, t.AssigneeID})
}

type history struct {
	ID, Content, Created, UserID, UserName string
	UserIsAI                               bool
}
type transition struct {
	ID                                               string
	FromID, FromName, ToID, ToName, UserID, UserName *string
	Moved                                            string
	UserIsAI                                         bool
}
type comment struct {
	ID, Content, Created, Updated, UserID, UserName string
	UserIsAI                                        bool
}
type user struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	LastName *string `json:"last_name"`
	Email    string  `json:"email"`
	IsAI     bool    `json:"is_ai"`
}

func (h history) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID       string `json:"id"`
		Content  string `json:"content"`
		Created  string `json:"created_at"`
		UserID   string `json:"user_id"`
		UserName string `json:"user_name"`
		UserIsAI bool   `json:"user_is_ai"`
	}{h.ID, h.Content, h.Created, h.UserID, h.UserName, h.UserIsAI})
}
func (c comment) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID       string `json:"id"`
		Content  string `json:"content"`
		Created  string `json:"created_at"`
		Updated  string `json:"updated_at"`
		UserID   string `json:"user_id"`
		UserName string `json:"user_name"`
		UserIsAI bool   `json:"user_is_ai"`
	}{c.ID, c.Content, c.Created, c.Updated, c.UserID, c.UserName, c.UserIsAI})
}
func (t transition) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID       string  `json:"id"`
		FromID   *string `json:"from_status_id"`
		FromName *string `json:"from_status_name"`
		ToID     *string `json:"to_status_id"`
		ToName   *string `json:"to_status_name"`
		UserID   *string `json:"user_id"`
		UserName *string `json:"user_name"`
		UserIsAI bool    `json:"user_is_ai"`
		Moved    string  `json:"moved_at"`
	}{t.ID, t.FromID, t.FromName, t.ToID, t.ToName, t.UserID, t.UserName, t.UserIsAI, t.Moved})
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }
func uid() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return hex.EncodeToString(b[:])
}
func write(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func errw(w http.ResponseWriter, code int, msg string) {
	write(w, code, map[string]string{"error": msg})
}
func noContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("invalid JSON")
	}
	return nil
}
func idParam(r *http.Request, n string) string { return r.PathValue(n) }
func txrun(db *sql.DB, f func(*sql.Tx) error) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	if e = f(tx); e != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("%w; rollback: %v", e, rollbackErr)
		}
		return e
	}
	return tx.Commit()
}
func required(s string) bool    { return strings.TrimSpace(s) != "" }
func shortName(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
func validShort(s string) bool {
	if len(s) < 1 || len(s) > 10 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func parseList(s string) ([]string, error) {
	var x []string
	if e := json.Unmarshal([]byte(s), &x); e != nil {
		return nil, e
	}
	return x, nil
}
func listJSON(x []string) string { b, _ := json.Marshal(x); return string(b) }
func contains(x []string, id string) bool {
	for _, v := range x {
		if v == id {
			return true
		}
	}
	return false
}
func remove(x []string, id string) []string {
	r := x[:0]
	for _, v := range x {
		if v != id {
			r = append(r, v)
		}
	}
	return r
}
