package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"
	"my-tickets/internal/filetools"
)

func TestLocalChatPlainAndThinkStripping(t *testing.T) {
	database, root, chatID, handler, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		if request["stream"] != true || request["model"] != "qwen3:4b" {
			t.Errorf("completion request = %#v", request)
		}
		messages, _ := request["messages"].([]any)
		last, _ := messages[len(messages)-1].(map[string]any)
		if last["content"] != "reply /no_think" {
			t.Errorf("fallback user message = %#v", last)
		}
		writeSSE(w,
			`{"choices":[{"delta":{"content":"OK<th"}}]}`,
			`{"choices":[{"delta":{"content":"ink>private reasoning</th"}}]}`,
			`{"choices":[{"delta":{"content":"ink> safe"},"reasoning_content":"must not show"}]}`,
		)
	})
	defer handler.Close()
	if code := sendLocalMessage(t, handler, chatID, "reply"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if got := localAssistant(t, database, chatID); got != "OK safe" {
		t.Fatalf("assistant = %q", got)
	}
	var userMessage string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='user'`, chatID).Scan(&userMessage); err != nil || userMessage != "reply" {
		t.Fatalf("stored user message = %q, %v", userMessage, err)
	}
	if _, err := os.Stat(filepath.Join(root, "chats", chatID)); err != nil {
		t.Fatal(err)
	}

}

func TestLocalChatRedactsConnectorSecretFromMessages(t *testing.T) {
	const secret = "do-not-store-token"
	for _, tc := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request, map[string]any)
	}{
		{
			name: "streamed response split across chunks",
			handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				if r.Header.Get("Authorization") != "Bearer "+secret {
					t.Errorf("OpenAI Authorization = %q", r.Header.Get("Authorization"))
				}
				writeSSE(w,
					`{"choices":[{"delta":{"content":"before do-not-"}}]}`,
					`{"choices":[{"delta":{"content":"store-token after"}}]}`,
				)
			},
		},
		{
			name: "upstream error",
			handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				if r.Header.Get("Authorization") != "Bearer "+secret {
					t.Errorf("OpenAI Authorization = %q", r.Header.Get("Authorization"))
				}
				w.WriteHeader(http.StatusBadGateway)
				_, _ = fmt.Fprintf(w, `{"error":"upstream rejected %s"}`, secret)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, _, chatID, server, events := newLocalChatFixture(t, tc.handler)
			defer server.Close()
			var connectorID string
			if err := database.QueryRow(`SELECT connector_id FROM chat WHERE id=?`, chatID).Scan(&connectorID); err != nil {
				t.Fatal(err)
			}
			if err := keyring.Set(connectorService, connectorSecret(connectorID), secret); err != nil {
				t.Fatal(err)
			}
			if code := sendLocalMessage(t, server, chatID, "reply"); code != http.StatusAccepted {
				t.Fatalf("send = %d", code)
			}
			deadline := time.After(5 * time.Second)
			for {
				select {
				case event := <-events:
					if eventType(event) == "delta" && strings.Contains(event["text"].(string), secret) {
						t.Fatal("stream exposed connector secret")
					}
					if eventType(event) == "done" {
						goto checkMessages
					}
				case <-deadline:
					t.Fatal("timed out waiting for local chat done")
				}
			}
		checkMessages:
			rows, err := database.Query(`SELECT content FROM chat_message WHERE chat_id=?`, chatID)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var content string
				if err := rows.Scan(&content); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(content, secret) {
					t.Fatalf("chat message stored connector secret: %q", content)
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLocalOllamaSSEFixture(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'local','Fixture',?)`, connectorID, now()); err != nil {
		t.Fatal(err)
	}
	chatID := createDriverTestChat(t, database, connectorID, root)
	manager := newChatManager(database, Options{Emit: func(string, any) {}})
	fixture, err := os.ReadFile(filepath.Join("testdata", "local-ollama-stream.sse"))
	if err != nil {
		t.Fatal(err)
	}
	driver := &localRuntime{manager: manager, chatID: chatID}
	result, err := driver.readSSE(context.Background(), strings.NewReader(string(fixture)))
	if err != nil || result.text != "OK" {
		t.Fatalf("Ollama fixture result = %#v, %v", result, err)
	}
}

func TestLocalChatReadToolLoopAndTicketNamespacedTool(t *testing.T) {
	var calls atomic.Int32
	database, root, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		call := calls.Add(1)
		encoded, _ := json.Marshal(request)
		if call == 1 {
			if !strings.Contains(string(encoded), `"name":"file__read_file"`) || !strings.Contains(string(encoded), `"name":"ticket__list_boards"`) {
				t.Errorf("namespaced tools missing: %s", encoded)
			}
			writeSSE(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"file__read_","arguments":"{\"path\":"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"file","arguments":"\"hello.txt\"}"}}]}}]}`,
			)
			return
		}
		messages, _ := request["messages"].([]any)
		if len(messages) < 4 {
			t.Errorf("tool result missing from next request: %#v", request)
		}
		last, _ := messages[len(messages)-1].(map[string]any)
		if last["role"] != "tool" || last["content"] != "hi from file" {
			t.Errorf("last message = %#v", last)
		}
		writeSSE(w, `{"choices":[{"delta":{"content":"Read it"}}]}`)
	})
	defer server.Close()
	if err := os.WriteFile(filepath.Join(root, "chats", chatID, "hello.txt"), []byte("hi from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := sendLocalMessage(t, server, chatID, "read hello.txt"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if got := localAssistant(t, database, chatID); got != "Read it" {
		t.Fatalf("assistant = %q", got)
	}
	var summary string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='tool'`, chatID).Scan(&summary); err != nil || !strings.Contains(summary, "file__read_file") {
		t.Fatalf("tool summary = %q, %v", summary, err)
	}
}

