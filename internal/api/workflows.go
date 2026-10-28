package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
)

type workflowNode struct {
	ID          string  `json:"id"`
	BotID       string  `json:"bot_id"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	ConnectorID *string `json:"connector_id,omitempty"`
	Model       *string `json:"model,omitempty"`
	Mode        string  `json:"mode,omitempty"`
}
type workflowEdge struct {
	ID             string `json:"id"`
	From           string `json:"from"`
	To             string `json:"to"`
	ReportBack     bool   `json:"report_back"`
	Instruction    string `json:"instruction"`
	TimeoutMinutes int    `json:"timeout_minutes"`
}
type workflowGraph struct {
	Nodes []workflowNode `json:"nodes"`
	Edges []workflowEdge `json:"edges"`
}
type workflow struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	HopBudget   int           `json:"hop_budget"`
	Graph       workflowGraph `json:"graph"`
	CreatedAt   string        `json:"created_at"`
	UpdatedAt   string        `json:"updated_at"`
}
type workflowInput struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	HopBudget   int           `json:"hop_budget"`
	Graph       workflowGraph `json:"graph"`
}

func (s *Server) loadWorkflow(id string) (workflow, error) {
	var w workflow
	var graph string
	err := s.db.QueryRow(`SELECT id,name,description,hop_budget,graph,created_at,updated_at FROM workflow WHERE id=? AND deleted_at IS NULL`, id).Scan(&w.ID, &w.Name, &w.Description, &w.HopBudget, &graph, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return w, err
	}
	if err = json.Unmarshal([]byte(graph), &w.Graph); err != nil {
		return w, err
	}
	return w, nil
}
func validWorkflow(in *workflowInput, id string, db *sql.DB) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("name required")
	}
	if in.HopBudget == 0 {
		in.HopBudget = 30
	}
	if in.HopBudget < 1 || in.HopBudget > 500 {
		return errors.New("hop_budget must be 1..500")
	}
	if len(in.Graph.Nodes) == 0 {
		return errors.New("at least one node required")
	}
	if in.Graph.Nodes == nil {
		in.Graph.Nodes = []workflowNode{}
	}
	if in.Graph.Edges == nil {
		in.Graph.Edges = []workflowEdge{}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM workflow WHERE name=? COLLATE NOCASE AND deleted_at IS NULL AND id<>?`, in.Name, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("workflow name already exists")
	}
	nodes := map[string]bool{}
	incoming := map[string]int{}
	for _, n := range in.Graph.Nodes {
		if n.Mode != "" && n.Mode != "auto" && n.Mode != "wait" {
			return errors.New("mode must be auto or wait")
		}
		if n.ID == "" || nodes[n.ID] {
			return errors.New("node ids must be unique")
		}
		nodes[n.ID] = true
		var exists int
		if err := db.QueryRow(`SELECT count(*) FROM "user" WHERE id=? AND is_ai=1 AND deleted_at IS NULL`, n.BotID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return errors.New("unknown bot")
		}
	}
	pairs, edgeIDs := map[string]bool{}, map[string]bool{}
	for i := range in.Graph.Edges {
		e := &in.Graph.Edges[i]
		if e.ID == "" || edgeIDs[e.ID] {
			return errors.New("edge ids must be unique")
		}
		edgeIDs[e.ID] = true
		if !nodes[e.From] || !nodes[e.To] {
			return errors.New("edge references unknown node")
		}
		if e.From == e.To {
			return errors.New("self-loop")
		}
		key := e.From + "\x00" + e.To
		if pairs[key] {
			return errors.New("duplicate edge")
		}
		pairs[key] = true
		incoming[e.To]++
		if e.TimeoutMinutes < 0 || e.TimeoutMinutes > 1440 {
			return errors.New("timeout_minutes must be 0..1440")
		}
	}
	var roots []string
	for _, n := range in.Graph.Nodes {
		if incoming[n.ID] == 0 {
			roots = append(roots, n.ID)
		}
	}
	if len(roots) != 1 {
		return errors.New("workflow must have exactly one root")
	}
	// Loops are allowed (review → fix → review); steps the root can't reach would never run.
	reached := map[string]bool{roots[0]: true}
	for queue := roots; len(queue) > 0; queue = queue[1:] {
		for _, e := range in.Graph.Edges {
			if e.From == queue[0] && !reached[e.To] {
				reached[e.To] = true
				queue = append(queue, e.To)
			}
		}
	}
	if len(reached) != len(in.Graph.Nodes) {
		return errors.New("every step must be reachable from the start")
	}
	return nil
}
func (s *Server) listWorkflows(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,name,description,hop_budget,graph,created_at,updated_at FROM workflow WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE,rowid`)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []workflow{}
	for rows.Next() {
		var v workflow
		var g string
		if err = rows.Scan(&v.ID, &v.Name, &v.Description, &v.HopBudget, &g, &v.CreatedAt, &v.UpdatedAt); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if err = json.Unmarshal([]byte(g), &v.Graph); err != nil {
			errw(w, 500, err.Error())
			return
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, out)
}
func (s *Server) createWorkflow(w http.ResponseWriter, r *http.Request) {
	var in workflowInput
	if decode(r, &in) != nil {
		errw(w, 400, "invalid JSON")
		return
	}
	if err := validWorkflow(&in, "", s.db); err != nil {
		errw(w, 400, err.Error())
		return
	}
	g, err := json.Marshal(in.Graph)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	v := workflow{ID: uid(), Name: in.Name, Description: in.Description, HopBudget: in.HopBudget, Graph: in.Graph, CreatedAt: now(), UpdatedAt: now()}
	_, err = s.db.Exec(`INSERT INTO workflow(id,name,description,hop_budget,graph,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, v.ID, v.Name, v.Description, v.HopBudget, string(g), v.CreatedAt, v.UpdatedAt)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 201, v)
}
func (s *Server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	v, err := s.loadWorkflow(idParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "workflow not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, v)
}
func (s *Server) updateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	var in workflowInput
	if decode(r, &in) != nil {
		errw(w, 400, "invalid JSON")
		return
	}
	if _, err := s.loadWorkflow(id); errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "workflow not found")
		return
	} else if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if err := validWorkflow(&in, id, s.db); err != nil {
		errw(w, 400, err.Error())
		return
	}
	g, err := json.Marshal(in.Graph)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	_, err = s.db.Exec(`UPDATE workflow SET name=?,description=?,hop_budget=?,graph=?,updated_at=? WHERE id=? AND deleted_at IS NULL`, in.Name, in.Description, in.HopBudget, string(g), now(), id)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	s.getWorkflow(w, r)
}
func (s *Server) deleteWorkflow(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.Exec(`UPDATE workflow SET deleted_at=?,updated_at=? WHERE id=? AND deleted_at IS NULL`, now(), now(), idParam(r, "id"))
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		errw(w, 500, err.Error())
	} else if n == 0 {
		errw(w, 404, "workflow not found")
	} else {
		noContent(w)
	}
}

