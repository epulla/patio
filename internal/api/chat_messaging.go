package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type agentMessageRequest struct {
	FromChatID     string `json:"from_chat_id"`
	Text           string `json:"text"`
	ReportBack     bool   `json:"report_back"`
	WorkflowEdge   string `json:"workflow_edge,omitempty"`
	TimeoutMinutes int    `json:"timeout_minutes,omitempty"`
}

func countWorkflowIncoming(g workflowGraph, node string) int {
	n := 0
	for _, e := range g.Edges {
		if e.To == node {
			n++
		}
	}
	return n
}

func claimWorkflowJoin(s *Server, run, node string, prompt *string) (int, error) {
	var inputs []struct{ title, status, text string }
	err := txrun(s.db, func(tx *sql.Tx) error {
		rows, err := tx.Query(`UPDATE workflow_join_input SET delivered=1 WHERE run_id=? AND node_id=? AND delivered=0 RETURNING from_chat_id,edge_id,status,text`, run, node)
		if err != nil {
			return err
		}
		for rows.Next() {
			var x struct {
				from         sql.NullString
				edgeID       string
				status, text string
			}
			if err = rows.Scan(&x.from, &x.edgeID, &x.status, &x.text); err != nil {
				rows.Close()
				return err
			}
			title := x.from.String
			if x.from.Valid {
				if e := tx.QueryRow(`SELECT title FROM chat WHERE id=?`, x.from.String).Scan(&title); e != nil && !errors.Is(e, sql.ErrNoRows) {
					rows.Close()
					return e
				}
			} else {
				var graph string
				if e := tx.QueryRow(`SELECT w.graph FROM workflow w JOIN chat c ON c.workflow_id=w.id WHERE c.id=?`, run).Scan(&graph); e != nil {
					rows.Close()
					return e
				}
				var g workflowGraph
				if e := json.Unmarshal([]byte(graph), &g); e != nil {
					rows.Close()
					return e
				}
				for _, edge := range g.Edges {
					if edge.ID == x.edgeID {
						for _, step := range g.Nodes {
							if step.ID == edge.From {
								if e := tx.QueryRow(`SELECT name FROM "user" WHERE id=?`, step.BotID).Scan(&title); e != nil {
									rows.Close()
									return e
								}
								break
							}
						}
						break
					}
				}
			}
			inputs = append(inputs, struct{ title, status, text string }{title, x.status, x.text})
		}
		return errors.Join(rows.Err(), rows.Close())
	})
	if err != nil {
		return 0, err
	}
	*prompt = fmt.Sprintf("Inputs from %d steps:", len(inputs))
	for _, x := range inputs {
		*prompt += fmt.Sprintf("\n\n### %s (%s)\n%s", x.title, x.status, x.text)
	}
	return len(inputs), nil
}