func TestLocalChatCallsTicketMCPTool(t *testing.T) {
	var calls atomic.Int32
	database, _, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		if calls.Add(1) == 1 {
			writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"boards-1","type":"function","function":{"name":"ticket__list_boards","arguments":"{}"}}]}}]}`)
			return
		}
		messages, _ := request["messages"].([]any)
		last, _ := messages[len(messages)-1].(map[string]any)
		if last["role"] != "tool" || last["content"] != "[]" {
			t.Errorf("ticket MCP result message = %#v", last)
		}
		writeSSE(w, `{"choices":[{"delta":{"content":"No boards"}}]}`)
	})
	defer server.Close()
	if code := sendLocalMessage(t, server, chatID, "list boards"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if got := localAssistant(t, database, chatID); got != "No boards" {
		t.Fatalf("assistant = %q", got)
	}
}

func TestLocalChatWriteApprovalAllowAndDeny(t *testing.T) {
	for _, allow := range []bool{true, false} {
		t.Run(fmt.Sprintf("allow_%v", allow), func(t *testing.T) {
			var calls atomic.Int32
			database, root, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
				if calls.Add(1) == 1 {
					writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"write-1","type":"function","function":{"name":"file__write_file","arguments":"{\"path\":\"hello.txt\",\"content\":\"hi\"}"}}]}}]}`)
					return
				}
				writeSSE(w, `{"choices":[{"delta":{"content":"Finished"}}]}`)
			})
			defer server.Close()
			if code := sendLocalMessage(t, server, chatID, "create file"); code != http.StatusAccepted {
				t.Fatalf("send = %d", code)
			}
			waitLocalEvent(t, events, "approval")
			var approvalID string
			if err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending'`, chatID).Scan(&approvalID); err != nil {
				t.Fatal(err)
			}
			code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": allow}, nil)
			if code != http.StatusOK {
				t.Fatalf("answer approval = %d", code)
			}
			waitLocalDone(t, events)
			_, err := os.Stat(filepath.Join(root, "chats", chatID, "hello.txt"))
			if allow && err != nil {
				t.Fatalf("approved file missing: %v", err)
			}
			if !allow && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("denied file exists or stat failed: %v", err)
			}
			if allow {
				if got, err := os.ReadFile(filepath.Join(root, "chats", chatID, "hello.txt")); err != nil || string(got) != "hi" {
					t.Fatalf("approved file = %q, %v", got, err)
				}
			} else {
				var denied bool
				rows, err := database.Query(`SELECT content FROM chat_message WHERE chat_id=? AND role='tool'`, chatID)
				if err != nil {
					t.Fatal(err)
				}
				for rows.Next() {
					var content string
					if err := rows.Scan(&content); err != nil {
						t.Fatal(err)
					}
					denied = denied || strings.Contains(content, "file__write_file")
				}
				if err := rows.Close(); err != nil {
					t.Fatal(err)
				}
				if !denied {
					t.Fatal("denied write tool was not recorded")
				}
			}
		})
	}
}

func TestLocalChatFallsBackWhenToolsUnsupported(t *testing.T) {
	var calls atomic.Int32
	database, _, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		if calls.Add(1) == 1 {
			if _, ok := request["tools"]; !ok {
				t.Error("first request omitted tools")
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":"model does not support tools"}`)
			return
		}
		if _, ok := request["tools"]; ok {
			t.Error("fallback request still included tools")
		}
		writeSSE(w, `{"choices":[{"delta":{"content":"Hello"}}]}`)
	})
	defer server.Close()
	if code := sendLocalMessage(t, server, chatID, "hello"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if calls.Load() != 2 {
		t.Fatalf("completion requests = %d", calls.Load())
	}
	var note string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='error'`, chatID).Scan(&note); err != nil || note != localToolNote {
		t.Fatalf("fallback note = %q, %v", note, err)
	}
}

func TestLocalChatStopsAfterTwentyToolCalls(t *testing.T) {
	var calls atomic.Int32
	database, _, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		calls.Add(1)
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"read","type":"function","function":{"name":"file__list_dir","arguments":"{}"}}]}}]}`)
	})
	defer server.Close()
	if code := sendLocalMessage(t, server, chatID, "list files repeatedly"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if calls.Load() != localMaxToolCalls {
		t.Fatalf("completion requests = %d", calls.Load())
	}
	if got := localAssistant(t, database, chatID); !strings.Contains(got, localToolLimitNote) {
		t.Fatalf("assistant limit note missing: %q", got)
	}
}