func (s *Server) runWorkflow(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt   string `json:"prompt"`
		ChatID   string `json:"chat_id"`
		Announce bool   `json:"announce"`
	}
	if decode(r, &in) != nil || (strings.TrimSpace(in.Prompt) == "") == (in.ChatID == "") {
		errw(w, 400, "exactly one of prompt or chat_id required")
		return
	}
	wf, err := s.loadWorkflow(idParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "workflow not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	var root workflowNode
	incoming := map[string]int{}
	for _, e := range wf.Graph.Edges {
		incoming[e.To]++
	}
	for _, n := range wf.Graph.Nodes {
		if incoming[n.ID] == 0 {
			root = n
			break
		}
	}
	if in.ChatID != "" {
		c, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, in.ChatID))
		if errors.Is(err, sql.ErrNoRows) {
			errw(w, 404, "chat not found")
			return
		}
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		if c.ConnectorID == nil {
			errw(w, 400, errChatConnectorMissing.Error())
			return
		}
		attached, err := s.db.Exec(`UPDATE chat SET workflow_id=?,workflow_node=?,workflow_run_id=id,workflow_wait=1,bot_id=? WHERE id=? AND workflow_run_id IS NULL`, wf.ID, root.ID, root.BotID, in.ChatID)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		if n, err := attached.RowsAffected(); err != nil || n == 0 {
			if err == nil {
				errw(w, 409, "chat is already in a workflow run")
			} else {
				errw(w, 500, err.Error())
			}
			return
		}
		note, err := workflowStepNote(s.db, in.ChatID)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		if in.Announce {
			kind, dir, e := s.chats.connectorConfig(*c.ConnectorID)
			if e != nil {
				errw(w, 500, e.Error())
				return
			}
			stored := `Workflow "` + wf.Name + `" started in this chat.`
			_, e = s.chats.Send(c, kind, dir, ChatSendRequest{Text: stored + "\n\n---\n" + strings.TrimSpace(note), StoredText: stored})
			if e != nil {
				code := 500
				if errors.Is(e, errChatTurnRunning) {
					code = 409
				}
				if _, undoErr := s.db.Exec(`UPDATE chat SET workflow_id=NULL,workflow_node=NULL,workflow_run_id=NULL,workflow_wait=0,bot_id=? WHERE id=?`, c.BotID, in.ChatID); undoErr != nil {
					e, code = errors.Join(e, undoErr), 500
				}
				errw(w, code, e.Error())
				return
			}
		}
		write(w, 200, map[string]string{"chat_id": in.ChatID, "note": strings.TrimSpace(note)})
		return
	}
	b, err := scanBot(s.db.QueryRow(`SELECT `+botColumns+` FROM "user" WHERE id=? AND deleted_at IS NULL`, root.BotID))
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defaults, err := s.chatDefaults()
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	connector := b.ConnectorID
	model := b.Model
	if connector == nil || *connector == "" {
		connector = defaults.ConnectorID
		model = defaults.Model
	}
	if connector == nil || *connector == "" {
		errw(w, 400, "connector_id required")
		return
	}
	kind, dir, err := s.chats.connectorConfig(*connector)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	title := strings.TrimSpace(in.Prompt)
	runes := []rune(title)
	if len(runes) > 60 {
		title = string(runes[:60])
	}
	title = wf.Name + ": " + title
	id := uid()
	created := now()
	botID := b.ID
	child := chat{ID: id}
	workdir := s.chatWorkdir(child)
	if err = os.MkdirAll(workdir, 0700); err != nil {
		errw(w, 500, err.Error())
		return
	}
	if err = os.Chmod(workdir, 0700); err != nil {
		errw(w, 500, cleanupConnectorDir(workdir, err).Error())
		return
	}
	wait := 0
	if root.Mode == "wait" {
		wait = 1
	}
	_, err = s.db.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at,thinking,show_thinking,auto_allow,parent_chat_id,bot_id,workflow_id,workflow_node,workflow_run_id,workflow_wait) VALUES(?,?,?,?,NULL,?,?,?,?,?,NULL,?,?,?,?,?)`, id, *connector, title, model, created, created, defaults.Thinking, defaults.ShowThinking, defaults.AutoAllow, botID, wf.ID, root.ID, id, wait)
	if err != nil {
		errw(w, 500, cleanupConnectorDir(workdir, err).Error())
		return
	}
	child.ConnectorID = connector
	child.Model = model
	child.BotID = &botID
	text, err := withStepNote(s.db, id, in.Prompt)
	if err == nil {
		_, err = s.chats.Send(child, kind, dir, ChatSendRequest{Text: text})
	}
	if err != nil {
		_, delErr := s.db.Exec(`DELETE FROM chat WHERE id=?`, id)
		err = cleanupConnectorDir(workdir, errors.Join(err, delErr))
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, map[string]string{"chat_id": id})
}
