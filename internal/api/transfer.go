package api

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

var htmlTag = regexp.MustCompile(`<[^>]*>`)
var exportHeaders = []string{"Code", "Title", "Status", "Type", "Created", "Updated", "Status updated", "Due date", "Description"}

type transferTicket struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Number        int     `json:"number"`
	Code          string  `json:"code"`
	Created       string  `json:"created_at"`
	Updated       string  `json:"updated_at"`
	DueDate       *string `json:"due_date"`
	StatusUpdated *string `json:"status_updated_at"`
	StatusID      string  `json:"status_id"`
	BoardID       string  `json:"board_id"`
	TicketTypeID  *string `json:"ticket_type_id"`
	ReporterID    *string `json:"reporter_id"`
	AssigneeID    *string `json:"assignee_id"`
}
type transferStatus struct {
	status
	TicketIDs []string `json:"ticket_ids"`
}
type transferType = ticketType
type transferUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LastName string `json:"last_name"`
	Email    string `json:"email"`
	IsAI     bool   `json:"is_ai,omitempty"`
}
type transferHistory struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Created  string `json:"created_at"`
	TicketID string `json:"ticket_id"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
	IsAI     bool   `json:"-"`
}
type transferTransition struct {
	ID             string  `json:"id"`
	TicketID       string  `json:"ticket_id"`
	BoardID        string  `json:"board_id"`
	FromStatusID   *string `json:"from_status_id"`
	ToStatusID     *string `json:"to_status_id"`
	UserID         *string `json:"user_id"`
	FromStatusName *string `json:"from_status_name"`
	ToStatusName   *string `json:"to_status_name"`
	UserName       *string `json:"user_name"`
	Moved          string  `json:"moved_at"`
	IsAI           bool    `json:"-"`
}
type transferComment struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Created  string `json:"created_at"`
	Updated  string `json:"updated_at"`
	TicketID string `json:"ticket_id"`
	UserID   string `json:"user_id"`
}
type transferCalendarItem struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	StartsAt    string  `json:"starts_at"`
	EndsAt      *string `json:"ends_at"`
	AllDay      bool    `json:"all_day"`
	TicketID    *string `json:"ticket_id"`
	CreatedBy   string  `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	NotifiedAt  *string `json:"notified_at"`
}
type boardBackup struct {
	Format        string                 `json:"format"`
	Version       int                    `json:"version"`
	ExportedAt    string                 `json:"exported_at"`
	Board         board                  `json:"board"`
	Statuses      []transferStatus       `json:"statuses"`
	TicketTypes   []transferType         `json:"ticket_types"`
	Tickets       []transferTicket       `json:"tickets"`
	History       []transferHistory      `json:"history"`
	Transitions   []transferTransition   `json:"transitions"`
	Comments      []transferComment      `json:"comments"`
	CalendarItems []transferCalendarItem `json:"calendar_items,omitempty"`
	Users         []transferUser         `json:"users"`
}