func TestLocalChatStopCancelsStreamingRequest(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	database, _, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"working\"}}]}\n\n")
		flusher.Flush()
		close(started)
		<-r.Context().Done()
		close(canceled)
	})
	defer server.Close()
	if code := sendLocalMessage(t, server, chatID, "wait"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not start")
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/stop", nil, nil); code != http.StatusNoContent {
		t.Fatalf("stop = %d", code)
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("request context was not canceled")
	}
	waitLocalEvent(t, events, "done")
	var assistants int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='assistant'`, chatID).Scan(&assistants); err != nil || assistants != 0 {
		t.Fatalf("assistant rows after stop = %d, %v", assistants, err)
	}
}

func TestLocalChatStopCancelsPendingApproval(t *testing.T) {
	database, root, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"write-1","type":"function","function":{"name":"file__write_file","arguments":"{\"path\":\"hello.txt\",\"content\":\"hi\"}"}}]}}]}`)
	})
	defer server.Close()
	if code := sendLocalMessage(t, server, chatID, "create file"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalEvent(t, events, "approval")
	var approvalID string
	if err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending'`, chatID).Scan(&approvalID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/stop", nil, nil); code != http.StatusNoContent {
		t.Fatalf("stop = %d", code)
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM chat_approval WHERE id=?`, approvalID).Scan(&status); err != nil || status != "expired" {
		t.Fatalf("approval status = %q, %v", status, err)
	}
	if _, err := os.Stat(filepath.Join(root, "chats", chatID, "hello.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file after stopped approval: %v", err)
	}
}

func newLocalChatFixture(t *testing.T, handler func(http.ResponseWriter, *http.Request, map[string]any)) (*sql.DB, string, string, *Server, <-chan map[string]any) {
	t.Helper()
	keyring.MockInit()
	database, _, root := chatAPI(t)
	openAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		handler(w, r, body)
	}))
	t.Cleanup(openAI.Close)
	connectorID := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,base_url,model,created_at) VALUES(?,'local','Local',?,'qwen3:4b',?)`, connectorID, openAI.URL+"/v1", now()); err != nil {
		t.Fatal(err)
	}
	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET model='qwen3:4b' WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	events := make(chan map[string]any, 512)
	server := New(database, "local", false, "test", root, Options{
		DBPath:     filepath.Join(root, "test.db"),
		mcpConnect: fakeLocalMCPConnect(),
		Emit:       func(_ string, event any) { events <- event.(map[string]any) },
	})
	t.Cleanup(func() { _ = server.Close() })
	return database, root, chatID, server, events
}

func fakeLocalMCPConnect() mcpConnector {
	return func(ctx context.Context, _ mcpServerSpec) (*mcp.ClientSession, func(), error) {
		server := mcp.NewServer(&mcp.Implementation{Name: "local-test", Version: "1"}, nil)
		server.AddTool(&mcp.Tool{Name: "list_boards", Description: "List boards", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "[]"}}}, nil
		})
		for _, tool := range fakeTicketTools() {
			if tool.Name == "list_boards" {
				continue
			}
			server.AddTool(tool, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
			})
		}
		clientTransport, serverTransport := mcp.NewInMemoryTransports()
		serverSession, err := server.Connect(ctx, serverTransport, nil)
		if err != nil {
			return nil, nil, err
		}
		client := mcp.NewClient(&mcp.Implementation{Name: "local-test-client", Version: "1"}, nil)
		session, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			_ = serverSession.Close()
			return nil, nil, err
		}
		cleanup := func() {
			_ = session.Close()
			_ = serverSession.Close()
		}
		return session, cleanup, nil
	}
}

func fakeTicketTools() []*mcp.Tool {
	names := []string{"list_boards", "get_board", "create_board", "create_ticket", "get_ticket", "update_ticket", "move_ticket", "search_tickets", "add_comment", "list_calendar", "create_calendar_item", "update_calendar_item", "delete_calendar_item", "list_skills", "load_skill"}
	tools := make([]*mcp.Tool, 0, len(names))
	for _, name := range names {
		properties := map[string]any{}
		for _, field := range []string{"id", "board_id", "title", "description", "starts_at"} {
			properties[field] = map[string]any{"type": "string", "description": "Optional value used to identify or describe the requested ticket, board, calendar item, or date."}
		}
		tools = append(tools, &mcp.Tool{
			Name: name, Description: "Perform the " + strings.ReplaceAll(name, "_", " ") + " operation for the current chat with validated input and return its result.",
			InputSchema: map[string]any{"type": "object", "properties": properties, "additionalProperties": false},
		})
	}
	return tools
}

