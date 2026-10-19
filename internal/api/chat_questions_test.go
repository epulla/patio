package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	connectorcli "patio/internal/connector"
)

func TestNormalUserMessageDeactivatesOpenQuestions(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	question, err := InsertChatMessage(database, chatID, "question", `{"questions":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InsertChatMessage(database, chatID, "user", "reply"); err != nil {
		t.Fatal(err)
	}
	var answered bool
	if err := database.QueryRow(`SELECT answered FROM chat_message WHERE id=?`, question.ID).Scan(&answered); err != nil || !answered {
		t.Fatalf("question answered = %v, %v", answered, err)
	}
}

func TestAnswerQuestionRejectsBusyAndAlreadyAnswered(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	server := New(database, "local", false, "dev", root)
	question, err := InsertChatMessage(database, chatID, "question", `{"questions":[{"header":"Scope","question":"Which?","options":[{"label":"Small"},{"label":"Full"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	runtime := server.chats.runtime(chatID)
	runtime.busy = true
	path := "/api/chats/" + chatID + "/questions/" + question.ID + "/answer"
	if code := requestJSON(t, server, http.MethodPost, path, map[string]any{"answers": []any{map[string]any{"header": "Scope", "selected": []string{"Small"}}}}, nil); code != http.StatusConflict {
		t.Fatalf("busy answer status = %d", code)
	}
	runtime.busy = false
	if _, err := database.Exec(`UPDATE chat_message SET answered=1 WHERE id=?`, question.ID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, server, http.MethodPost, path, map[string]any{"answers": []any{map[string]any{"header": "Scope", "selected": []string{"Small"}}}}, nil); code != http.StatusConflict {
		t.Fatalf("double answer status = %d", code)
	}
}

func TestAnswerQuestionSendsFormattedUserMessage(t *testing.T) {
	database, _, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeSSE(w, `{"choices":[{"delta":{"content":"Received"}}]}`)
	})
	question, err := InsertChatMessage(database, chatID, "question", `{"questions":[{"header":"Scope","question":"Which scope?","options":[{"label":"Small"},{"label":"Full"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/chats/" + chatID + "/questions/" + question.ID + "/answer"
	if code := requestJSON(t, server, http.MethodPost, path, map[string]any{"answers": []any{map[string]any{"header": "Scope", "selected": []string{"Small"}}}}, nil); code != http.StatusAccepted {
		t.Fatalf("answer status = %d", code)
	}
	waitLocalDone(t, events)
	var content string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='user' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "Scope: Small" {
		t.Fatalf("sent answer = %q", content)
	}
	var answered bool
	if err := database.QueryRow(`SELECT answered FROM chat_message WHERE id=?`, question.ID).Scan(&answered); err != nil || !answered {
		t.Fatalf("question answered = %v, %v", answered, err)
	}
}

func TestAskUserToolDetectionPerDriver(t *testing.T) {
	input := `{"questions":[{"header":"Scope","question":"Which?","options":[{"label":"Small"},{"label":"Full"}]}]}`
	t.Run("OpenCode", func(t *testing.T) {
		database, _, root := chatAPI(t)
		connectorID := createTestConnector(t, database)
		chatID := createDriverTestChat(t, database, connectorID, root)
		manager := newChatManager(database, Options{})
		driver := &opencodeRuntime{manager: manager, chatID: chatID, active: true}
		server := &opencodeServer{chats: map[string]*opencodeRuntime{"session": driver}}
		data, _ := json.Marshal(map[string]any{"sessionID": "session", "name": "patio_ask_user", "input": json.RawMessage(input)})
		event, _ := json.Marshal(map[string]any{"type": "session.tool.called", "data": json.RawMessage(data)})
		var eventMap map[string]json.RawMessage
		_ = json.Unmarshal(event, &eventMap)
		if err := server.dispatchV2(eventMap); err != nil {
			t.Fatal(err)
		}
		assertQuestionMessage(t, database, chatID)
	})
	t.Run("Claude", func(t *testing.T) {
		database, _, root := chatAPI(t)
		connectorID := createTestConnector(t, database)
		chatID := createDriverTestChat(t, database, connectorID, root)
		driver := &claudeRuntime{manager: newChatManager(database, Options{}), chatID: chatID}
		line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__patio__ask_user","input":` + input + `}]}}`
		if err := driver.parseLine([]byte(line)); err != nil {
			t.Fatal(err)
		}
		assertQuestionMessage(t, database, chatID)
	})
	t.Run("Codex", func(t *testing.T) {
		database, _, root := chatAPI(t)
		connectorID := createTestConnector(t, database)
		chatID := createDriverTestChat(t, database, connectorID, root)
		driver := &codexRuntime{manager: newChatManager(database, Options{}), chatID: chatID}
		params, _ := json.Marshal(map[string]any{"item": map[string]any{"id": "call", "type": "mcpToolCall", "server": "patio", "toolName": "ask_user", "arguments": json.RawMessage(input)}})
		if err := driver.handleNotification(connectorcli.Notification{Method: "item/completed", Params: params}); err != nil {
			t.Fatal(err)
		}
		assertQuestionMessage(t, database, chatID)
	})
	t.Run("Copilot", func(t *testing.T) {
		database, _, root := chatAPI(t)
		connectorID := createTestConnector(t, database)
		chatID := createDriverTestChat(t, database, connectorID, root)
		driver := &copilotRuntime{manager: newChatManager(database, Options{}), chatID: chatID}
		update, _ := json.Marshal(map[string]any{"update": map[string]any{"sessionUpdate": "tool_call", "title": "mcp__patio__ask_user", "toolCall": map[string]any{"rawInput": json.RawMessage(input)}}})
		driver.handleUpdate(update)
		assertQuestionMessage(t, database, chatID)
	})
	t.Run("Local", func(t *testing.T) {
		calls := 0
		database, _, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			calls++
			if calls == 1 {
				payload := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "ask", "type": "function", "function": map[string]any{"name": "ticket__ask_user", "arguments": input}}}}}}}
				toolCall, _ := json.Marshal(payload)
				writeSSE(w, string(toolCall))
				return
			}
			writeSSE(w, `{"choices":[{"delta":{"content":"Done"}}]}`)
		})
		server.chats.options.mcpConnect = func(ctx context.Context, _ mcpServerSpec) (*sdk.ClientSession, func(), error) {
			mcpServer := sdk.NewServer(&sdk.Implementation{Name: "question-test", Version: "1"}, nil)
			sdk.AddTool[map[string]any, any](mcpServer, &sdk.Tool{Name: "ask_user"}, func(context.Context, *sdk.CallToolRequest, map[string]any) (*sdk.CallToolResult, any, error) {
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "Questions shown"}}}, nil, nil
			})
			clientTransport, serverTransport := sdk.NewInMemoryTransports()
			serverSession, err := mcpServer.Connect(ctx, serverTransport, nil)
			if err != nil {
				return nil, nil, err
			}
			client := sdk.NewClient(&sdk.Implementation{Name: "question-test-client", Version: "1"}, nil)
			session, err := client.Connect(ctx, clientTransport, nil)
			if err != nil {
				_ = serverSession.Close()
				return nil, nil, err
			}
			return session, func() { _ = session.Close(); _ = serverSession.Close() }, nil
		}
		if code := sendLocalMessage(t, server, chatID, "ask"); code != http.StatusAccepted {
			t.Fatalf("send = %d", code)
		}
		waitLocalDone(t, events)
		assertQuestionMessage(t, database, chatID)
	})
}

func assertQuestionMessage(t *testing.T, database *sql.DB, chatID string) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=? AND role='question'`, chatID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("question count = %d, %v", count, err)
	}
	var storedChatID, content string
	if err := database.QueryRow(`SELECT chat_id,content FROM chat_message WHERE chat_id=? AND role='question'`, chatID).Scan(&storedChatID, &content); err != nil {
		t.Fatal(err)
	}
	var questions struct {
		Questions []json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal([]byte(content), &questions); err != nil || storedChatID != chatID || len(questions.Questions) != 1 {
		t.Fatalf("question = chat %q, content %q, err %v", storedChatID, content, err)
	}
	if strings.TrimSpace(content) == "" {
		t.Fatal("question content empty")
	}
}