// fireWorkflowJoin claims accumulated inputs and creates the join step. Both the
// last arriving edge and timeout sweeps use this path so a join starts once.
func fireWorkflowJoin(s *Server, run, workflowID, node string, edge workflowEdge, wf workflow) (string, error) {
	var prompt string
	claimed, err := claimWorkflowJoin(s, run, node, &prompt)
	if err != nil || claimed == 0 {
		return "", err
	}
	var parentID string
	if err := s.db.QueryRow(`SELECT from_chat_id FROM workflow_join_input WHERE run_id=? AND node_id=? AND from_chat_id IS NOT NULL ORDER BY created_at,rowid LIMIT 1`, run, node).Scan(&parentID); err != nil {
		return "", err
	}
	parent, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, parentID))
	if err != nil {
		return "", err
	}
	var step workflowNode
	for _, n := range wf.Graph.Nodes {
		if n.ID == node {
			step = n
			break
		}
	}
	bot, err := scanBot(s.db.QueryRow(`SELECT `+botColumns+` FROM "user" WHERE id=?`, step.BotID))
	if err != nil {
		return "", err
	}
	defaults, err := s.chatDefaults()
	if err != nil {
		return "", err
	}
	connector := bot.ConnectorID
	model := bot.Model
	if connector == nil || *connector == "" {
		connector = parent.ConnectorID
		model = parent.Model
	}
	if connector == nil || *connector == "" {
		connector = defaults.ConnectorID
		model = defaults.Model
	}
	if connector == nil || *connector == "" {
		return "", errors.New("connector_id required")
	}
	if bot.ConnectorID != nil && *bot.ConnectorID == *connector {
		model = bot.Model
	}
	id := uid()
	created := now()
	title := prompt
	if len(title) > 60 {
		title = title[:60]
	}
	if title == "" {
		title = "Workflow join"
	}
	child := chat{ID: id, Folder: parent.Folder}
	madeDir := false
	if parent.Folder == nil {
		dir := s.chatWorkdir(child)
		if err = os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
		madeDir = true
		if err = os.Chmod(dir, 0700); err != nil {
			return "", cleanupConnectorDir(dir, err)
		}
	}
	if _, err = s.db.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at,thinking,show_thinking,auto_allow,parent_chat_id,bot_id,workflow_id,workflow_node,workflow_run_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, *connector, title, model, parent.Folder, created, created, parent.Thinking, parent.ShowThinking, parent.AutoAllow, parentID, bot.ID, wf.ID, node, run); err != nil {
		if madeDir {
			err = cleanupConnectorDir(s.chatWorkdir(child), err)
		}
		return "", err
	}
	if err = s.queueAgentMessage(id, agentMessageRequest{FromChatID: parentID, Text: prompt, ReportBack: edge.ReportBack, WorkflowEdge: edge.ID, TimeoutMinutes: edge.TimeoutMinutes}); err != nil {
		_, delErr := s.db.Exec(`DELETE FROM chat WHERE id=?`, id)
		if madeDir {
			err = cleanupConnectorDir(s.chatWorkdir(child), err)
		}
		return "", errors.Join(err, delErr)
	}
	return id, nil
}

func (s *chatManager) deliverHandoffReports(parent string) error {
	type report struct{ id, child, status, result, created, title string }
	var reports []report
	err := txrun(s.db, func(tx *sql.Tx) error {
		rows, err := tx.Query(`UPDATE chat_handoff SET delivered=1 WHERE from_chat_id=? AND status!='pending' AND delivered=0 AND NOT EXISTS(SELECT 1 FROM chat_handoff p WHERE p.from_chat_id=? AND p.status='pending') RETURNING id,to_chat_id,status,result,created_at`, parent, parent)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r report
			if err := rows.Scan(&r.id, &r.child, &r.status, &r.result, &r.created); err != nil {
				rows.Close()
				return err
			}
			reports = append(reports, r)
		}
		return errors.Join(rows.Err(), rows.Close())
	})
	if err != nil {
		return err
	}
	for i := range reports {
		if err := s.db.QueryRow(`SELECT title FROM chat WHERE id=?`, reports[i].child).Scan(&reports[i].title); err != nil {
			return err
		}
	}
	if len(reports) == 0 {
		return nil
	}
	sort.SliceStable(reports, func(i, j int) bool { return reports[i].created < reports[j].created })
	text := reports[0].result
	if reports[0].status != "ok" {
		text = "[Status: " + reports[0].status + "] " + text
	}
	if len(reports) > 1 {
		text = fmt.Sprintf("Reports from %d handoffs:", len(reports))
		for _, r := range reports {
			text += fmt.Sprintf("\n\n### %s (%s)\n%s", r.title, r.status, r.result)
		}
	}
	if s.reportBack == nil {
		return nil
	}
	return s.reportBack(parent, reports[0].child, text)
}