func TestLocalToolJSONFits4096Context(t *testing.T) {
	var raw []map[string]any
	for _, tool := range fakeTicketTools() {
		raw = append(raw, map[string]any{"type": "function", "function": map[string]any{
			"name": "ticket__" + tool.Name, "description": tool.Description, "parameters": tool.InputSchema,
		}})
	}
	for _, tool := range filetools.Tools() {
		raw = append(raw, map[string]any{"type": "function", "function": map[string]any{
			"name": "file__" + tool.Name, "description": tool.Description, "parameters": json.RawMessage(tool.Parameters),
		}})
	}
	before, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	compact := make([]map[string]any, 0, len(raw))
	for _, tool := range raw {
		function := tool["function"].(map[string]any)
		definition, err := localToolDefinition(function["name"].(string), function["description"].(string), function["parameters"])
		if err != nil {
			t.Fatal(err)
		}
		compact = append(compact, definition)
	}
	compact, err = compactLocalTools(compact)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(compact)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("tool JSON bytes: before=%d after=%d tools=%d", len(before), len(after), len(compact))
	if len(after) >= localToolsMaxBytes {
		t.Fatalf("local tool JSON = %d bytes, want under %d", len(after), localToolsMaxBytes)
	}
	prompt := localSystemPrompt("scratch", true)
	for _, example := range []string{`"Hi" → "Hi!" (no tool)`, `call file__list_dir`, `call ticket__list_boards`} {
		if !strings.Contains(prompt, example) {
			t.Errorf("system prompt missing example %q: %s", example, prompt)
		}
	}
	if len(after)+len(prompt) >= localToolsMaxBytes {
		t.Fatalf("tool JSON plus prompt = %d bytes, want under %d", len(after)+len(prompt), localToolsMaxBytes)
	}
}

func TestLocalChatToolsDisabledOpenAI(t *testing.T) {
	database, _, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		if _, exists := request["tools"]; exists {
			t.Error("tools-disabled OpenAI request included tools")
		}
		messages, _ := request["messages"].([]any)
		system, _ := messages[0].(map[string]any)
		prompt, _ := system["content"].(string)
		if !strings.Contains(prompt, "Tools unavailable") || strings.Contains(prompt, "call file__list_dir") {
			t.Errorf("tools-disabled system prompt = %q", prompt)
		}
		writeSSE(w, `{"choices":[{"delta":{"content":"Hello"}}]}`)
	})
	defer server.Close()
	var updated chat
	if code := requestJSON(t, server, http.MethodPut, "/api/chats/"+chatID, map[string]bool{"tools": false}, &updated); code != http.StatusOK || updated.Tools {
		t.Fatalf("disable tools = %d, %#v", code, updated)
	}
	if code := sendLocalMessage(t, server, chatID, "hello"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if localAssistant(t, database, chatID) != "Hello" {
		t.Fatal("tools-disabled reply missing")
	}
}

func TestLocalChatToolsDisabledOllama(t *testing.T) {
	database, _, chatID, server, events, _, _ := newOllamaChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		if _, exists := request["tools"]; exists {
			t.Error("tools-disabled Ollama request included tools")
		}
		messages, _ := request["messages"].([]any)
		system, _ := messages[0].(map[string]any)
		prompt, _ := system["content"].(string)
		if !strings.Contains(prompt, "Tools unavailable") || strings.Contains(prompt, "call ticket__list_boards") {
			t.Errorf("tools-disabled system prompt = %q", prompt)
		}
		writeNDJSON(w, `{"message":{"content":"Hello"},"done":true}`)
	})
	if code := requestJSON(t, server, http.MethodPut, "/api/chats/"+chatID, map[string]bool{"tools": false}, nil); code != http.StatusOK {
		t.Fatalf("disable tools = %d", code)
	}
	if code := sendLocalMessage(t, server, chatID, "hello"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if localAssistant(t, database, chatID) != "Hello" {
		t.Fatal("tools-disabled reply missing")
	}
}

func TestLocalHistoryUsesNewestMessagesWithinBudget(t *testing.T) {
	database, _, _ := chatAPI(t)
	chatID := uid()
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES(?,?,?,?)`, chatID, "History", now(), now()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 45; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		content := fmt.Sprintf("%02d %s", i, strings.Repeat("x", 200))
		if _, err := database.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at) VALUES(?,?,?,?,?)`, uid(), chatID, role, content, fmt.Sprintf("%02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at) VALUES(?,?,?,?,?)`, uid(), chatID, "user", "current turn", "99"); err != nil {
		t.Fatal(err)
	}
	driver := &localRuntime{manager: newChatManager(database, Options{}), chatID: chatID}
	messages, err := driver.history()
	if err != nil {
		t.Fatal(err)
	}
	var historyChars int
	for _, message := range messages[1:] {
		content := message["content"].(string)
		historyChars += len(content)
	}
	if historyChars > localHistoryBudget || len(messages) > localHistoryLimit+1 {
		t.Fatalf("history chars/messages = %d/%d", historyChars, len(messages))
	}
	if messages[len(messages)-1]["content"] != "current turn" {
		t.Fatalf("latest user message missing or out of order: %#v", messages[len(messages)-1])
	}
}

