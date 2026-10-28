package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func workflowHandoffFixture(t *testing.T, graph workflowGraph, budget int) (*Server, string, string, string, string) {
	t.Helper()
	db, _, rootDir := chatAPI(t)
	connector := createTestConnector(t, db)
	if _, err := db.Exec(`UPDATE "user" SET connector_id=?,model='test' WHERE id='ai'`, connector); err != nil {
		t.Fatal(err)
	}
	root := createDriverTestChat(t, db, connector, rootDir)
	first := createDriverTestChat(t, db, connector, rootDir)
	second := createDriverTestChat(t, db, connector, rootDir)
	graphJSON, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	wid := uid()
	if _, err := db.Exec(`INSERT INTO workflow(id,name,hop_budget,graph,created_at,updated_at) VALUES(?,?,?,? ,?,?)`, wid, "Test flow", budget, string(graphJSON), now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE chat SET workflow_id=?,workflow_node='root',workflow_run_id=? WHERE id=?`, wid, root, root); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE chat SET workflow_id=?,workflow_node='a',workflow_run_id=? WHERE id=?`, wid, root, first); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE chat SET workflow_id=?,workflow_node='b',workflow_run_id=? WHERE id=?`, wid, root, second); err != nil {
		t.Fatal(err)
	}
	s := New(db, "local", false, "dev", rootDir)
	t.Cleanup(func() { _ = s.Close() })
	return s, root, first, second, wid
}

func fanJoinGraph() workflowGraph {
	return testGraph([]workflowNode{{ID: "root", BotID: "ai"}, {ID: "a", BotID: "ai"}, {ID: "b", BotID: "ai"}, {ID: "join", BotID: "ai", Mode: "wait"}},
		workflowEdge{ID: "root-a", From: "root", To: "a"}, workflowEdge{ID: "root-b", From: "root", To: "b"},
		workflowEdge{ID: "a-join", From: "a", To: "join", ReportBack: true, TimeoutMinutes: 15, Instruction: "merge alpha"},
		workflowEdge{ID: "b-join", From: "b", To: "join", ReportBack: true, TimeoutMinutes: 25})
}

func postWorkflowEdge(t *testing.T, s *Server, from, edge, prompt string, out any) int {
	t.Helper()
	return requestJSON(t, s, http.MethodPost, "/api/chats/"+from+"/handoff", map[string]any{"prompt": prompt, "workflow_edge": edge}, out)
}

func TestWorkflowHandoffRejectsEdgeFromOtherStep(t *testing.T) {
	s, _, a, _, _ := workflowHandoffFixture(t, fanJoinGraph(), 10)
	if code := postWorkflowEdge(t, s, a, "root-a", "work", nil); code != 400 {
		t.Fatalf("status=%d", code)
	}
}

func TestWorkflowHandoffCreatesChildFromEdge(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       int
	}{{"auto", "", 0}, {"wait", "wait", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			g := testGraph([]workflowNode{{ID: "root", BotID: "ai"}, {ID: "next", BotID: "ai", Mode: tc.mode}}, workflowEdge{ID: "go", From: "root", To: "next", ReportBack: true, TimeoutMinutes: 19})
			s, root, _, _, wid := workflowHandoffFixture(t, g, 10)
			var out map[string]string
			if code := postWorkflowEdge(t, s, root, "go", "Do work", &out); code != 200 || out["chat_id"] == "" {
				t.Fatalf("status=%d response=%v", code, out)
			}
			var botID, gotWorkflow, node, runID, edgeID, deadline string
			var wait int
			if err := s.db.QueryRow(`SELECT c.bot_id,c.workflow_id,c.workflow_node,c.workflow_run_id,h.workflow_edge,h.deadline_at,c.workflow_wait FROM chat c JOIN chat_handoff h ON h.to_chat_id=c.id WHERE c.id=?`, out["chat_id"]).Scan(&botID, &gotWorkflow, &node, &runID, &edgeID, &deadline, &wait); err != nil {
				t.Fatal(err)
			}
			if botID != "ai" || gotWorkflow != wid || node != "next" || runID != root || edgeID != "go" || wait != tc.want {
				t.Fatalf("child bot=%s workflow=%s node=%s run=%s edge=%s wait=%d", botID, gotWorkflow, node, runID, edgeID, wait)
			}
			d, err := time.Parse("2006-01-02T15:04:05.000Z", deadline)
			if err != nil || time.Until(d) < 18*time.Minute {
				t.Fatalf("deadline=%s err=%v", deadline, err)
			}
		})
	}
}

func TestWorkflowStepNoteAutoAndWait(t *testing.T) {
	g := testGraph([]workflowNode{{ID: "root", BotID: "ai"}, {ID: "next", BotID: "ai"}}, workflowEdge{ID: "go", From: "root", To: "next"})
	s, root, _, _, _ := workflowHandoffFixture(t, g, 10)
	for _, tc := range []struct {
		name               string
		wait               int
		contains, excludes string
	}{{"auto", 0, "Before ending your turn, hand off", "Do not hand off"}, {"wait", 1, "Do not hand off until the user tells you", "Before ending your turn, hand off"}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.db.Exec(`UPDATE chat SET workflow_wait=? WHERE id=?`, tc.wait, root); err != nil {
				t.Fatal(err)
			}
			note, err := workflowStepNote(s.db, root)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(note, tc.contains) || strings.Contains(note, tc.excludes) {
				t.Fatalf("note=%q", note)
			}
		})
	}
}

func TestChatPromptSavedWorkflowsOnlyOutsideWorkflow(t *testing.T) {
	g := testGraph([]workflowNode{{ID: "root", BotID: "ai"}}, []workflowEdge{}...)
	s, root, plain, _, wid := workflowHandoffFixture(t, g, 10)
	if _, err := s.db.Exec(`UPDATE chat SET workflow_id=NULL,workflow_node=NULL,workflow_run_id=NULL WHERE id=?`, plain); err != nil {
		t.Fatal(err)
	}
	prompt, err := ChatPrompt(s.db, plain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Saved workflows") || !strings.Contains(prompt, "Test flow") || !strings.Contains(prompt, wid) {
		t.Fatalf("saved workflow absent: %s", prompt)
	}
	prompt, err = ChatPrompt(s.db, root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "Saved workflows") || strings.Contains(prompt, wid) {
		t.Fatalf("workflow chat listed saved workflows: %s", prompt)
	}
}

func TestWorkflowJoinWaitsThenFiresCombinedInputs(t *testing.T) {
	s, root, a, b, _ := workflowHandoffFixture(t, fanJoinGraph(), 10)
	for id, title := range map[string]string{a: "Alpha", b: "Beta"} {
		if _, err := s.db.Exec(`UPDATE chat SET title=? WHERE id=?`, title, id); err != nil {
			t.Fatal(err)
		}
	}
	var waiting map[string]any
	if code := postWorkflowEdge(t, s, a, "a-join", "first", &waiting); code != 200 || waiting["status"] != "waiting" || waiting["received"] != float64(1) || waiting["expected"] != float64(2) {
		t.Fatalf("first=%d %v", code, waiting)
	}
	var chats int
	if err := s.db.QueryRow(`SELECT count(*) FROM chat WHERE workflow_run_id=? AND workflow_node='join'`, root).Scan(&chats); err != nil || chats != 0 {
		t.Fatalf("join chats=%d err=%v", chats, err)
	}
	var fired map[string]string
	if code := postWorkflowEdge(t, s, b, "b-join", "second", &fired); code != 200 || fired["chat_id"] == "" {
		t.Fatalf("second=%d %v", code, fired)
	}
	var wait int
	if err := s.db.QueryRow(`SELECT workflow_wait FROM chat WHERE id=?`, fired["chat_id"]).Scan(&wait); err != nil || wait != 1 {
		t.Fatalf("join workflow_wait=%d err=%v", wait, err)
	}
	var prompt string
	if err := s.db.QueryRow(`SELECT text FROM chat_queue WHERE chat_id=?`, fired["chat_id"]).Scan(&prompt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "### Alpha (ok)\nfirst") || !strings.Contains(prompt, "### Beta (ok)\nsecond") {
		t.Fatalf("combined prompt=%q", prompt)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM workflow_join_input WHERE run_id=? AND node_id='join' AND delivered=1`, root).Scan(&chats); err != nil || chats != 2 {
		t.Fatalf("delivered=%d err=%v", chats, err)
	}
}

