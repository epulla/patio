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
)

func TestLocalChatPlainAndThinkStripping(t *testing.T) {
	database, root, chatID, handler, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
		if request["stream"] != true || request["model"] != "qwen3:4b" {
			t.Errorf("completion request = %#v", request)
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
			handler: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
				writeSSE(w,
					`{"choices":[{"delta":{"content":"before do-not-"}}]}`,
					`{"choices":[{"delta":{"content":"store-token after"}}]}`,
				)
			},
		},
		{
			name: "upstream error",
			handler: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
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
				if event["message"] == localToolNote {
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