func TestLocalHistoryPromptNamesSharedFolder(t *testing.T) {
	database, _, _ := chatAPI(t)
	chatID := uid()
	if _, err := database.Exec(`INSERT INTO chat(id,title,folder,created_at,updated_at) VALUES(?,?,?, ?, ?)`, chatID, "History", "/allowed/projects/ProjectFolder", now(), now()); err != nil {
		t.Fatal(err)
	}
	driver := &localRuntime{manager: newChatManager(database, Options{}), chatID: chatID}
	messages, err := driver.history()
	if err != nil {
		t.Fatal(err)
	}
	prompt := messages[0]["content"].(string)
	if !strings.Contains(prompt, "ProjectFolder") || strings.Contains(prompt, "/allowed/projects") {
		t.Fatalf("shared-folder system prompt = %q", prompt)
	}
}

func writeSSE(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, chunk := range chunks {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func sendLocalMessage(t *testing.T, server *Server, chatID, text string) int {
	t.Helper()
	return requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": text}, nil)
}

func waitLocalDone(t *testing.T, events <-chan map[string]any) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-events:
			if eventType(event) == "error" {
				if event["message"] == localToolNote || event["message"] == localContextNote {
					continue
				}
				t.Fatalf("local runtime error: %v", event["message"])
			}
			if eventType(event) == "done" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for local chat done")
		}
	}
}

func waitLocalEvent(t *testing.T, events <-chan map[string]any, wanted string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-events:
			if eventType(event) == wanted {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for local chat %s", wanted)
		}
	}
}

func localAssistant(t *testing.T, database *sql.DB, chatID string) string {
	t.Helper()
	var content string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='assistant' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&content); err != nil {
		t.Fatal(err)
	}
	return content
}

func TestLocalChatEmptyModelUsesConnectorModel(t *testing.T) {
	database, _, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		if request["model"] != "qwen3:4b" {
			t.Errorf("completion model = %#v, want connector model", request["model"])
		}
		writeSSE(w, `{"choices":[{"delta":{"content":"OK"}}]}`)
	})
	defer server.Close()
	if _, err := database.Exec(`UPDATE chat SET model='' WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	if code := sendLocalMessage(t, server, chatID, "reply"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if got := localAssistant(t, database, chatID); got != "OK" {
		t.Fatalf("assistant = %q", got)
	}
}

func TestOpenAIFallbackThinkingChangesNextTurnWithoutRestart(t *testing.T) {
	var calls atomic.Int32
	database, _, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		messages, _ := request["messages"].([]any)
		last, _ := messages[len(messages)-1].(map[string]any)
		if _, exists := request["think"]; exists {
			t.Error("OpenAI-compatible request included native think field")
		}
		if calls.Add(1) == 1 && last["content"] != "first /no_think" {
			t.Errorf("thinking-off user content = %#v", last["content"])
		}
		if calls.Load() == 2 && last["content"] != "second" {
			t.Errorf("thinking-on user content = %#v", last["content"])
		}
		writeSSE(w, `{"choices":[{"delta":{"content":"OK"}}]}`)
	})
	defer server.Close()
	if code := sendLocalMessage(t, server, chatID, "first"); code != http.StatusAccepted {
		t.Fatalf("first send = %d", code)
	}
	waitLocalDone(t, events)
	var updated chat
	if code := requestJSON(t, server, http.MethodPut, "/api/chats/"+chatID, map[string]bool{"thinking": true}, &updated); code != http.StatusOK || !updated.Thinking {
		t.Fatalf("enable thinking = %d, %#v", code, updated)
	}
	if code := sendLocalMessage(t, server, chatID, "second"); code != http.StatusAccepted {
		t.Fatalf("second send = %d", code)
	}
	waitLocalDone(t, events)
	if calls.Load() != 2 {
		t.Fatalf("completion requests = %d", calls.Load())
	}
	var stored string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='user' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&stored); err != nil || stored != "second" {
		t.Fatalf("stored user message = %q, %v", stored, err)
	}
	var versionCalls int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND content LIKE '%/no_think%'`, chatID).Scan(&versionCalls); err != nil || versionCalls != 0 {
		t.Fatalf("stored /no_think messages = %d, %v", versionCalls, err)
	}
}