func TestWorkflowJoinSweepStartsWithMissingInput(t *testing.T) {
	s, root, a, _, wid := workflowHandoffFixture(t, fanJoinGraph(), 10)
	if _, err := s.db.Exec(`UPDATE chat SET title='Alpha' WHERE id=?`, a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO workflow_join_input(id,run_id,node_id,edge_id,from_chat_id,status,text,created_at,deadline_at) VALUES(?,?,?,?,?,'ok','alpha result',?,?)`, uid(), root, "join", "a-join", a, now(), "2020-01-01T00:00:00.000Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.SweepHandoffs(context.Background()); err != nil {
		t.Fatal(err)
	}
	var id, prompt string
	if err := s.db.QueryRow(`SELECT c.id,q.text FROM chat c JOIN chat_queue q ON q.chat_id=c.id WHERE c.workflow_run_id=? AND c.workflow_node='join'`, root).Scan(&id, &prompt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "### Alpha (ok)\nalpha result") || !strings.Contains(prompt, "### Martin (failed)\nNo report before the timeout.") {
		t.Fatalf("prompt=%q", prompt)
	}
	var status string
	if err := s.db.QueryRow(`SELECT status FROM workflow_join_input WHERE run_id=? AND edge_id='b-join'`, root).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("missing status=%q err=%v", status, err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM notification WHERE title=? AND body=? AND link=?`, "Workflow step started with missing inputs", "Test flow", "/ai/chats/"+id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("notification count=%d err=%v (workflow %s)", n, err, wid)
	}
}

