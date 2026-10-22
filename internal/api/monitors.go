package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/robfig/cron/v3"
)

type monitor struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	MCPServerID string  `json:"mcp_server_id"`
	Tool        string  `json:"tool"`
	Args        string  `json:"args"`
	Schedule    string  `json:"schedule"`
	NextRunAt   *string `json:"next_run_at"`
	Condition   string  `json:"condition"`
	ResultPath  string  `json:"result_path"`
	IDPath      string  `json:"id_path"`
	Actions     string  `json:"actions"`
	Enabled     bool    `json:"enabled"`
	LastRunAt   *string `json:"last_run_at"`
	LastStatus  *string `json:"last_status"`
	LastError   string  `json:"last_error"`
	LastHash    string  `json:"last_hash"`
	FailStreak  int     `json:"fail_streak"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type monitorInput struct {
	Name        string          `json:"name"`
	MCPServerID string          `json:"mcp_server_id"`
	Tool        string          `json:"tool"`
	Args        json.RawMessage `json:"args"`
	Schedule    string          `json:"schedule"`
	Condition   string          `json:"condition"`
	ResultPath  string          `json:"result_path"`
	IDPath      string          `json:"id_path"`
	Actions     json.RawMessage `json:"actions"`
	Enabled     *bool           `json:"enabled"`
}

const monitorColumns = `id,name,mcp_server_id,tool,args,schedule,next_run_at,"condition",result_path,id_path,actions,enabled,last_run_at,last_status,last_error,last_hash,fail_streak,created_at,updated_at`

func scanMonitor(row interface{ Scan(...any) error }) (monitor, error) {
	var m monitor
	var enabled int
	err := row.Scan(&m.ID, &m.Name, &m.MCPServerID, &m.Tool, &m.Args, &m.Schedule, &m.NextRunAt, &m.Condition, &m.ResultPath, &m.IDPath, &m.Actions, &enabled, &m.LastRunAt, &m.LastStatus, &m.LastError, &m.LastHash, &m.FailStreak, &m.CreatedAt, &m.UpdatedAt)
	m.Enabled = enabled != 0
	return m, err
}

func (s *Server) validateMonitor(in *monitorInput) (string, string, *string, error) {
	if !required(in.Name) || !required(in.Tool) {
		return "", "", nil, errors.New("name and tool required")
	}
	if _, err := s.loadMCPServer(in.MCPServerID); errors.Is(err, sql.ErrNoRows) {
		return "", "", nil, errors.New("mcp server not found")
	} else if err != nil {
		return "", "", nil, err
	}
	args := in.Args
	if len(args) == 0 {
		args = []byte(`{}`)
	}
	var argMap map[string]any
	if err := json.Unmarshal(args, &argMap); err != nil || argMap == nil {
		return "", "", nil, errors.New("args must be a JSON object")
	}
	if in.Condition == "" {
		in.Condition = "changed"
	}
	if in.Condition != "changed" && in.Condition != "non_empty" && in.Condition != "new_items" {
		return "", "", nil, errors.New("invalid condition")
	}
	if in.Condition == "new_items" && !required(in.IDPath) {
		return "", "", nil, errors.New("id_path required for new_items")
	}
	actions := in.Actions
	if len(actions) == 0 {
		actions = []byte(`{}`)
	}
	var actionMap map[string]json.RawMessage
	if err := json.Unmarshal(actions, &actionMap); err != nil || actionMap == nil {
		return "", "", nil, errors.New("actions must be a JSON object")
	}
	var ticket *struct {
		BoardID string `json:"board_id"`
		Title   string `json:"title"`
	}
	if v, ok := actionMap["ticket"]; ok && string(v) != "null" {
		ticket = &struct {
			BoardID string `json:"board_id"`
			Title   string `json:"title"`
		}{}
		if err := json.Unmarshal(v, ticket); err != nil {
			return "", "", nil, errors.New("invalid ticket action")
		}
		if !required(ticket.BoardID) || !required(ticket.Title) {
			return "", "", nil, errors.New("ticket action requires board_id and title")
		}
		var exists int
		if err := s.db.QueryRow(`SELECT 1 FROM board WHERE id=?`, ticket.BoardID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", "", nil, errors.New("ticket board not found")
			}
			return "", "", nil, err
		}
	}
	if v, ok := actionMap["chat"]; ok && string(v) != "null" {
		var chat struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(v, &chat); err != nil || !required(chat.Prompt) {
			return "", "", nil, errors.New("chat action requires prompt")
		}
	}
	parsed, err := cron.ParseStandard(in.Schedule)
	if err != nil {
		return "", "", nil, fmt.Errorf("invalid schedule: %w", err)
	}
	now := time.Now()
	first, second := parsed.Next(now), parsed.Next(parsed.Next(now))
	if second.Sub(first) < time.Minute {
		return "", "", nil, errors.New("schedule must not fire more than once per minute")
	}
	var next *string
	if in.Enabled == nil {
		yes := true
		in.Enabled = &yes
	}
	if *in.Enabled {
		value := first.UTC().Format("2006-01-02T15:04:05.000Z")
		next = &value
	}
	return string(args), string(actions), next, nil
}

func (s *Server) listMonitors(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(`SELECT ` + monitorColumns + ` FROM monitor ORDER BY created_at DESC,rowid DESC`)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []monitor{}
	for rows.Next() {
		m, e := scanMonitor(rows)
		if e != nil {
			errw(w, 500, e.Error())
			return
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, out)
}
func (s *Server) getMonitor(w http.ResponseWriter, r *http.Request) {
	m, e := scanMonitor(s.db.QueryRow(`SELECT `+monitorColumns+` FROM monitor WHERE id=?`, idParam(r, "id")))
	if errors.Is(e, sql.ErrNoRows) {
		errw(w, 404, "monitor not found")
	} else if e != nil {
		errw(w, 500, e.Error())
	} else {
		write(w, 200, m)
	}
}
func (s *Server) saveMonitor(w http.ResponseWriter, r *http.Request, create bool) {
	var in monitorInput
	if decode(r, &in) != nil {
		errw(w, 400, "invalid monitor JSON")
		return
	}
	args, actions, next, e := s.validateMonitor(&in)
	if e != nil {
		errw(w, 400, e.Error())
		return
	}
	id, stamp := idParam(r, "id"), now()
	if create {
		id = uid()
		_, e = s.db.Exec(`INSERT INTO monitor(`+monitorColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, in.Name, in.MCPServerID, in.Tool, args, in.Schedule, next, in.Condition, in.ResultPath, in.IDPath, actions, *in.Enabled, nil, nil, "", "", 0, stamp, stamp)
	} else {
		_, e = s.db.Exec(`UPDATE monitor SET name=?,mcp_server_id=?,tool=?,args=?,schedule=?,next_run_at=?,"condition"=?,result_path=?,id_path=?,actions=?,enabled=?,updated_at=? WHERE id=?`, in.Name, in.MCPServerID, in.Tool, args, in.Schedule, next, in.Condition, in.ResultPath, in.IDPath, actions, *in.Enabled, stamp, id)
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	m, e := scanMonitor(s.db.QueryRow(`SELECT `+monitorColumns+` FROM monitor WHERE id=?`, id))
	if errors.Is(e, sql.ErrNoRows) {
		errw(w, 404, "monitor not found")
		return
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	code := 200
	if create {
		code = 201
	}
	write(w, code, m)
}
func (s *Server) createMonitor(w http.ResponseWriter, r *http.Request) { s.saveMonitor(w, r, true) }
func (s *Server) updateMonitor(w http.ResponseWriter, r *http.Request) { s.saveMonitor(w, r, false) }
func (s *Server) deleteMonitor(w http.ResponseWriter, r *http.Request) {
	res, e := s.db.Exec(`DELETE FROM monitor WHERE id=?`, idParam(r, "id"))
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	n, e := res.RowsAffected()
	if e != nil {
		errw(w, 500, e.Error())
	} else if n == 0 {
		errw(w, 404, "monitor not found")
	} else {
		noContent(w)
	}
}
func (s *Server) monitorRuns(w http.ResponseWriter, r *http.Request) {
	rows, e := s.db.Query(`SELECT id,monitor_id,started_at,status,result_hash,new_count,error FROM monitor_run WHERE monitor_id=? ORDER BY started_at DESC LIMIT 100`, idParam(r, "id"))
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, mid, started, status, hash, msg string
		var count int
		if e := rows.Scan(&id, &mid, &started, &status, &hash, &count, &msg); e != nil {
			errw(w, 500, e.Error())
			return
		}
		out = append(out, map[string]any{"id": id, "monitor_id": mid, "started_at": started, "status": status, "result_hash": hash, "new_count": count, "error": msg})
	}
	write(w, 200, out)
}

func (s *Server) callMonitorTool(ctx context.Context, serverID, tool string, args map[string]any) (string, error) {
	server, err := s.loadMCPServer(serverID)
	if err != nil {
		return "", err
	}
	spec, err := s.mcpServerCheckSpec(ctx, server)
	if err != nil {
		return "", err
	}
	if spec.Auth == "oauth" && spec.OAuthHeader == nil {
		return "", errors.New("Sign in first")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	session, cleanup, err := s.mcpConnect(ctx, spec)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return "", err
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return "", err
	}
	if result.IsError {
		var text []string
		for _, c := range result.Content {
			if t, ok := c.(*mcp.TextContent); ok {
				text = append(text, t.Text)
			}
		}
		return "", errors.New(strings.Join(text, "\n"))
	}
	if result.StructuredContent != nil {
		b, e := json.Marshal(result.StructuredContent)
		return string(b), e
	}
	var text []string
	for _, c := range result.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			text = append(text, t.Text)
		}
	}
	return strings.Join(text, ""), nil
}
func (s *Server) testMonitor(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MCPServerID string          `json:"mcp_server_id"`
		Tool        string          `json:"tool"`
		Args        json.RawMessage `json:"args"`
	}
	if decode(r, &in) != nil || !required(in.MCPServerID) || !required(in.Tool) {
		errw(w, 400, "mcp_server_id and tool required")
		return
	}
	var args map[string]any
	if len(in.Args) == 0 {
		args = map[string]any{}
	} else if e := json.Unmarshal(in.Args, &args); e != nil || args == nil {
		errw(w, 400, "args must be a JSON object")
		return
	}
	result, e := s.callMonitorTool(r.Context(), in.MCPServerID, in.Tool, args)
	if e != nil {
		write(w, 200, map[string]string{"error": e.Error()})
		return
	}
	write(w, 200, map[string]string{"result": result})
}