func newOllamaChatFixture(t *testing.T, handler func(http.ResponseWriter, *http.Request, map[string]any)) (*sql.DB, string, string, *Server, <-chan map[string]any, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var versionCalls, showCalls atomic.Int32
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			versionCalls.Add(1)
			_, _ = fmt.Fprint(w, `{"version":"test"}`)
		case "/api/ps":
			_, _ = fmt.Fprint(w, `{"models":[{"name":"qwen2.5:3b","context_length":4096}]}`)
		case "/api/show":
			showCalls.Add(1)
			_, _ = fmt.Fprint(w, `{"model_info":{"qwen.context_length":32768}}`)
		case "/api/chat":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode Ollama request: %v", err)
				return
			}
			handler(w, r, body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ollama.Close)
	database, root, chatID, server, events := newLocalChatFixture(t, func(http.ResponseWriter, *http.Request, map[string]any) {})
	var connectorID string
	if err := database.QueryRow(`SELECT connector_id FROM chat WHERE id=?`, chatID).Scan(&connectorID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE connector SET base_url=? WHERE id=?`, ollama.URL, connectorID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE chat SET model='qwen2.5:3b' WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	return database, root, chatID, server, events, &versionCalls, &showCalls
}

func writeNDJSON(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	for _, chunk := range chunks {
		_, _ = fmt.Fprintln(w, chunk)
	}
}

func TestLocalOllamaNativeToolLoopThinkingAndContextNote(t *testing.T) {
	var calls atomic.Int32
	database, root, chatID, server, events, versionCalls, showCalls := newOllamaChatFixture(t, func(w http.ResponseWriter, r *http.Request, request map[string]any) {
		if r.Header.Get("Authorization") != "Bearer native-secret" {
			t.Errorf("native Authorization = %q", r.Header.Get("Authorization"))
		}
		if _, exists := request["num_ctx"]; exists {
			t.Error("native request set num_ctx")
		}
		switch calls.Add(1) {
		case 1:
			if request["think"] != false {
				t.Errorf("default native think = %#v", request["think"])
			}
			tools, _ := request["tools"].([]any)
			encoded, _ := json.Marshal(tools)
			if len(tools) != 24 || len(encoded) >= localToolsMaxBytes {
				t.Errorf("native tool definitions count/bytes = %d/%d", len(tools), len(encoded))
			}
			writeNDJSON(w,
				`{"message":{"thinking":"private reasoning","content":"Reading "},"done":false}`,
				`{"message":{"tool_calls":[{"function":{"name":"file__read_file","arguments":{"path":"hello.txt"}}}]},"done":false}`,
				`{"done":true,"prompt_eval_count":4096}`,
			)
		case 2:
			messages, _ := request["messages"].([]any)
			var assistantCall, toolResult bool
			for _, raw := range messages {
				message, _ := raw.(map[string]any)
				switch message["role"] {
				case "assistant":
					calls, _ := message["tool_calls"].([]any)
					if len(calls) > 0 {
						call, _ := calls[0].(map[string]any)
						function, _ := call["function"].(map[string]any)
						args, _ := function["arguments"].(map[string]any)
						assistantCall = args["path"] == "hello.txt"
					}
				case "tool":
					toolResult = message["tool_name"] == "file__read_file" && message["content"] == "hello content"
					if _, exists := message["tool_call_id"]; exists {
						t.Error("native tool result included OpenAI tool_call_id")
					}
				}
			}
			if !assistantCall || !toolResult {
				t.Errorf("native tool history assistant=%v result=%v messages=%#v", assistantCall, toolResult, messages)
			}
			writeNDJSON(w, `{"message":{"content":"Found it"},"done":true,"prompt_eval_count":4096}`)
		case 3:
			if request["think"] != true {
				t.Errorf("updated native think = %#v", request["think"])
			}
			writeNDJSON(w,
				`{"message":{"thinking":"hidden", "content":"safe native-"},"done":false}`,
				`{"message":{"content":"secret end"},"done":true,"prompt_eval_count":4096}`,
			)
		}
	})
	var connectorID string
	if err := database.QueryRow(`SELECT connector_id FROM chat WHERE id=?`, chatID).Scan(&connectorID); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(connectorService, connectorSecret(connectorID), "native-secret"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "chats", chatID, "hello.txt"), []byte("hello content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := sendLocalMessage(t, server, chatID, "read file"); code != http.StatusAccepted {
		t.Fatalf("native send = %d", code)
	}
	waitLocalDone(t, events)
	if got := localAssistant(t, database, chatID); got != "Reading Found it" {
		rows, _ := database.Query(`SELECT role,content FROM chat_message WHERE chat_id=? ORDER BY rowid`, chatID)
		defer rows.Close()
		for rows.Next() {
			var role, content string
			_ = rows.Scan(&role, &content)
			t.Logf("message %s: %q", role, content)
		}
		t.Fatalf("native assistant = %q, completion calls=%d", got, calls.Load())
	}
	var thinkingMessages int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND (content LIKE '%private reasoning%' OR content LIKE '%hidden%')`, chatID).Scan(&thinkingMessages); err != nil || thinkingMessages != 0 {
		t.Fatalf("hidden thinking messages = %d, %v", thinkingMessages, err)
	}
	var contextNotes int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='error' AND content=?`, chatID, localContextNote).Scan(&contextNotes); err != nil || contextNotes != 1 {
		t.Fatalf("context note count = %d, %v", contextNotes, err)
	}
	var updated chat
	if code := requestJSON(t, server, http.MethodPut, "/api/chats/"+chatID, map[string]bool{"thinking": true}, &updated); code != http.StatusOK || !updated.Thinking {
		t.Fatalf("enable Ollama thinking = %d, %#v", code, updated)
	}
	if code := sendLocalMessage(t, server, chatID, "second turn"); code != http.StatusAccepted {
		t.Fatalf("second native send = %d", code)
	}
	waitLocalDone(t, events)
	if calls.Load() != 3 || versionCalls.Load() != 1 || showCalls.Load() != 1 {
		t.Fatalf("native calls/version/show = %d/%d/%d", calls.Load(), versionCalls.Load(), showCalls.Load())
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='error' AND content=?`, chatID, localContextNote).Scan(&contextNotes); err != nil || contextNotes != 2 {
		t.Fatalf("per-turn context notes = %d, %v", contextNotes, err)
	}
	if got := localAssistant(t, database, chatID); got != "safe [redacted] end" {
		t.Fatalf("native secret redaction = %q", got)
	}
	var leaked int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND content LIKE '%native-secret%'`, chatID).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("native secret-bearing messages = %d, %v", leaked, err)
	}
}

func TestLocalOllamaNativeFallsBackWithoutTools(t *testing.T) {
	var calls atomic.Int32
	database, _, chatID, server, events, _, _ := newOllamaChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		if calls.Add(1) == 1 {
			if _, ok := request["tools"]; !ok {
				t.Error("first native request omitted tools")
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":"model does not support tools"}`)
			return
		}
		if _, ok := request["tools"]; ok {
			t.Error("native fallback request still included tools")
		}
		writeNDJSON(w, `{"message":{"content":"Hello"},"done":true}`)
	})
	if code := sendLocalMessage(t, server, chatID, "hello"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if calls.Load() != 2 || localAssistant(t, database, chatID) != "Hello" {
		t.Fatalf("native fallback requests/reply = %d/%q", calls.Load(), localAssistant(t, database, chatID))
	}
	var note string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='error'`, chatID).Scan(&note); err != nil || note != localToolNote {
		t.Fatalf("native fallback note = %q, %v", note, err)
	}
}

func TestLocalOllamaNativeWriteApprovalAllowAndDeny(t *testing.T) {
	for _, allow := range []bool{true, false} {
		t.Run(fmt.Sprintf("allow_%v", allow), func(t *testing.T) {
			var calls atomic.Int32
			database, root, chatID, server, events, _, _ := newOllamaChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
				if calls.Add(1) == 1 {
					writeNDJSON(w, `{"message":{"tool_calls":[{"function":{"name":"file__write_file","arguments":{"path":"hello.txt","content":"hi"}}}]},"done":true}`)
					return
				}
				writeNDJSON(w, `{"message":{"content":"Finished"},"done":true}`)
			})
			if code := sendLocalMessage(t, server, chatID, "create file"); code != http.StatusAccepted {
				t.Fatalf("send = %d", code)
			}
			waitLocalEvent(t, events, "approval")
			var approvalID string
			if err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending'`, chatID).Scan(&approvalID); err != nil {
				t.Fatal(err)
			}
			if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": allow}, nil); code != http.StatusOK {
				t.Fatalf("answer approval = %d", code)
			}
			waitLocalDone(t, events)
			_, err := os.Stat(filepath.Join(root, "chats", chatID, "hello.txt"))
			if allow && err != nil {
				t.Fatalf("approved file missing: %v", err)
			}
			if !allow && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("denied file exists or stat failed: %v", err)
			}
		})
	}
}