func TestWorkflowBudgetNotifiesOnlyOnce(t *testing.T) {
	g := testGraph([]workflowNode{{ID: "root", BotID: "ai"}, {ID: "next", BotID: "ai"}}, workflowEdge{ID: "go", From: "root", To: "next"})
	s, root, child, _, _ := workflowHandoffFixture(t, g, 1)
	if _, err := s.db.Exec(`UPDATE chat SET workflow_run_id=? WHERE id=?`, root, child); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if code := postWorkflowEdge(t, s, root, "go", "work", nil); code != 409 {
			t.Fatalf("attempt %d status=%d", i, code)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM notification WHERE title='Workflow paused: handoff budget reached' AND link=?`, "/ai/chats/"+root).Scan(&n); err != nil || n != 1 {
		t.Fatalf("notifications=%d err=%v", n, err)
	}
}

func TestWorkflowChatsBypassAgentHopCap(t *testing.T) {
	s, root, workflowChat, ordinary, _ := workflowHandoffFixture(t, fanJoinGraph(), 10)
	if _, err := s.db.Exec(`UPDATE chat SET workflow_run_id=NULL,workflow_id=NULL,workflow_node=NULL WHERE id=?`, ordinary); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{workflowChat, ordinary} {
		if _, err := s.db.Exec(`UPDATE chat SET agent_hops=10 WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ id, want string }{{workflowChat, "queued"}, {ordinary, "held"}} {
		var out map[string]string
		code := requestJSON(t, s, http.MethodPost, "/api/chats/"+tc.id+"/agent-messages", map[string]string{"from_chat_id": root, "text": "message"}, &out)
		if code != 200 || out["status"] != tc.want {
			t.Fatalf("chat=%s status=%d result=%v", tc.id, code, out)
		}
	}
}

func TestChatPromptWorkflowEdgesAndLastStep(t *testing.T) {
	graph := fanJoinGraph()
	s, _, a, _, _ := workflowHandoffFixture(t, graph, 10)
	prompt, err := ChatPrompt(s.db, a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, `workflow_edge "a-join"`) || !strings.Contains(prompt, "waits for all its inputs") || !strings.Contains(prompt, "merge alpha") {
		t.Fatalf("workflow prompt missing edge guidance: %s", prompt)
	}
	if _, err = s.db.Exec(`UPDATE chat SET workflow_node='join' WHERE id=?`, a); err != nil {
		t.Fatal(err)
	}
	prompt, err = ChatPrompt(s.db, a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "This is the last step: do it and finish with your result.") {
		t.Fatalf("last-step prompt=%s", prompt)
	}
	if _, err = s.db.Exec(`UPDATE workflow SET deleted_at='9'`); err != nil {
		t.Fatal(err)
	}
	if prompt, err = ChatPrompt(s.db, a); err != nil || strings.Contains(prompt, "Workflow ") {
		t.Fatalf("deleted workflow prompt=%q err=%v", prompt, err)
	}
}