func (s *chatManager) finishHandoffs(chatID, result string, turnErr error, stopping bool) ([]string, bool, error) {
	var targetRows int
	if err := s.db.QueryRow(`SELECT count(*) FROM chat_handoff WHERE to_chat_id=?`, chatID).Scan(&targetRows); err != nil {
		return nil, false, err
	}
	if targetRows == 0 {
		return nil, false, nil
	}
	var ownPending int
	if err := s.db.QueryRow(`SELECT count(*) FROM chat_handoff WHERE from_chat_id=? AND status='pending'`, chatID).Scan(&ownPending); err != nil {
		return nil, true, err
	}
	if ownPending > 0 {
		return nil, true, nil
	}
	status := "ok"
	if stopping {
		status = "stopped"
	} else if turnErr != nil {
		status = "failed"
	}
	var parents []string
	if err := txrun(s.db, func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT DISTINCT from_chat_id FROM chat_handoff WHERE to_chat_id=? AND status='pending'`, chatID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				rows.Close()
				return err
			}
			parents = append(parents, p)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE chat_handoff SET status=?,result=?,finished_at=? WHERE to_chat_id=? AND status='pending'`, status, result, now(), chatID)
		return err
	}); err != nil {
		return nil, true, err
	}
	return parents, true, nil
}

func (s *Server) agentMessage(w http.ResponseWriter, r *http.Request) {
	var in agentMessageRequest
	if decode(r, &in) != nil || !required(in.FromChatID) || !required(in.Text) {
		errw(w, 400, "from_chat_id and text required")
		return
	}
	id := idParam(r, "id")
	lock := s.chats.queueLock(id)
	lock.Lock()
	defer lock.Unlock()
	var hops int
	if err := s.db.QueryRow(`SELECT agent_hops FROM chat WHERE id=?`, id).Scan(&hops); err != nil {
		errw(w, 404, "chat not found")
		return
	}
	var senderExists int
	if err := s.db.QueryRow(`SELECT 1 FROM chat WHERE id=?`, in.FromChatID).Scan(&senderExists); err != nil {
		errw(w, 400, "sender chat not found")
		return
	}
	var workflowRun sql.NullString
	if err := s.db.QueryRow(`SELECT workflow_run_id FROM chat WHERE id=?`, id).Scan(&workflowRun); err != nil {
		errw(w, 500, err.Error())
		return
	}
	if hops >= 10 && !workflowRun.Valid {
		if err := s.queueAgentMessage(id, in); err != nil {
			log.Printf("queue held agent message for %s: %v", id, err)
			errw(w, 500, err.Error())
			return
		}
		if err := s.notifyAgentHopCap(id); err != nil {
			log.Printf("notify agent hop cap for %s: %v", id, err)
		}
		write(w, 200, map[string]string{"status": "held"})
		return
	}
	if err := s.queueAgentMessage(id, in); err != nil {
		log.Printf("queue agent message for %s: %v", id, err)
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, map[string]string{"status": "queued"})
}

func (s *Server) notifyAgentHopCap(chatID string) error {
	result, err := s.db.Exec(`UPDATE chat SET agent_hops_notified=1 WHERE id=? AND agent_hops>=10 AND agent_hops_notified=0 AND EXISTS(SELECT 1 FROM chat_queue WHERE chat_id=? AND from_chat_id IS NOT NULL)`, chatID, chatID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return nil
	}
	var title string
	if err := s.db.QueryRow(`SELECT title FROM chat WHERE id=?`, chatID).Scan(&title); err != nil {
		return err
	}
	return s.Notify("chat", "Chat paused: too many agent messages", title+" received 10 agent messages in a row. Open it to continue.", "/ai/chats/"+chatID, true)
}

func reportTarget(report bool, id string) any {
	if report {
		return id
	}
	return nil
}
func agentMessageLabel(fromID, title string, reportBack bool) string {
	label := `[Message from chat_id ` + fromID + `, title ` + strconv.Quote(title) + `. Reply with send_to_chat if needed.]`
	if reportBack {
		label = `[Message from chat_id ` + fromID + `, title ` + strconv.Quote(title) + `. Final reply will be sent back automatically; do not call send_to_chat to answer.]`
	}
	return label + "\n\n"
}

