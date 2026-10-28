package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

func (s *Server) flowStatus(id string) (string, error) {
	if s.chats.Running(id) {
		return "running", nil
	}
	var held, hops, queued, agentQueued, pending int
	err := s.db.QueryRow(`SELECT c.queue_held,c.agent_hops,(SELECT count(*) FROM chat_queue q WHERE q.chat_id=c.id),(SELECT count(*) FROM chat_queue q WHERE q.chat_id=c.id AND q.from_chat_id IS NOT NULL),(SELECT count(*) FROM chat_handoff h WHERE h.from_chat_id=c.id AND h.status='pending') FROM chat c WHERE c.id=?`, id).Scan(&held, &hops, &queued, &agentQueued, &pending)
	if err != nil {
		return "", err
	}
	if held == 1 && queued > 0 || hops >= 10 && agentQueued > 0 {
		return "held", nil
	}
	if queued > 0 {
		return "queued", nil
	}
	if pending > 0 {
		return "waiting", nil
	}
	var role sql.NullString
	err = s.db.QueryRow(`SELECT role FROM chat_message WHERE chat_id=? ORDER BY created_at DESC,rowid DESC LIMIT 1`, id).Scan(&role)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if role.Valid && role.String == "error" {
		return "error", nil
	}
	return "idle", nil
}
func (s *Server) chatTree(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	var exists int
	if err := s.db.QueryRow(`SELECT count(*) FROM chat WHERE id=?`, id).Scan(&exists); err != nil {
		errw(w, 500, err.Error())
		return
	}
	if exists == 0 {
		errw(w, 404, "chat not found")
		return
	}
	root := id
	seen := map[string]bool{}
	for !seen[root] {
		seen[root] = true
		var parent sql.NullString
		err := s.db.QueryRow(`SELECT parent_chat_id FROM chat WHERE id=?`, root).Scan(&parent)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		if !parent.Valid || seen[parent.String] {
			break
		}
		root = parent.String
	}
	type node struct {
		ChatID        string  `json:"chat_id"`
		ParentChatID  *string `json:"parent_chat_id"`
		Title         string  `json:"title"`
		BotID         *string `json:"bot_id"`
		BotName       string  `json:"bot_name"`
		BotColor      string  `json:"bot_color"`
		ConnectorName string  `json:"connector_name"`
		Model         string  `json:"model"`
		Status        string  `json:"status"`
		WorkflowNode  *string `json:"workflow_node"`
		Join          *struct {
			Received int `json:"received"`
			Expected int `json:"expected"`
		} `json:"join"`
	}
	type edge struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Status string `json:"status"`
	}
	rows, err := s.db.Query(`WITH RECURSIVE tree(id) AS (SELECT ? UNION ALL SELECT c.id FROM chat c JOIN tree t ON c.parent_chat_id=t.id) SELECT c.id,c.parent_chat_id,c.title,c.bot_id,COALESCE(u.name,''),COALESCE(u.color,''),COALESCE(x.name,''),c.model,c.workflow_node,c.workflow_run_id,c.workflow_id FROM tree JOIN chat c ON c.id=tree.id LEFT JOIN "user" u ON u.id=c.bot_id LEFT JOIN connector x ON x.id=c.connector_id ORDER BY c.created_at,c.rowid`, root)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	type row struct {
		node
		run, workflowID sql.NullString
	}
	var found []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.ChatID, &x.ParentChatID, &x.Title, &x.BotID, &x.BotName, &x.BotColor, &x.ConnectorName, &x.Model, &x.WorkflowNode, &x.run, &x.workflowID); err != nil {
			rows.Close()
			errw(w, 500, err.Error())
			return
		}
		found = append(found, x)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		errw(w, 500, err.Error())
		return
	}
	nodes := []node{}
	edges := []edge{}
	for _, x := range found {
		n := x.node
		if n.Status, err = s.flowStatus(n.ChatID); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if n.ParentChatID != nil {
			status := "ok"
			if err := s.db.QueryRow(`SELECT status FROM chat_handoff WHERE from_chat_id=? AND to_chat_id=? ORDER BY created_at DESC,rowid DESC LIMIT 1`, *n.ParentChatID, n.ChatID).Scan(&status); err != nil && !errors.Is(err, sql.ErrNoRows) {
				errw(w, 500, err.Error())
				return
			}
			edges = append(edges, edge{*n.ParentChatID, n.ChatID, status})
		}
		if n.WorkflowNode != nil && x.workflowID.Valid {
			var graph string
			if err := s.db.QueryRow(`SELECT graph FROM workflow WHERE id=? AND deleted_at IS NULL`, x.workflowID.String).Scan(&graph); err != nil && !errors.Is(err, sql.ErrNoRows) {
				errw(w, 500, err.Error())
				return
			}
			var g workflowGraph
			if graph != "" {
				if err := json.Unmarshal([]byte(graph), &g); err != nil {
					errw(w, 500, err.Error())
					return
				}
			}
			incoming := 0
			for _, e := range g.Edges {
				if e.To == *n.WorkflowNode {
					incoming++
				}
			}
			if incoming >= 2 {
				var received int
				if err := s.db.QueryRow(`SELECT count(*) FROM workflow_join_input WHERE run_id=? AND node_id=?`, x.run.String, *n.WorkflowNode).Scan(&received); err != nil {
					errw(w, 500, err.Error())
					return
				}
				n.Join = &struct {
					Received int `json:"received"`
					Expected int `json:"expected"`
				}{received, incoming}
			}
		}
		nodes = append(nodes, n)
	}
	var workflow any = nil
	var wid, wname string
	err = s.db.QueryRow(`SELECT w.id,w.name FROM chat c JOIN workflow w ON w.id=c.workflow_id AND w.deleted_at IS NULL WHERE c.id=?`, root).Scan(&wid, &wname)
	if err == nil {
		workflow = map[string]string{"id": wid, "name": wname}
	} else if !errors.Is(err, sql.ErrNoRows) {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, map[string]any{"root_id": root, "workflow": workflow, "nodes": nodes, "edges": edges})
}