func TestLocalOllamaTextJSONToolCallStoresHiddenCappedResult(t *testing.T) {
	var calls atomic.Int32
	database, root, chatID, server, events, _, _ := newOllamaChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		switch calls.Add(1) {
		case 1:
			writeNDJSON(w, "{\"message\":{\"content\":\"```json\\n{\\\"name\\\":\\\"read_file\\\",\\\"arguments\\\":{\\\"path\\\":\\\"large.txt\\\"}}\\n```\"},\"done\":true}")
		case 2:
			messages, _ := request["messages"].([]any)
			last, _ := messages[len(messages)-1].(map[string]any)
			if last["role"] != "tool" || last["content"] != strings.Repeat("x", 3000) {
				t.Errorf("tool result sent to model = %#v", last)
			}
			writeNDJSON(w, `{"message":{"content":"Read it"},"done":true}`)
		default:
			t.Errorf("unexpected completion request %d", calls.Load())
			writeNDJSON(w, `{"message":{"content":"unexpected"},"done":true}`)
		}
	})
	if err := os.WriteFile(filepath.Join(root, "chats", chatID, "large.txt"), []byte(strings.Repeat("x", 3000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := sendLocalMessage(t, server, chatID, "read large.txt"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if got := localAssistant(t, database, chatID); got != "Read it" {
		t.Fatalf("assistant = %q", got)
	}
	var result string
	if err := database.QueryRow(`SELECT tool_result FROM chat_message WHERE chat_id=? AND role='tool'`, chatID).Scan(&result); err != nil {
		t.Fatal(err)
	}
	if len(result) > localStoredToolMax || !strings.HasSuffix(result, "[tool result truncated]") {
		t.Fatalf("stored tool result length/suffix = %d/%q", len(result), result[len(result)-min(len(result), 40):])
	}
	var detail map[string]any
	if code := requestJSON(t, server, http.MethodGet, "/api/chats/"+chatID, nil, &detail); code != http.StatusOK {
		t.Fatalf("get chat = %d", code)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), strings.Repeat("x", 100)) || strings.Contains(string(encoded), "tool_result") {
		t.Fatalf("private tool result exposed by chat API: %s", encoded)
	}
}

func TestLocalOllamaTextJSONWriteNeedsApproval(t *testing.T) {
	var calls atomic.Int32
	database, root, chatID, server, events, _, _ := newOllamaChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		if calls.Add(1) == 1 {
			writeNDJSON(w, `{"message":{"content":"{\"name\":\"file__write_file\",\"parameters\":{\"path\":\"approved.txt\",\"content\":\"ok\"}}"},"done":true}`)
			return
		}
		writeNDJSON(w, `{"message":{"content":"Finished"},"done":true}`)
	})
	if code := sendLocalMessage(t, server, chatID, "write approved.txt"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalEvent(t, events, "approval")
	var approvalID string
	if err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending'`, chatID).Scan(&approvalID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": true}, nil); code != http.StatusOK {
		t.Fatalf("answer approval = %d", code)
	}
	waitLocalDone(t, events)
	if data, err := os.ReadFile(filepath.Join(root, "chats", chatID, "approved.txt")); err != nil || string(data) != "ok" {
		t.Fatalf("approved write = %q, %v", data, err)
	}
}