func plain(s string) string { return html.UnescapeString(htmlTag.ReplaceAllString(s, "")) }
func safeCell(s string) string {
	if len(s) > 0 && strings.ContainsRune("=+-@", rune(s[0])) {
		return "'" + s
	}
	return s
}
func (s *Server) backup(id string) (boardBackup, error) {
	var b boardBackup
	var e error
	b.Board, e = s.boardBy(s.db, id)
	if e != nil {
		return b, e
	}
	var raw string
	if e = s.db.QueryRow(`SELECT status_ids FROM status_order WHERE board_id=?`, id).Scan(&raw); e != nil {
		return b, e
	}
	statusIDs, e := parseList(raw)
	if e != nil {
		return b, e
	}
	if e = s.db.QueryRow(`SELECT ticket_type_ids FROM ticket_type_order WHERE board_id=?`, id).Scan(&raw); e != nil {
		return b, e
	}
	typeIDs, e := parseList(raw)
	if e != nil {
		return b, e
	}
	for _, sid := range statusIDs {
		var x transferStatus
		if e = s.db.QueryRow(`SELECT id,name,type,board_id FROM status WHERE id=?`, sid).Scan(&x.ID, &x.Name, &x.Type, &x.BoardID); e != nil {
			return b, e
		}
		var q string
		if e = s.db.QueryRow(`SELECT ticket_ids FROM ticket_order WHERE status_id=?`, sid).Scan(&q); e != nil {
			return b, e
		}
		x.TicketIDs, e = parseList(q)
		if e != nil {
			return b, e
		}
		b.Statuses = append(b.Statuses, x)
	}
	for _, tid := range typeIDs {
		var x transferType
		if e = s.db.QueryRow(`SELECT id,name,color,board_id FROM ticket_type WHERE id=?`, tid).Scan(&x.ID, &x.Name, &x.Color, &x.BoardID); e != nil {
			return b, e
		}
		b.TicketTypes = append(b.TicketTypes, x)
	}
	rows, e := s.db.Query(`SELECT id,title,description,number,code,created_at,updated_at,due_date,status_updated_at,status_id,board_id,ticket_type_id,reporter_id,assignee_id FROM ticket WHERE board_id=? ORDER BY number`, id)
	if e != nil {
		return b, e
	}
	defer rows.Close()
	for rows.Next() {
		t, e := scanTransferTicket(rows)
		if e != nil {
			return b, e
		}
		b.Tickets = append(b.Tickets, t)
	}
	if e = rows.Err(); e != nil {
		return b, e
	}
	rows, e = s.db.Query(`SELECT h.id,h.content,h.created_at,h.ticket_id,h.user_id,u.name,u.is_ai FROM ticket_history h JOIN "user" u ON u.id=h.user_id WHERE h.ticket_id IN (SELECT id FROM ticket WHERE board_id=?) ORDER BY h.created_at,h.rowid`, id)
	if e != nil {
		return b, e
	}
	for rows.Next() {
		var x transferHistory
		if e = rows.Scan(&x.ID, &x.Content, &x.Created, &x.TicketID, &x.UserID, &x.UserName, &x.IsAI); e != nil {
			rows.Close()
			return b, e
		}
		b.History = append(b.History, x)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return b, e
	}
	rows, e = s.db.Query(`SELECT x.id,x.ticket_id,x.board_id,x.from_status_id,fs.name,x.to_status_id,ts.name,x.user_id,u.name,u.is_ai,x.moved_at FROM ticket_status_transition x LEFT JOIN status fs ON fs.id=x.from_status_id JOIN status ts ON ts.id=x.to_status_id JOIN "user" u ON u.id=x.user_id WHERE x.board_id=? ORDER BY x.moved_at,x.rowid`, id)
	if e != nil {
		return b, e
	}
	for rows.Next() {
		var x transferTransition
		var f, fn, t, tn, u, un sql.NullString
		if e = rows.Scan(&x.ID, &x.TicketID, &x.BoardID, &f, &fn, &t, &tn, &u, &un, &x.IsAI, &x.Moved); e != nil {
			rows.Close()
			return b, e
		}
		if f.Valid {
			x.FromStatusID = &f.String
		}
		if fn.Valid {
			x.FromStatusName = &fn.String
		}
		if t.Valid {
			x.ToStatusID = &t.String
		}
		if tn.Valid {
			x.ToStatusName = &tn.String
		}
		if u.Valid {
			x.UserID = &u.String
		}
		if un.Valid {
			x.UserName = &un.String
		}
		b.Transitions = append(b.Transitions, x)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return b, e
	}
	rows, e = s.db.Query(`SELECT id,content,created_at,updated_at,ticket_id,user_id FROM ticket_comment WHERE ticket_id IN (SELECT id FROM ticket WHERE board_id=?) ORDER BY created_at,rowid`, id)
	if e != nil {
		return b, e
	}
	for rows.Next() {
		var x transferComment
		if e = rows.Scan(&x.ID, &x.Content, &x.Created, &x.Updated, &x.TicketID, &x.UserID); e != nil {
			rows.Close()
			return b, e
		}
		b.Comments = append(b.Comments, x)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return b, e
	}
	rows, e = s.db.Query(calendarItemSelect+` WHERE c.deleted_at IS NULL AND (c.board_id=? OR c.ticket_id IN (SELECT id FROM ticket WHERE board_id=?)) ORDER BY c.starts_at,c.rowid`, id, id)
	if e != nil {
		return b, e
	}
	for rows.Next() {
		item, err := scanCalendarItem(rows)
		if err != nil {
			rows.Close()
			return b, err
		}
		b.CalendarItems = append(b.CalendarItems, transferCalendarItem{
			ID: item.ID, Kind: item.Kind, Title: item.Title, Description: item.Description,
			StartsAt: item.StartsAt, EndsAt: item.EndsAt, AllDay: item.AllDay,
			TicketID: item.TicketID, CreatedBy: item.CreatedBy, CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt, NotifiedAt: item.NotifiedAt,
		})
	}
	if e = rows.Close(); e != nil {
		return b, e
	}
	if e = rows.Err(); e != nil {
		return b, e
	}
	refs := map[string]bool{}
	for _, t := range b.Tickets {
		if t.ReporterID != nil {
			refs[*t.ReporterID] = true
		}
		if t.AssigneeID != nil {
			refs[*t.AssigneeID] = true
		}
	}
	for _, h := range b.History {
		refs[h.UserID] = true
	}
	for _, x := range b.Transitions {
		if x.UserID != nil {
			refs[*x.UserID] = true
		}
	}
	for _, x := range b.Comments {
		refs[x.UserID] = true
	}
	for _, x := range b.CalendarItems {
		refs[x.CreatedBy] = true
	}
	for id := range refs {
		var u transferUser
		if e = s.db.QueryRow(`SELECT id,name,COALESCE(last_name,''),email,is_ai FROM "user" WHERE id=?`, id).Scan(&u.ID, &u.Name, &u.LastName, &u.Email, &u.IsAI); e != nil {
			return b, e
		}
		b.Users = append(b.Users, u)
	}
	sort.Slice(b.Users, func(i, j int) bool { return b.Users[i].ID < b.Users[j].ID })
	b.Format = "my-tickets-board"
	b.Version = 1
	b.ExportedAt = now()
	return b, nil
}
func scanTransferTicket(r interface{ Scan(...any) error }) (transferTicket, error) {
	t, e := scanTicket(r)
	if e != nil {
		return transferTicket{}, e
	}
	return transferTicket{
		ID: t.ID, Title: t.Title, Description: t.Description, Number: t.Number,
		Code: t.Code, Created: t.Created, Updated: t.Updated, DueDate: t.DueDate,
		StatusUpdated: t.StatusUpdated, StatusID: *t.StatusID, BoardID: *t.BoardID,
		TicketTypeID: t.TicketTypeID, ReporterID: t.ReporterID, AssigneeID: t.AssigneeID,
	}, nil
}
func rowsForExport(b boardBackup) [][]string {
	names := map[string]string{}
	for _, x := range b.Statuses {
		names[x.ID] = x.Name
	}
	types := map[string]string{}
	for _, x := range b.TicketTypes {
		types[x.ID] = x.Name
	}
	out := [][]string{exportHeaders}
	for _, t := range b.Tickets {
		typ := ""
		if t.TicketTypeID != nil {
			typ = types[*t.TicketTypeID]
		}
		st := names[t.StatusID]
		due := ""
		if t.DueDate != nil {
			due = *t.DueDate
		}
		su := ""
		if t.StatusUpdated != nil {
			su = *t.StatusUpdated
		}
		out = append(out, []string{t.Code, t.Title, st, typ, t.Created, t.Updated, su, due, plain(t.Description)})
	}
	return out
}
func (s *Server) exportBoard(w http.ResponseWriter, r *http.Request) {
	b, e := s.backup(idParam(r, "id"))
	if e == sql.ErrNoRows {
		errw(w, 404, "board not found")
		return
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	f := r.URL.Query().Get("format")
	ext := f
	if f != "json" && f != "csv" && f != "xlsx" {
		errw(w, 400, "format must be xlsx, csv, or json")
		return
	}
	name := fmt.Sprintf("%s-%s.%s", b.Board.Short, now()[:10], ext)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if f == "json" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(b)
		return
	}
	data := rowsForExport(b)
	if f == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Write([]byte{0xEF, 0xBB, 0xBF})
		cw := csv.NewWriter(w)
		for _, row := range data {
			for i := range row {
				row[i] = safeCell(row[i])
			}
			if e = cw.Write(row); e != nil {
				return
			}
		}
		cw.Flush()
		return
	}
	x := excelize.NewFile()
	defer x.Close()
	for i, row := range data {
		for j, v := range row {
			if e = x.SetCellValue("Sheet1", fmt.Sprintf("%c%d", 'A'+j, i+1), safeCell(v)); e != nil {
				errw(w, 500, e.Error())
				return
			}
		}
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	e = x.Write(w)
	if e != nil {
		return
	}
}
func (s *Server) exportTicket(w http.ResponseWriter, r *http.Request) {
	t, e := s.ticketExport(idParam(r, "id"))
	if e == sql.ErrNoRows {
		errw(w, 404, "ticket not found")
		return
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	f := r.URL.Query().Get("format")
	if f == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="`+t.Ticket.Code+`.json"`)
		json.NewEncoder(w).Encode(t)
		return
	}
	if f != "md" {
		errw(w, 400, "format must be json or md")
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+t.Ticket.Code+`.md"`)
	fmt.Fprintf(w, "# %s (%s)\n\n- Status: %s\n- Type: %s\n- Created: %s\n- Updated: %s\n- Due date: %s\n\n%s\n\n## History\n", t.Ticket.Title, t.Ticket.Code, t.StatusName, t.TypeName, t.Ticket.Created, t.Ticket.Updated, ptrText(t.Ticket.DueDate), plain(t.Ticket.Description))
	for _, h := range t.History {
		name := h.UserName
		if h.IsAI {
			name += " (AI)"
		}
		fmt.Fprintf(w, "- %s: %s (%s)\n", h.Created, h.Content, name)
	}
	fmt.Fprint(w, "\n## Status transitions\n")
	for _, x := range t.Transitions {
		name := ptrText(x.UserName)
		if x.IsAI {
			name += " (AI)"
		}
		if x.FromStatusName == nil {
			fmt.Fprintf(w, "- %s: Created in %s (%s)\n", x.Moved, ptrText(x.ToStatusName), name)
		} else {
			fmt.Fprintf(w, "- %s: %s -> %s (%s)\n", x.Moved, ptrText(x.FromStatusName), ptrText(x.ToStatusName), name)
		}
	}
}

type ticketExport struct {
	Ticket      transferTicket       `json:"ticket"`
	StatusName  string               `json:"status_name"`
	TypeName    string               `json:"type_name"`
	History     []transferHistory    `json:"history"`
	Transitions []transferTransition `json:"transitions"`
	Comments    []transferComment    `json:"comments"`
}

func ptrText(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func (s *Server) ticketExport(id string) (ticketExport, error) {
	var o ticketExport
	var t ticket
	var e error
	t, e = s.ticketBy(s.db, id)
	if e != nil {
		return o, e
	}
	o.Ticket = transferTicket{ID: t.ID, Title: t.Title, Description: t.Description, Number: t.Number, Code: t.Code, Created: t.Created, Updated: t.Updated, DueDate: t.DueDate, StatusUpdated: t.StatusUpdated, StatusID: *t.StatusID, BoardID: *t.BoardID, TicketTypeID: t.TicketTypeID, ReporterID: t.ReporterID, AssigneeID: t.AssigneeID}
	if e = s.db.QueryRow(`SELECT name FROM status WHERE id=?`, *t.StatusID).Scan(&o.StatusName); e != nil {
		return o, e
	}
	if t.TicketTypeID != nil {
		if e = s.db.QueryRow(`SELECT name FROM ticket_type WHERE id=?`, *t.TicketTypeID).Scan(&o.TypeName); e != nil {
			return o, e
		}
	}
	var b boardBackup
	b, e = s.backup(*t.BoardID)
	if e != nil {
		return o, e
	}
	for _, h := range b.History {
		if h.TicketID == t.ID {
			o.History = append(o.History, h)
		}
	}
	for _, x := range b.Transitions {
		if x.TicketID == t.ID {
			o.Transitions = append(o.Transitions, x)
		}
	}
	for _, x := range b.Comments {
		if x.TicketID == t.ID {
			o.Comments = append(o.Comments, x)
		}
	}
	return o, nil
}

func (s *Server) importBoard(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	var b boardBackup
	if json.NewDecoder(r.Body).Decode(&b) != nil || b.Format != "my-tickets-board" || b.Version != 1 {
		errw(w, 400, "invalid board backup")
		return
	}
	statusMap, typeMap, ticketMap, userMap := map[string]transferStatus{}, map[string]transferType{}, map[string]transferTicket{}, map[string]transferUser{}
	if !required(b.Board.Name) || !validShort(shortName(b.Board.Short)) {
		errw(w, 400, "invalid board")
		return
	}
	for _, x := range b.Statuses {
		if x.BoardID != b.Board.ID || statusMap[x.ID].ID != "" {
			errw(w, 400, "invalid status reference")
			return
		}
		statusMap[x.ID] = x
	}
	for _, x := range b.TicketTypes {
		if x.BoardID != b.Board.ID || typeMap[x.ID].ID != "" {
			errw(w, 400, "invalid ticket type reference")
			return
		}
		typeMap[x.ID] = x
	}
	for _, x := range b.Users {
		if userMap[x.ID].ID != "" {
			errw(w, 400, "invalid user reference")
			return
		}
		userMap[x.ID] = x
	}
	for _, x := range b.Tickets {
		if x.BoardID != b.Board.ID || statusMap[x.StatusID].ID == "" || (x.TicketTypeID != nil && typeMap[*x.TicketTypeID].ID == "") || ticketMap[x.ID].ID != "" {
			errw(w, 400, "invalid ticket reference")
			return
		}
		ticketMap[x.ID] = x
	}
	for _, x := range b.Statuses {
		for _, id := range x.TicketIDs {
			if ticketMap[id].ID == "" || ticketMap[id].StatusID != x.ID {
				errw(w, 400, "invalid ticket order reference")
				return
			}
		}
	}
	for _, x := range b.History {
		if ticketMap[x.TicketID].ID == "" || userMap[x.UserID].ID == "" {
			errw(w, 400, "invalid history reference")
			return
		}
	}
	for _, x := range b.Comments {
		if ticketMap[x.TicketID].ID == "" || userMap[x.UserID].ID == "" {
			errw(w, 400, "invalid comment reference")
			return
		}
	}
	for _, x := range b.Transitions {
		if ticketMap[x.TicketID].ID == "" || userMap[ptrText(x.UserID)].ID == "" || statusMap[ptrText(x.ToStatusID)].ID == "" || (x.FromStatusID != nil && statusMap[*x.FromStatusID].ID == "") {
			errw(w, 400, "invalid transition reference")
			return
		}
	}
	for _, x := range b.CalendarItems {
		if (x.Kind != "event" && x.Kind != "reminder") || !required(x.Title) || (x.TicketID != nil && ticketMap[*x.TicketID].ID == "") || userMap[x.CreatedBy].ID == "" {
			errw(w, 400, "invalid calendar item reference")
			return
		}
	}
	short := shortName(b.Board.Short)
	var out board
	e := txrun(s.db, func(tx *sql.Tx) error {
		base := short
		for n := 2; ; n++ {
			var exists int
			if e := tx.QueryRow(`SELECT COUNT(*) FROM board WHERE short_name=? AND deleted_at IS NULL`, short).Scan(&exists); e != nil {
				return e
			}
			if exists == 0 {
				break
			}
			suffix := strconv.Itoa(n)
			if len(base)+len(suffix) > 10 {
				base = base[:10-len(suffix)]
			}
			short = base + suffix
		}
		bid := uid()
		if _, e := tx.Exec(`INSERT INTO board(id,name,short_name,template,created_at,updated_at) VALUES(?,?,?,?,?,?)`, bid, b.Board.Name, short, b.Board.Template, b.Board.Created, b.Board.Updated); e != nil {
			return e
		}
		if _, e := tx.Exec(`INSERT INTO board_counter(last_number,board_id) VALUES(?,?)`, 0, bid); e != nil {
			return e
		}
		if _, e := tx.Exec(`INSERT INTO status_order(id,status_ids,board_id) VALUES(?,?,?)`, uid(), "[]", bid); e != nil {
			return e
		}
		if _, e := tx.Exec(`INSERT INTO ticket_type_order(id,ticket_type_ids,board_id) VALUES(?,?,?)`, uid(), "[]", bid); e != nil {
			return e
		}
		users := map[string]string{}
		for _, u := range b.Users {
			var id string
			if e := tx.QueryRow(`SELECT id FROM "user" WHERE id=? AND deleted_at IS NULL`, u.ID).Scan(&id); e == sql.ErrNoRows {
				e = tx.QueryRow(`SELECT id FROM "user" WHERE email=? AND deleted_at IS NULL`, u.Email).Scan(&id)
				if e == sql.ErrNoRows {
					id = uid()
					if _, e = tx.Exec(`INSERT INTO "user"(id,name,last_name,email,is_ai) VALUES(?,?,?,?,?)`, id, u.Name, u.LastName, u.Email, u.IsAI); e != nil {
						return e
					}
				} else if e != nil {
					return e
				}
			} else if e != nil {
				return e
			}
			users[u.ID] = id
		}
		sm, tm := map[string]string{}, map[string]string{}
		so := []string{}
		for _, x := range b.Statuses {
			id := uid()
			sm[x.ID] = id
			so = append(so, id)
			if _, e := tx.Exec(`INSERT INTO status(id,name,type,board_id,created_at,updated_at) VALUES(?,?,?,?,?,?)`, id, x.Name, x.Type, bid, b.Board.Created, b.Board.Updated); e != nil {
				return e
			}
			if _, e := tx.Exec(`INSERT INTO ticket_order(id,ticket_ids,status_id) VALUES(?,?,?)`, uid(), "[]", id); e != nil {
				return e
			}
		}
		for _, x := range b.TicketTypes {
			id := uid()
			tm[x.ID] = id
			if _, e := tx.Exec(`INSERT INTO ticket_type(id,name,color,board_id,created_at,updated_at) VALUES(?,?,?,?,?,?)`, id, x.Name, x.Color, bid, b.Board.Created, b.Board.Updated); e != nil {
				return e
			}
		}
		orders := map[string][]string{}
		ticketIDs := map[string]string{}
		max := 0
		for _, x := range b.Tickets {
			id := uid()
			ticketIDs[x.ID] = id
			num := x.Number
			if num > max {
				max = num
			}
			code := fmt.Sprintf("%s-%d", short, num)
			var ty, rep, ass any
			if x.TicketTypeID != nil {
				ty = tm[*x.TicketTypeID]
			}
			if x.ReporterID != nil {
				rep = users[*x.ReporterID]
			}
			if x.AssigneeID != nil {
				ass = users[*x.AssigneeID]
			}
			if _, e := tx.Exec(`INSERT INTO ticket(id,title,description,number,code,created_at,updated_at,due_date,status_updated_at,status_id,board_id,ticket_type_id,reporter_id,assignee_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, x.Title, x.Description, num, code, x.Created, x.Updated, x.DueDate, x.StatusUpdated, sm[x.StatusID], bid, ty, rep, ass); e != nil {
				return e
			}
			orders[x.StatusID] = append(orders[x.StatusID], id)
		}
		for _, x := range b.Statuses {
			ids := []string{}
			for _, old := range x.TicketIDs {
				ids = append(ids, ticketIDs[old])
			}
			if len(ids) == 0 {
				ids = orders[x.ID]
			}
			if _, e := tx.Exec(`UPDATE ticket_order SET ticket_ids=? WHERE status_id=?`, listJSON(ids), sm[x.ID]); e != nil {
				return e
			}
		}
		for _, x := range b.History {
			if _, e := tx.Exec(`INSERT INTO ticket_history(id,content,created_at,ticket_id,user_id) VALUES(?,?,?,?,?)`, uid(), x.Content, x.Created, ticketIDs[x.TicketID], users[x.UserID]); e != nil {
				return e
			}
		}
		for _, x := range b.Transitions {
			var f any
			if x.FromStatusID != nil {
				f = sm[*x.FromStatusID]
			}
			if _, e := tx.Exec(`INSERT INTO ticket_status_transition(id,ticket_id,board_id,from_status_id,to_status_id,user_id,moved_at) VALUES(?,?,?,?,?,?,?)`, uid(), ticketIDs[x.TicketID], bid, f, sm[ptrText(x.ToStatusID)], users[ptrText(x.UserID)], x.Moved); e != nil {
				return e
			}
		}
		for _, x := range b.Comments {
			if _, e := tx.Exec(`INSERT INTO ticket_comment(id,content,created_at,updated_at,ticket_id,user_id) VALUES(?,?,?,?,?,?)`, uid(), x.Content, x.Created, x.Updated, ticketIDs[x.TicketID], users[x.UserID]); e != nil {
				return e
			}
		}
		for _, x := range b.CalendarItems {
			var ticketID any
			if x.TicketID != nil {
				ticketID = ticketIDs[*x.TicketID]
			}
			if _, e := tx.Exec(`INSERT INTO calendar_item(id,kind,title,description,starts_at,ends_at,all_day,board_id,ticket_id,created_by,created_at,updated_at,notified_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, uid(), x.Kind, x.Title, x.Description, x.StartsAt, x.EndsAt, x.AllDay, bid, ticketID, users[x.CreatedBy], x.CreatedAt, x.UpdatedAt, x.NotifiedAt); e != nil {
				return e
			}
		}
		if _, e := tx.Exec(`UPDATE status_order SET status_ids=? WHERE board_id=?`, listJSON(so), bid); e != nil {
			return e
		}
		to := []string{}
		for _, x := range b.TicketTypes {
			to = append(to, tm[x.ID])
		}
		if _, e := tx.Exec(`UPDATE ticket_type_order SET ticket_type_ids=? WHERE board_id=?`, listJSON(to), bid); e != nil {
			return e
		}
		if _, e := tx.Exec(`UPDATE board_counter SET last_number=? WHERE board_id=?`, max, bid); e != nil {
			return e
		}
		out = board{bid, b.Board.Name, short, b.Board.Template, b.Board.Created, b.Board.Updated}
		return nil
	})
	if e != nil {
		log.Printf("import failed: %v", e)
		errw(w, 500, "import failed")
		return
	}
	write(w, 201, out)
}