func (s *Server) queueAgentMessage(id string, in agentMessageRequest) error {
	created := now()
	err := txrun(s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO chat_queue(id,chat_id,text,attachments,created_at,from_chat_id,report_to_chat_id) VALUES(?,?,?,'[]',?,?,?)`, uid(), id, in.Text, created, in.FromChatID, reportTarget(in.ReportBack, in.FromChatID)); err != nil {
			return err
		}
		if in.ReportBack {
			var runID sql.NullString
			if err := tx.QueryRow(`SELECT workflow_run_id FROM chat WHERE id=?`, in.FromChatID).Scan(&runID); err != nil {
				return err
			}
			minutes := in.TimeoutMinutes
			if minutes == 0 {
				minutes = 30
			}
			deadline := time.Now().UTC().Add(time.Duration(minutes) * time.Minute).Format("2006-01-02T15:04:05.000Z")
			var edge any
			if in.WorkflowEdge != "" {
				edge = in.WorkflowEdge
			}
			_, err := tx.Exec(`INSERT INTO chat_handoff(id,run_id,from_chat_id,to_chat_id,workflow_edge,status,result,created_at,deadline_at) VALUES(?,?,?,?,?,'pending','',?,?)`, uid(), runID, in.FromChatID, id, edge, created, deadline)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.chats.emit(id, map[string]any{"type": "queue"})
	return nil
}

func (s *Server) reportChatResult(target, from, text string) error {
	data, err := json.Marshal(agentMessageRequest{FromChatID: from, Text: text})
	if err != nil {
		return err
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chats/"+target+"/agent-messages", bytes.NewReader(data))
	req.SetPathValue("id", target)
	rec := httptest.NewRecorder()
	s.agentMessage(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		deliveryErr := errors.New(strings.TrimSpace(rec.Body.String()))
		title := target
		if err := s.db.QueryRow(`SELECT title FROM chat WHERE id=?`, target).Scan(&title); err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("lookup report target %s title: %v", target, err)
		}
		if err := s.Notify("chat", "Couldn't report back to "+title, deliveryErr.Error(), "/ai/chats/"+from, true); err != nil {
			log.Printf("notify report-back failure to %s: %v", target, err)
		}
		return deliveryErr
	}
	if s.chatWindow != "" || s.userID != "local" {
		return nil
	}
	if err := s.DeliverAgentMessages(context.Background()); err != nil {
		return err
	}
	return nil
}

func (s *Server) createChatHandoff(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConnectorID  string `json:"connector_id"`
		Model        string `json:"model"`
		Bot          string `json:"bot"`
		Prompt       string `json:"prompt"`
		ReportBack   bool   `json:"report_back"`
		WorkflowEdge string `json:"workflow_edge"`
	}
	if decode(r, &in) != nil || !required(in.Prompt) {
		errw(w, 400, "prompt required")
		return
	}
	caller, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, idParam(r, "id")))
	if err != nil {
		errw(w, 404, "caller chat not found")
		return
	}
	var selectedBot bot
	var wf workflow
	var edge workflowEdge
	workflowRun := ""
	workflowIDValue := ""
	incomingCount := 0
	if in.WorkflowEdge != "" {
		var wid, node, run sql.NullString
		if err := s.db.QueryRow(`SELECT workflow_id,workflow_node,workflow_run_id FROM chat WHERE id=?`, caller.ID).Scan(&wid, &node, &run); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if !wid.Valid || !node.Valid {
			errw(w, 400, "workflow_edge requires a workflow chat")
			return
		}
		workflowIDValue = wid.String
		wf, err = s.loadWorkflow(wid.String)
		if err != nil {
			errw(w, 400, "workflow not found")
			return
		}
		for _, e := range wf.Graph.Edges {
			if e.ID == in.WorkflowEdge && e.From == node.String {
				edge = e
				break
			}
		}
		if edge.ID == "" {
			errw(w, 400, "workflow_edge must leave the caller's workflow step")
			return
		}
		if run.Valid {
			workflowRun = run.String
		}
		incomingCount = countWorkflowIncoming(wf.Graph, edge.To)
		var count int
		if err := s.db.QueryRow(`SELECT count(*) FROM chat WHERE workflow_run_id=? AND id<>?`, workflowRun, workflowRun).Scan(&count); err != nil {
			errw(w, 500, err.Error())
			return
		}
		var joins int
		if err := s.db.QueryRow(`SELECT count(*) FROM workflow_join_input WHERE run_id=? AND delivered=0`, workflowRun).Scan(&joins); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if count+joins >= wf.HopBudget {
			var notified int
			if err := s.db.QueryRow(`SELECT count(*) FROM app_setting WHERE key=?`, "workflow_budget_notified_"+workflowRun).Scan(&notified); err != nil {
				errw(w, 500, err.Error())
				return
			}
			if notified == 0 {
				result, err := s.db.Exec(`INSERT OR IGNORE INTO app_setting(key,value) VALUES(?,?)`, "workflow_budget_notified_"+workflowRun, `"1"`)
				if err != nil {
					errw(w, 500, err.Error())
					return
				}
				inserted, err := result.RowsAffected()
				if err != nil {
					errw(w, 500, err.Error())
					return
				}
				if inserted != 0 {
					if err := s.Notify("chat", "Workflow paused: handoff budget reached", wf.Name, "/ai/chats/"+workflowRun, true); err != nil {
						if _, deleteErr := s.db.Exec(`DELETE FROM app_setting WHERE key=?`, "workflow_budget_notified_"+workflowRun); deleteErr != nil {
							err = errors.Join(err, deleteErr)
						}
						errw(w, 500, err.Error())
						return
					}
				}
			}
			errw(w, 409, fmt.Sprintf("workflow run reached its handoff budget (%d)", wf.HopBudget))
			return
		}
		for _, n := range wf.Graph.Nodes {
			if n.ID == edge.To {
				selectedBot, err = scanBot(s.db.QueryRow(`SELECT `+botColumns+` FROM "user" WHERE id=?`, n.BotID))
				if err != nil {
					errw(w, 500, err.Error())
					return
				}
				break
			}
		}
		in.ReportBack = edge.ReportBack
	}
	defaults, err := s.chatDefaults()
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if in.WorkflowEdge == "" && in.Bot != "" {
		selectedBot, err = scanBot(s.db.QueryRow(`SELECT `+botColumns+` FROM "user" WHERE name=? COLLATE NOCASE AND is_ai=1 AND deleted_at IS NULL`, in.Bot))
		if errors.Is(err, sql.ErrNoRows) {
			rows, queryErr := s.db.Query(`SELECT name FROM "user" WHERE is_ai=1 AND deleted_at IS NULL ORDER BY name COLLATE NOCASE,rowid`)
			if queryErr != nil {
				errw(w, 500, queryErr.Error())
				return
			}
			var names []string
			for rows.Next() {
				var name string
				if queryErr = rows.Scan(&name); queryErr != nil {
					break
				}
				names = append(names, name)
			}
			queryErr = errors.Join(queryErr, rows.Err(), rows.Close())
			if queryErr != nil {
				errw(w, 500, queryErr.Error())
				return
			}
			errw(w, 400, `unknown bot `+strconv.Quote(in.Bot)+`; available bots: `+strings.Join(names, ", "))
			return
		}
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
	}
	connectorID := in.ConnectorID
	if connectorID == "" && selectedBot.ConnectorID != nil {
		connectorID = *selectedBot.ConnectorID
	}
	if connectorID == "" && caller.ConnectorID != nil {
		connectorID = *caller.ConnectorID
	}
	if connectorID == "" && defaults.ConnectorID != nil {
		connectorID = *defaults.ConnectorID
	}
	if connectorID == "" {
		errw(w, 400, "connector_id required")
		return
	}
	model := in.Model
	if model == "" && selectedBot.ConnectorID != nil && connectorID == *selectedBot.ConnectorID {
		model = selectedBot.Model
	}
	if model == "" && caller.ConnectorID != nil && connectorID == *caller.ConnectorID {
		model = caller.Model
	}
	if model == "" && defaults.ConnectorID != nil && connectorID == *defaults.ConnectorID {
		model = defaults.Model
	}
	depth := 0
	parent := caller.ID
	for parent != "" {
		var p sql.NullString
		if err := s.db.QueryRow(`SELECT parent_chat_id FROM chat WHERE id=?`, parent).Scan(&p); err != nil {
			break
		}
		if !p.Valid {
			break
		}
		depth++
		parent = p.String
	}
	if depth >= 5 {
		errw(w, 400, "chat handoff depth limit reached")
		return
	}
	if in.WorkflowEdge != "" && incomingCount >= 2 {
		created := now()
		minutes := edge.TimeoutMinutes
		if minutes == 0 {
			minutes = 30
		}
		deadline := time.Now().UTC().Add(time.Duration(minutes) * time.Minute).Format("2006-01-02T15:04:05.000Z")
		_, err := s.db.Exec(`INSERT INTO workflow_join_input(id,run_id,node_id,edge_id,from_chat_id,status,text,created_at,deadline_at,delivered) VALUES(?,?,?,?,?,'ok',?,?,?,0)`, uid(), workflowRun, edge.To, edge.ID, caller.ID, in.Prompt, created, deadline)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				errw(w, 409, "already handed off on this edge")
			} else {
				errw(w, 500, err.Error())
			}
			return
		}
		var received int
		if err = s.db.QueryRow(`SELECT count(*) FROM workflow_join_input WHERE run_id=? AND node_id=? AND delivered=0`, workflowRun, edge.To).Scan(&received); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if received < incomingCount {
			write(w, 200, map[string]any{"status": "waiting", "received": received, "expected": incomingCount})
			return
		}
		id, err := fireWorkflowJoin(s, workflowRun, workflowIDValue, edge.To, edge, wf)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		write(w, 200, map[string]string{"chat_id": id, "status": "queued"})
		return
	}
	title := strings.TrimSpace(in.Prompt)
	if len(title) > 60 {
		title = title[:60]
	}
	if title == "" {
		title = "Handoff from " + caller.Title
	}
	id := uid()
	botID := caller.BotID
	if in.WorkflowEdge != "" || in.Bot != "" {
		botID = &selectedBot.ID
	}
	if botID == nil && defaults.DefaultBotID != "none" {
		botID = &defaults.DefaultBotID
	}
	folder := caller.Folder
	created := now()
	child := chat{ID: id, Folder: folder}
	createdWorkdir := false
	if folder == nil {
		dir := s.chatWorkdir(child)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			errw(w, 500, err.Error())
			return
		}
		createdWorkdir = true
		if err := os.Chmod(dir, 0o700); err != nil {
			errw(w, 500, cleanupConnectorDir(dir, err).Error())
			return
		}
	}
	var workflowID, workflowNode, runID any
	if in.WorkflowEdge != "" {
		workflowID, workflowNode, runID = workflowIDValue, edge.To, workflowRun
	}
	if _, err := s.db.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at,thinking,show_thinking,auto_allow,parent_chat_id,bot_id,workflow_id,workflow_node,workflow_run_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, connectorID, title, model, folder, created, created, caller.Thinking, caller.ShowThinking, caller.AutoAllow, caller.ID, botID, workflowID, workflowNode, runID); err != nil {
		if createdWorkdir {
			err = cleanupConnectorDir(s.chatWorkdir(child), err)
		}
		errw(w, 500, err.Error())
		return
	}
	var outcome struct {
		Status string `json:"status"`
	}
	body := agentMessageRequest{FromChatID: caller.ID, Text: in.Prompt, ReportBack: in.ReportBack, WorkflowEdge: in.WorkflowEdge, TimeoutMinutes: edge.TimeoutMinutes}
	raw, marshalErr := json.Marshal(body)
	if marshalErr != nil {
		errw(w, 500, marshalErr.Error())
		return
	}
	req := r.Clone(r.Context())
	req.SetPathValue("id", id)
	req.Body = io.NopCloser(bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	s.agentMessage(rec, req)
	if rec.Code >= 400 {
		cause := errors.New(strings.TrimSpace(rec.Body.String()))
		_, deleteErr := s.db.Exec(`DELETE FROM chat WHERE id=?`, id)
		if deleteErr != nil {
			cause = errors.Join(cause, deleteErr)
		}
		if createdWorkdir {
			cause = cleanupConnectorDir(s.chatWorkdir(child), cause)
		}
		errw(w, rec.Code, cause.Error())
		return
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &outcome); err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, map[string]string{"chat_id": id, "status": outcome.Status})
}