func TestLocalOllamaEmptyJSONRetriesWithoutTools(t *testing.T) {
	var calls atomic.Int32
	database, _, chatID, server, events, _, _ := newOllamaChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		if calls.Add(1) == 1 {
			if _, ok := request["tools"]; !ok {
				t.Error("first request omitted tools")
			}
			writeNDJSON(w, `{"message":{"content":"{}"},"done":true}`)
			return
		}
		if _, ok := request["tools"]; ok {
			t.Error("retry included tools")
		}
		writeNDJSON(w, `{"message":{"content":"Hello from retry"},"done":true}`)
	})
	if code := sendLocalMessage(t, server, chatID, "hello"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if calls.Load() != 2 || localAssistant(t, database, chatID) != "Hello from retry" {
		t.Fatalf("requests/reply = %d/%q", calls.Load(), localAssistant(t, database, chatID))
	}
	var emptyReplies int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='assistant' AND content='{}'`, chatID).Scan(&emptyReplies); err != nil || emptyReplies != 0 {
		t.Fatalf("stored empty JSON replies = %d, %v", emptyReplies, err)
	}
}

func TestLocalOllamaStillInvalidAfterRetryStoresShortNote(t *testing.T) {
	var calls atomic.Int32
	database, _, chatID, server, events, _, _ := newOllamaChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		calls.Add(1)
		writeNDJSON(w, `{"message":{"content":"{}"},"done":true}`)
	})
	if code := sendLocalMessage(t, server, chatID, "hello"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if calls.Load() != 2 || localAssistant(t, database, chatID) != localInvalidReply {
		t.Fatalf("requests/reply = %d/%q", calls.Load(), localAssistant(t, database, chatID))
	}
}

func TestLocalOllamaGreetingUsesPlainTextAndFolderPrompt(t *testing.T) {
	var calls atomic.Int32
	database, _, chatID, server, events, _, _ := newOllamaChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		calls.Add(1)
		messages, _ := request["messages"].([]any)
		system, _ := messages[0].(map[string]any)
		prompt, _ := system["content"].(string)
		if !strings.Contains(prompt, "a private scratch folder") || !strings.Contains(prompt, time.Now().In(time.Local).Format("2006-01-02")) || !strings.Contains(prompt, "Reply normally") {
			t.Errorf("system prompt = %q", prompt)
		}
		writeNDJSON(w, `{"message":{"content":"Hi! Nice to meet you."},"done":true}`)
	})
	if code := sendLocalMessage(t, server, chatID, "Hi!"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	if calls.Load() != 1 || localAssistant(t, database, chatID) != "Hi! Nice to meet you." {
		t.Fatalf("requests/reply = %d/%q", calls.Load(), localAssistant(t, database, chatID))
	}
	var toolMessages int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='tool'`, chatID).Scan(&toolMessages); err != nil || toolMessages != 0 {
		t.Fatalf("tool messages = %d, %v", toolMessages, err)
	}
}

func TestLocalOllamaToolErrorsStartWithError(t *testing.T) {
	var calls atomic.Int32
	database, _, chatID, server, events, _, _ := newOllamaChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		if calls.Add(1) == 1 {
			writeNDJSON(w, `{"message":{"tool_calls":[{"function":{"name":"file__read_file","arguments":{"path":"missing.txt"}}}]},"done":true}`)
			return
		}
		messages, _ := request["messages"].([]any)
		last, _ := messages[len(messages)-1].(map[string]any)
		if last["role"] != "tool" || !strings.HasPrefix(last["content"].(string), "Error: ") {
			t.Errorf("tool error result = %#v", last)
		}
		writeNDJSON(w, `{"message":{"content":"File missing"},"done":true}`)
	})
	if code := sendLocalMessage(t, server, chatID, "read missing.txt"); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	waitLocalDone(t, events)
	var result string
	if err := database.QueryRow(`SELECT tool_result FROM chat_message WHERE chat_id=? AND role='tool'`, chatID).Scan(&result); err != nil || !strings.HasPrefix(result, "Error: ") {
		t.Fatalf("stored tool error result = %q, %v", result, err)
	}
}
