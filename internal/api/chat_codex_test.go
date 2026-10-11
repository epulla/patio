package api

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	connectorcli "my-tickets/internal/connector"
)

func TestCodexTurnOverPipes(t *testing.T) {
	for _, allow := range []bool{true, false} {
		t.Run(map[bool]string{true: "allow", false: "deny"}[allow], func(t *testing.T) {
			database, _, root := chatAPI(t)
			connectorID := createCodexTestConnector(t, database)
			chatID := createDriverTestChat(t, database, connectorID, root)
			if allow {
				if _, err := database.Exec(`UPDATE chat SET thinking=1 WHERE id=?`, chatID); err != nil {
					t.Fatal(err)
				}
			}
			var mu sync.Mutex
			var events []map[string]any
			serverErr := make(chan error, 1)
			gotApproval := make(chan string, 1)
			server := New(database, "local", false, "test-version", root, Options{
				DBPath:    filepath.Join(root, "test.db"),
				findCodex: func() (string, error) { return "codex-fixture", nil },
				Emit: func(_ string, event any) {
					mu.Lock()
					events = append(events, event.(map[string]any))
					mu.Unlock()
					if event.(map[string]any)["type"] == "approval" {
						gotApproval <- "approval"
					}
				},
				codexStart: func(ctx context.Context, path string, env []string, isolated bool, version, cwd string, overrides ...string) (codexClient, error) {
					if path != "codex-fixture" || isolated || version != "test-version" || cwd != filepath.Join(root, "chats", chatID) || len(env) == 0 || len(overrides) != 0 {
						return nil, fmt.Errorf("Codex launch = %q %v %v %q %q %#v", path, env, isolated, version, cwd, overrides)
					}
					client, err := pipeCodexClient(ctx, func(read *bufio.Reader, write io.Writer) error {
						request, err := readCodexRequest(read, "initialize")
						if err != nil {
							return err
						}
						if err := writeCodexResponse(write, request.ID, map[string]any{}); err != nil {
							return err
						}
						if err := expectCodexNotification(read, "initialized"); err != nil {
							return err
						}
						request, err = readCodexRequest(read, "thread/start")
						if err != nil {
							return err
						}
						var params map[string]any
						if err := json.Unmarshal(request.Params, &params); err != nil {
							return err
						}
						if params["cwd"] != cwd || params["approvalPolicy"] != "on-request" || params["sandbox"] != "read-only" || params["model"] != nil {
							return fmt.Errorf("thread/start params = %#v", params)
						}
						config, _ := params["config"].(map[string]any)
						servers, _ := config["mcp_servers"].(map[string]any)
						if len(servers) != 3 || servers["my-tickets"] == nil || servers["disabled-test"] != nil {
							return fmt.Errorf("thread/start missing MCP config: %#v", config)
						}
						stdio, _ := servers["stdio-test"].(map[string]any)
						stdioEnv, _ := stdio["env"].(map[string]any)
						if stdio["command"] == nil || stdioEnv["TOKEN"] != "stdio-secret" {
							return fmt.Errorf("Codex stdio config = %#v", stdio)
						}
						httpServer, _ := servers["http-test"].(map[string]any)
						headers, _ := httpServer["http_headers"].(map[string]any)
						if httpServer["url"] != "https://mcp.example.test/mcp" || headers["Authorization"] != "Bearer http-secret" {
							return fmt.Errorf("Codex HTTP config = %#v", httpServer)
						}
						if err := writeCodexResponse(write, request.ID, map[string]any{"thread": map[string]string{"id": "thread-fixture"}}); err != nil {
							return err
						}
						if !allow {
							request, err = readCodexRequest(read, "model/list")
							if err != nil {
								return err
							}
							if err := writeCodexResponse(write, request.ID, map[string]any{"data": []any{map[string]any{"id": "gpt-5-codex", "isDefault": true, "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "high"}, map[string]string{"reasoningEffort": "low"}}}}}); err != nil {
								return err
							}
						}
						request, err = readCodexRequest(read, "turn/start")
						if err != nil {
							return err
						}
						var turnParams map[string]any
						if err := json.Unmarshal(request.Params, &turnParams); err != nil {
							return err
						}
						if turnParams["threadId"] != "thread-fixture" {
							return fmt.Errorf("turn/start params = %#v", turnParams)
						}
						wantEffort := any("low")
						if allow {
							wantEffort = nil
						}
						if turnParams["effort"] != wantEffort {
							return fmt.Errorf("turn/start effort = %#v, want %#v", turnParams["effort"], wantEffort)
						}
						if err := writeCodexResponse(write, request.ID, map[string]any{"turn": map[string]string{"id": "turn-fixture"}}); err != nil {
							return err
						}
						for _, message := range []string{
							`{"method":"item/agentMessage/delta","params":{"threadId":"thread-fixture","turnId":"turn-fixture","delta":"OK"}}`,
							`{"method":"item/started","params":{"item":{"id":"item-fixture","type":"commandExecution","command":"touch hello.txt"}}}`,
							`{"method":"item/commandExecution/requestApproval","id":90,"params":{"threadId":"thread-fixture","turnId":"turn-fixture","itemId":"item-fixture","command":"touch hello.txt"}}`,
						} {
							if _, err := io.WriteString(write, message+"\n"); err != nil {
								return err
							}
						}
						var response struct {
							ID     int `json:"id"`
							Result struct {
								Decision string `json:"decision"`
							} `json:"result"`
						}
						if err := json.NewDecoder(read).Decode(&response); err != nil {
							return err
						}
						if response.ID != 90 || response.Result.Decision != map[bool]string{true: "accept", false: "decline"}[allow] {
							return fmt.Errorf("approval response = %#v", response)
						}
						for _, message := range []string{
							`{"method":"item/completed","params":{"item":{"id":"item-fixture","type":"commandExecution","command":"touch hello.txt"}}}`,
							`{"method":"account/rateLimits/updated","params":{"rateLimits":{"primary":{"usedPercent":25,"windowDurationMins":300,"resetsAt":1760000000},"secondary":{"usedPercent":10,"windowDurationMins":10080,"resetsAt":1760200000}}}}`,
							`{"method":"turn/completed","params":{"turn":{"id":"turn-fixture","status":"completed"}}}`,
						} {
							if _, err := io.WriteString(write, message+"\n"); err != nil {
								return err
							}
						}
						return nil
					})
					if err != nil {
						return nil, err
					}
					go func() { serverErr <- <-client.serverDone }()
					return client.client, nil
				},
			})
			addChatMCPTestServers(t, server)
			server.chats.approvalPollInterval = time.Millisecond
			if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "make file"}, nil); code != http.StatusAccepted {
				t.Fatalf("send status = %d", code)
			}
			select {
			case <-gotApproval:
			case <-time.After(time.Second):
				t.Fatal("approval was not requested")
			}
			var approvalID string
			waitFor(t, func() bool {
				return database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=?`, chatID).Scan(&approvalID) == nil
			})
			if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": allow}, nil); code != http.StatusOK {
				t.Fatalf("answer approval = %d", code)
			}
			waitFor(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				for _, event := range events {
					if event["type"] == "done" {
						return true
					}
				}
				return false
			})
			if err := <-serverErr; err != nil {
				t.Fatal(err)
			}
			var messages []ChatMessage
			rows, err := database.Query(`SELECT id,chat_id,role,content,created_at FROM chat_message WHERE chat_id=? ORDER BY rowid`, chatID)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var message ChatMessage
				if err := rows.Scan(&message.ID, &message.ChatID, &message.Role, &message.Content, &message.CreatedAt); err != nil {
					t.Fatal(err)
				}
				messages = append(messages, message)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
			if len(messages) != 3 || messages[0].Role != "user" || messages[1].Role != "tool" || messages[1].Content != "commandExecution touch hello.txt" || messages[2].Role != "assistant" || messages[2].Content != "OK" {
				t.Fatalf("messages = %#v", messages)
			}
			var session string
			if err := database.QueryRow(`SELECT session_id FROM chat WHERE id=?`, chatID).Scan(&session); err != nil || session != "thread-fixture" {
				t.Fatalf("session = %q, %v", session, err)
			}
			var quota connectorcli.Quota
			var quotaJSON string
			if err := database.QueryRow(`SELECT quota_json FROM connector WHERE id=?`, connectorID).Scan(&quotaJSON); err != nil || json.Unmarshal([]byte(quotaJSON), &quota) != nil || quota.Kind != "codex" || len(quota.Windows) != 2 || quota.Windows[0].Label != "5h" {
				t.Fatalf("quota = %s, %v", quotaJSON, err)
			}
			mu.Lock()
			defer mu.Unlock()
			var types []string
			for _, event := range events {
				types = append(types, event["type"].(string))
			}
			if strings.Join(types, ",") != "delta,approval,tool,done" {
				t.Fatalf("events = %v", types)
			}
			if err := server.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLowestCodexEffortSelection(t *testing.T) {
	models := []connectorcli.Model{
		{ID: "other", Efforts: []string{"minimal"}},
		{ID: "selected", Efforts: []string{"high", "medium", "low"}},
	}
	if got := lowestCodexEffort(models, "selected"); got != "low" {
		t.Fatalf("selected model effort = %q, want low", got)
	}
	if got := lowestCodexEffort(models, "missing"); got != "" {
		t.Fatalf("unknown model effort = %q, want empty", got)
	}
	if got := lowestCodexEffort([]connectorcli.Model{{ID: "selected", Efforts: []string{"custom"}}}, "selected"); got != "" {
		t.Fatalf("unknown supported effort = %q, want empty", got)
	}
}

func TestCodexUnknownModelOmitsEffortWithoutFailingTurn(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createCodexTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET model='missing' WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	client := &effortTestCodexClient{models: []connectorcli.Model{{ID: "other", Efforts: []string{"minimal"}}}}
	driver := &codexRuntime{manager: newChatManager(database, Options{}), chatID: chatID, client: client, threadID: "thread", ctx: context.Background(), model: "missing"}
	if err := driver.send(ChatSendRequest{Text: "hello"}); err != nil {
		t.Fatalf("send with unknown model = %v", err)
	}
	if _, exists := client.turnParams["effort"]; exists {
		t.Fatalf("unknown model turn/start effort = %#v", client.turnParams["effort"])
	}
}

type effortTestCodexClient struct {
	models     []connectorcli.Model
	turnParams map[string]any
}

func (c *effortTestCodexClient) Call(_ context.Context, method string, params, out any) error {
	if method != "turn/start" {
		return fmt.Errorf("unexpected Codex method %q", method)
	}
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &c.turnParams); err != nil {
		return err
	}
	data = []byte(`{"turn":{"id":"turn-test"}}`)
	return json.Unmarshal(data, out)
}

func (c *effortTestCodexClient) Models(context.Context) ([]connectorcli.Model, error) {
	return c.models, nil
}
func (*effortTestCodexClient) Notifications() <-chan connectorcli.Notification { return nil }
func (*effortTestCodexClient) Requests() <-chan connectorcli.Request           { return nil }
func (*effortTestCodexClient) Respond(json.RawMessage, any) error              { return nil }
func (*effortTestCodexClient) Close() error                                    { return nil }

func TestCodexThreadResumeWithoutPrompt(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createCodexTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET session_id='resume-thread' WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	serverErr := make(chan error, 1)
	manager := newChatManager(database, Options{findCodex: func() (string, error) { return "codex-fixture", nil }, codexStart: func(ctx context.Context, _ string, _ []string, _ bool, _ string, _ string, _ ...string) (codexClient, error) {
		client, err := pipeCodexClient(ctx, func(read *bufio.Reader, write io.Writer) error {
			request, err := readCodexRequest(read, "initialize")
			if err != nil {
				return err
			}
			if err := writeCodexResponse(write, request.ID, map[string]any{}); err != nil {
				return err
			}
			if err := expectCodexNotification(read, "initialized"); err != nil {
				return err
			}
			request, err = readCodexRequest(read, "thread/resume")
			if err != nil {
				return err
			}
			var params map[string]any
			if err := json.Unmarshal(request.Params, &params); err != nil {
				return err
			}
			if params["threadId"] != "resume-thread" || params["approvalPolicy"] != "on-request" || params["sandbox"] != "read-only" {
				return fmt.Errorf("resume params = %#v", params)
			}
			if err := writeCodexResponse(write, request.ID, map[string]any{"thread": map[string]string{"id": "resume-thread"}}); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		go func() { serverErr <- <-client.serverDone }()
		return client.client, nil
	}})
	manager.dbDir = root
	var current chat
	if err := database.QueryRow(`SELECT id,connector_id,title,model,folder,session_id,created_at,updated_at FROM chat WHERE id=?`, chatID).Scan(&current.ID, &current.ConnectorID, &current.Title, &current.Model, &current.Folder, &current.SessionID, &current.CreatedAt, &current.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	current.Workdir, _ = manager.workdir(chatID)
	manager.runtime(chatID).connectorID = connectorID
	driver, err := startCodex(manager, current, "", manager.runtime(chatID))
	if err != nil {
		t.Fatal(err)
	}
	if manager.Running(chatID) {
		t.Fatal("resume started a turn")
	}
	if err := driver.close(); err != nil {
		t.Fatal(err)
	}
	if err := <-serverErr; err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func TestCodexStopInterruptsTurn(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createCodexTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	serverErr := make(chan error, 1)
	server := New(database, "local", false, "dev", root, Options{
		findCodex: func() (string, error) { return "codex-fixture", nil },
		codexStart: func(ctx context.Context, _ string, _ []string, _ bool, _ string, _ string, _ ...string) (codexClient, error) {
			client, err := pipeCodexClient(ctx, func(read *bufio.Reader, write io.Writer) error {
				request, err := readCodexRequest(read, "initialize")
				if err != nil {
					return err
				}
				if err := writeCodexResponse(write, request.ID, map[string]any{}); err != nil {
					return err
				}
				if err := expectCodexNotification(read, "initialized"); err != nil {
					return err
				}
				request, err = readCodexRequest(read, "thread/start")
				if err != nil {
					return err
				}
				if err := writeCodexResponse(write, request.ID, map[string]any{"thread": map[string]string{"id": "thread-stop"}}); err != nil {
					return err
				}
				request, err = readCodexRequest(read, "model/list")
				if err != nil {
					return err
				}
				if err := writeCodexResponse(write, request.ID, map[string]any{"data": []any{}}); err != nil {
					return err
				}
				request, err = readCodexRequest(read, "turn/start")
				if err != nil {
					return err
				}
				if err := writeCodexResponse(write, request.ID, map[string]any{"turn": map[string]string{"id": "turn-stop"}}); err != nil {
					return err
				}
				request, err = readCodexRequest(read, "turn/interrupt")
				if err != nil {
					return err
				}
				var params map[string]any
				if err := json.Unmarshal(request.Params, &params); err != nil {
					return err
				}
				if params["threadId"] != "thread-stop" || params["turnId"] != "turn-stop" {
					return fmt.Errorf("interrupt params = %#v", params)
				}
				if err := writeCodexResponse(write, request.ID, map[string]any{}); err != nil {
					return err
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			go func() { serverErr <- <-client.serverDone }()
			return client.client, nil
		},
	})
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "wait"}, nil); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/stop", nil, nil); code != http.StatusNoContent {
		t.Fatalf("stop = %d", code)
	}
	if err := <-serverErr; err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func TestCodexTurnErrorsAreStored(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		params string
		want   string
	}{
		{"failed turn", "turn/completed", `{"turn":{"status":"failed","error":{"message":"model failed"}}}`, "model failed"},
		{"non-retry error", "error", `{"willRetry":false,"error":{"message":"connection failed"}}`, "connection failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, _, root := chatAPI(t)
			connectorID := createCodexTestConnector(t, database)
			chatID := createDriverTestChat(t, database, connectorID, root)
			manager := newChatManager(database, Options{})
			driver := &codexRuntime{manager: manager, chatID: chatID, runtime: manager.runtime(chatID)}
			if err := driver.handleNotification(connectorcli.Notification{Method: test.method, Params: json.RawMessage(test.params)}); err != nil {
				t.Fatal(err)
			}
			var role, content string
			if err := database.QueryRow(`SELECT role,content FROM chat_message WHERE chat_id=?`, chatID).Scan(&role, &content); err != nil || role != "error" || content != test.want {
				t.Fatalf("stored turn error = %q %q, %v", role, content, err)
			}
		})
	}
}

type pipeCodex struct {
	client     *connectorcli.Client
	serverDone <-chan error
}

func pipeCodexClient(ctx context.Context, serve func(*bufio.Reader, io.Writer) error) (pipeCodex, error) {
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()
	var closeOnce sync.Once
	closePipes := func() error {
		closeOnce.Do(func() {
			_ = clientToServerW.Close()
			_ = serverToClientW.Close()
			_ = clientToServerR.Close()
			_ = serverToClientR.Close()
		})
		return nil
	}
	result := make(chan error, 1)
	go func() { result <- serve(bufio.NewReader(clientToServerR), serverToClientW) }()
	client := connectorcli.NewClientForIO(serverToClientR, clientToServerW, closePipes)
	if err := client.Initialize(ctx, "test"); err != nil {
		_ = client.Close()
		return pipeCodex{}, err
	}
	return pipeCodex{client: client, serverDone: result}, nil
}

type codexTestRequest struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func readCodexRequest(read *bufio.Reader, method string) (codexTestRequest, error) {
	line, err := read.ReadString('\n')
	if err != nil {
		return codexTestRequest{}, err
	}
	var request codexTestRequest
	if err := json.Unmarshal([]byte(line), &request); err != nil {
		return codexTestRequest{}, err
	}
	if request.Method != method {
		return codexTestRequest{}, fmt.Errorf("RPC method = %q, want %q", request.Method, method)
	}
	return request, nil
}

func expectCodexNotification(read *bufio.Reader, method string) error {
	line, err := read.ReadString('\n')
	if err != nil {
		return err
	}
	var notification struct {
		Method string `json:"method"`
		ID     any    `json:"id"`
	}
	if err := json.Unmarshal([]byte(line), &notification); err != nil {
		return err
	}
	if notification.Method != method || notification.ID != nil {
		return fmt.Errorf("notification = %#v", notification)
	}
	return nil
}

func writeCodexResponse(write io.Writer, id uint64, value any) error {
	data, err := json.Marshal(map[string]any{"id": id, "result": value})
	if err != nil {
		return err
	}
	_, err = write.Write(append(data, '\n'))
	return err
}

func createCodexTestConnector(t *testing.T, database *sql.DB) string {
	t.Helper()
	id := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'codex','Codex',?)`, id, now()); err != nil {
		t.Fatal(err)
	}
	return id
}
