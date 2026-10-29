package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type dashboardDay struct {
	Date      string `json:"date"`
	Done      int    `json:"done"`
	Cancelled int    `json:"cancelled"`
}

type dashboardBoard struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Created   int    `json:"created"`
	Done      int    `json:"done"`
	Cancelled int    `json:"cancelled"`
}

func (s *Server) eachRow(query string, args []any, scan func(*sql.Rows) error) error {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// dashboard counts moves into Done-type (temporary) columns per local day; columns named "Cancel…" count as cancelled.
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 || days > 365 {
		errw(w, http.StatusBadRequest, "days must be a number from 1 to 365")
		return
	}
	filter, boardArgs := "", []any{}
	if boardID := strings.TrimSpace(r.URL.Query().Get("board_id")); boardID != "" {
		filter, boardArgs = ` AND b.id=?`, []any{boardID}
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day()-days+1, 0, 0, 0, 0, time.Local)
	sinceArgs := append([]any{start.UTC().Format("2006-01-02T15:04:05.000Z")}, boardArgs...)
	out := struct {
		Days      []dashboardDay   `json:"days"`
		Created   int              `json:"created"`
		Done      int              `json:"done"`
		Cancelled int              `json:"cancelled"`
		Boards    []dashboardBoard `json:"boards"`
		Overdue   []ticket         `json:"overdue"`
		DueSoon   []ticket         `json:"due_soon"`
	}{Days: make([]dashboardDay, days), Boards: []dashboardBoard{}, Overdue: []ticket{}, DueSoon: []ticket{}}
	index := map[string]int{}
	for i := range out.Days {
		out.Days[i].Date = start.AddDate(0, 0, i).Format("2006-01-02")
		index[out.Days[i].Date] = i
	}
	boards := map[string]*dashboardBoard{}
	var order []string
	today := now.Format("2006-01-02")
	err = s.eachRow(`SELECT b.id,b.name FROM board b WHERE b.deleted_at IS NULL`+filter+` ORDER BY b.name`, boardArgs, func(rows *sql.Rows) error {
		var b dashboardBoard
		err := rows.Scan(&b.ID, &b.Name)
		boards[b.ID], order = &b, append(order, b.ID)
		return err
	})
	if err == nil {
		err = s.eachRow(`SELECT x.board_id,x.moved_at,LOWER(st.name) LIKE 'cancel%' FROM ticket_status_transition x JOIN status st ON st.id=x.to_status_id JOIN board b ON b.id=x.board_id WHERE st.type='temporary' AND x.moved_at>=? AND b.deleted_at IS NULL`+filter, sinceArgs, func(rows *sql.Rows) error {
			var board, moved string
			var cancelled bool
			if err := rows.Scan(&board, &moved, &cancelled); err != nil {
				return err
			}
			at, err := time.Parse(time.RFC3339, moved)
			i, ok := index[at.Local().Format("2006-01-02")]
			if err != nil || !ok || boards[board] == nil {
				return nil
			}
			if cancelled {
				out.Days[i].Cancelled++
				out.Cancelled++
				boards[board].Cancelled++
			} else {
				out.Days[i].Done++
				out.Done++
				boards[board].Done++
			}
			return nil
		})
	}
	if err == nil {
		err = s.eachRow(`SELECT t.board_id,COUNT(*) FROM ticket t JOIN board b ON b.id=t.board_id WHERE t.created_at>=? AND b.deleted_at IS NULL`+filter+` GROUP BY t.board_id`, sinceArgs, func(rows *sql.Rows) error {
			var board string
			var n int
			if err := rows.Scan(&board, &n); err != nil || boards[board] == nil {
				return err
			}
			boards[board].Created = n
			out.Created += n
			return nil
		})
	}
	if err == nil {
		dueArgs := append([]any{now.AddDate(0, 0, 7).Format("2006-01-02")}, boardArgs...)
		err = s.eachRow(`SELECT t.id,t.title,t.description,t.number,t.code,t.created_at,t.updated_at,t.due_date,t.status_updated_at,t.status_id,t.board_id,t.ticket_type_id,t.reporter_id,t.assignee_id FROM ticket t JOIN status st ON st.id=t.status_id JOIN board b ON b.id=t.board_id WHERE st.type<>'temporary' AND substr(t.due_date,1,10)<=? AND b.deleted_at IS NULL`+filter+` ORDER BY t.due_date,t.number`, dueArgs, func(rows *sql.Rows) error {
			x, err := scanTicket(rows)
			if err == nil && *x.DueDate < today {
				out.Overdue = append(out.Overdue, x)
			} else if err == nil {
				out.DueSoon = append(out.DueSoon, x)
			}
			return err
		})
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, id := range order {
		if b := boards[id]; b.Created+b.Done+b.Cancelled > 0 {
			out.Boards = append(out.Boards, *b)
		}
	}
	write(w, http.StatusOK, out)
}
