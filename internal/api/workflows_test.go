package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"patio/internal/db"
)

func testGraph(nodes []workflowNode, edges ...workflowEdge) workflowGraph {
	if edges == nil {
		edges = []workflowEdge{}
	}
	return workflowGraph{Nodes: nodes, Edges: edges}
}

func TestWorkflowCRUDAndValidation(t *testing.T) {
	database, handler := newAPI(t)
	if _, err := database.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai) VALUES('billy','Billy','billy@test',1,1)`); err != nil {
		t.Fatal(err)
	}
	martin, billy := workflowNode{ID: "m", BotID: "ai"}, workflowNode{ID: "b", BotID: "billy"}
	flow := map[string]any{"name": "Dev flow", "graph": testGraph([]workflowNode{martin, billy}, workflowEdge{ID: "e", From: "m", To: "b", ReportBack: true})}
	var created workflow
	if code := requestJSON(t, handler, http.MethodPost, "/api/workflows", flow, &created); code != http.StatusCreated || created.HopBudget != 30 || len(created.Graph.Edges) != 1 {
		t.Fatalf("create = %d %+v", code, created)
	}
	for _, test := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"duplicate name", flow, "already exists"},
		{"no nodes", map[string]any{"name": "x", "graph": testGraph(nil)}, "at least one node"},
		{"unknown bot", map[string]any{"name": "x", "graph": testGraph([]workflowNode{{ID: "a", BotID: "nobody"}})}, "unknown bot"},
		{"two roots", map[string]any{"name": "x", "graph": testGraph([]workflowNode{martin, billy})}, "exactly one root"},
		{"self-loop", map[string]any{"name": "x", "graph": testGraph([]workflowNode{martin}, workflowEdge{ID: "e", From: "m", To: "m"})}, "self-loop"},
		{"unreachable loop", map[string]any{"name": "x", "graph": testGraph([]workflowNode{martin, billy, {ID: "c", BotID: "ai"}}, workflowEdge{ID: "e1", From: "b", To: "c"}, workflowEdge{ID: "e2", From: "c", To: "b"})}, "reachable"},
		{"duplicate edge id", map[string]any{"name": "x", "graph": testGraph([]workflowNode{martin, billy, {ID: "c", BotID: "ai"}}, workflowEdge{ID: "e", From: "m", To: "b"}, workflowEdge{ID: "e", From: "m", To: "c"})}, "edge ids"},
		{"dangling edge", map[string]any{"name": "x", "graph": testGraph([]workflowNode{martin}, workflowEdge{ID: "e", From: "m", To: "gone"})}, "unknown node"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out map[string]string
			if code := requestJSON(t, handler, http.MethodPost, "/api/workflows", test.body, &out); code != http.StatusBadRequest || !strings.Contains(out["error"], test.want) {
				t.Fatalf("status = %d, body = %v; want 400 containing %q", code, out, test.want)
			}
		})
	}
	var updated workflow
	if code := requestJSON(t, handler, http.MethodPatch, "/api/workflows/"+created.ID, map[string]any{"name": "Dev flow 2", "hop_budget": 12, "graph": flow["graph"]}, &updated); code != http.StatusOK || updated.Name != "Dev flow 2" || updated.HopBudget != 12 {
		t.Fatalf("update = %d %+v", code, updated)
	}
	if entries := itemHistoryOf(t, handler, "workflow", created.ID); len(entries) != 2 {
		t.Fatalf("history entries = %d, want 2", len(entries))
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/workflows/"+created.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
	var list []workflow
	if code := requestJSON(t, handler, http.MethodGet, "/api/workflows", nil, &list); code != http.StatusOK || len(list) != 0 {
		t.Fatalf("list after delete = %d %+v", code, list)
	}
}

func TestRunWorkflowStartsRootChatAndTree(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	root := t.TempDir()
	connectorID := createTestConnector(t, database)
	if _, err := database.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai,connector_id,color) VALUES('billy','Billy','billy@test',1,1,?,'#123456')`, connectorID); err != nil {
		t.Fatal(err)
	}
	s := New(database, "local", false, "dev", root, Options{
		DBPath:         filepath.Join(root, "test.db"),
		findClaude:     func() (string, error) { return "claude-test", nil },
		commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil },
		processFactory: func(string, []string, []string, string) (chatProcess, error) { return newPipeChatProcess(), nil },
	})
	defer s.Close()
	graph := testGraph([]workflowNode{{ID: "b", BotID: "billy"}, {ID: "s1", BotID: "ai"}, {ID: "s2", BotID: "ai"}, {ID: "j", BotID: "ai"}},
		workflowEdge{ID: "e1", From: "b", To: "s1"}, workflowEdge{ID: "e2", From: "b", To: "s2"}, workflowEdge{ID: "e3", From: "s1", To: "j"}, workflowEdge{ID: "e4", From: "s2", To: "j"})
	var created workflow
	if code := requestJSON(t, s, http.MethodPost, "/api/workflows", map[string]any{"name": "Fan", "graph": graph}, &created); code != http.StatusCreated {
		t.Fatalf("create = %d", code)
	}
	var run struct {
		ChatID string `json:"chat_id"`
	}
	if code := requestJSON(t, s, http.MethodPost, "/api/workflows/"+created.ID+"/run", map[string]string{"prompt": "Build it"}, &run); code != http.StatusOK || run.ChatID == "" {
		t.Fatalf("run = %d %+v", code, run)
	}
	var botID, workflowID, node, runID, text string
	if err := database.QueryRow(`SELECT c.bot_id,c.workflow_id,c.workflow_node,c.workflow_run_id,m.content FROM chat c JOIN chat_message m ON m.chat_id=c.id AND m.role='user' WHERE c.id=?`, run.ChatID).Scan(&botID, &workflowID, &node, &runID, &text); err != nil {
		t.Fatal(err)
	}
	if botID != "billy" || workflowID != created.ID || node != "b" || runID != run.ChatID || text != "Build it" {
		t.Fatalf("root chat bot=%q workflow=%q node=%q run=%q text=%q", botID, workflowID, node, runID, text)
	}

	// A join child with one of two inputs, linked by a pending handoff.
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at,parent_chat_id,bot_id,workflow_id,workflow_node,workflow_run_id) VALUES('join-chat','Join','9','9',?,'ai',?,'j',?)`, run.ChatID, created.ID, run.ChatID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO chat_handoff(id,run_id,from_chat_id,to_chat_id,status,created_at,deadline_at) VALUES('h1',?,?,'join-chat','pending','9','99')`, run.ChatID, run.ChatID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO workflow_join_input(id,run_id,node_id,edge_id,status,text,created_at,deadline_at) VALUES('in1',?,'j','e3','ok','done','9','99')`, run.ChatID); err != nil {
		t.Fatal(err)
	}
	var tree struct {
		RootID   string            `json:"root_id"`
		Workflow map[string]string `json:"workflow"`
		Nodes    []struct {
			ChatID   string `json:"chat_id"`
			BotName  string `json:"bot_name"`
			BotColor string `json:"bot_color"`
			Join     *struct {
				Received int `json:"received"`
				Expected int `json:"expected"`
			} `json:"join"`
		} `json:"nodes"`
		Edges []struct {
			From, To, Status string
		} `json:"edges"`
	}
	if code := requestJSON(t, s, http.MethodGet, "/api/chats/join-chat/tree", nil, &tree); code != http.StatusOK {
		t.Fatalf("tree = %d", code)
	}
	if tree.RootID != run.ChatID || tree.Workflow["name"] != "Fan" || len(tree.Nodes) != 2 || tree.Nodes[0].BotName != "Billy" || tree.Nodes[0].BotColor != "#123456" {
		t.Fatalf("tree = %+v", tree)
	}
	if join := tree.Nodes[1].Join; join == nil || join.Received != 1 || join.Expected != 2 {
		t.Fatalf("join = %+v", join)
	}
	if len(tree.Edges) != 1 || tree.Edges[0].From != run.ChatID || tree.Edges[0].Status != "pending" {
		t.Fatalf("edges = %+v", tree.Edges)
	}
	if code := requestJSON(t, s, http.MethodGet, "/api/chats/missing/tree", nil, nil); code != http.StatusNotFound {
		t.Fatalf("missing tree = %d", code)
	}
}

func TestFlowStatus(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	s := New(database, "local", false, "dev", root)
	defer s.Close()
	for _, test := range []struct {
		name, setup, want string
	}{
		{"idle", ``, "idle"},
		{"error", `INSERT INTO chat_message(id,chat_id,role,content,created_at) VALUES('m',$id,'error','boom','9')`, "error"},
		{"waiting", `INSERT INTO chat(id,title,created_at,updated_at) VALUES('child','c','1','1'); INSERT INTO chat_handoff(id,from_chat_id,to_chat_id,created_at,deadline_at) VALUES('h',$id,'child','1','2')`, "waiting"},
		{"queued", `INSERT INTO chat_queue(id,chat_id,text,attachments,created_at) VALUES('q',$id,'x','[]','1')`, "queued"},
		{"held", `INSERT INTO chat_queue(id,chat_id,text,attachments,created_at) VALUES('q2',$id,'x','[]','1'); UPDATE chat SET queue_held=1 WHERE id=$id`, "held"},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := createDriverTestChat(t, database, connectorID, root)
			for _, statement := range strings.Split(test.setup, ";") {
				if strings.TrimSpace(statement) == "" {
					continue
				}
				if _, err := database.Exec(strings.ReplaceAll(statement, "$id", "'"+id+"'")); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := s.flowStatus(id); err != nil || got != test.want {
				t.Fatalf("flowStatus = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}
