package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	connectorcli "patio/internal/connector"
	"patio/internal/db"
)

func TestChatThinkingOffLive(t *testing.T) {
	if os.Getenv("PATIO_CHAT_THINKING_LIVE") != "1" {
		t.Skip("set PATIO_CHAT_THINKING_LIVE=1 for logged-in Codex and Copilot checks")
	}
	binary := os.Getenv("PATIO_BINARY")
	if binary == "" {
		t.Fatal("PATIO_BINARY is required")
	}
	root := t.TempDir()
	dbPath := filepath.Join(root, "thinking-live.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	events := make(chan any, 32)
	var sentEffort any
	var copilotArgs []string
	codexPath, err := connectorcli.Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	copilotPath, err := connectorcli.Find("copilot")
	if err != nil {
		t.Fatal(err)
	}
	server := New(database, "local", false, "live", root, Options{
		DBPath:        dbPath,
		mcpExecutable: binary,
		Emit:          func(_ string, event any) { events <- event },
		findCodex:     func() (string, error) { return codexPath, nil },
		codexStart: func(ctx context.Context, path string, env []string, isolated bool, version, cwd string, overrides ...string) (codexClient, error) {
			client, err := connectorcli.StartCodexInDir(ctx, path, env, isolated, version, cwd, overrides...)
			if err != nil {
				return nil, err
			}
			return &thinkingLiveCodexClient{codexClient: client, recordEffort: func(value any) { sentEffort = value }}, nil
		},
		findCopilot: func() (string, error) { return copilotPath, nil },
		processFactory: func(path string, args, env []string, cwd string) (chatProcess, error) {
			if path == copilotPath {
				copilotArgs = append([]string(nil), args...)
			}
			return startOSChatProcess(path, args, env, cwd)
		},
	})
	server.chats.approvalTimeout = 30 * time.Second
	defer server.Close()

	codexID := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'codex','Codex live',?)`, codexID, now()); err != nil {
		t.Fatal(err)
	}
	codexChat := createThinkingLiveChat(t, server, codexID)
	codexReply := sendThinkingLivePrompt(t, server, events, codexChat)
	if effort, ok := sentEffort.(string); !ok || effort == "" {
		t.Fatalf("Codex Thinking-off effort = %#v, expected supported lowest effort", sentEffort)
	}
	t.Logf("LIVE Codex model=default thinking=off effort=%s reply=%q", sentEffort, codexReply)
	if err := server.chats.CloseChat(codexChat); err != nil {
		t.Fatal(err)
	}

	copilotID := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'copilot','Copilot live',?)`, copilotID, now()); err != nil {
		t.Fatal(err)
	}
	copilotChat := createThinkingLiveChat(t, server, copilotID)
	copilotReply := sendThinkingLivePrompt(t, server, events, copilotChat)
	wantArgs := []string{"--acp", "--stdio", "--reasoning-effort", "low"}
	if strings.Join(copilotArgs, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("Copilot Thinking-off args = %v, want %v", copilotArgs, wantArgs)
	}
	t.Logf("LIVE Copilot model=default thinking=off args=%v reply=%q", copilotArgs, copilotReply)
}

func createThinkingLiveChat(t *testing.T, server *Server, connectorID string) string {
	t.Helper()
	var c chat
	if code := requestJSON(t, server, http.MethodPost, "/api/chats", map[string]any{"connector_id": connectorID, "thinking": false}, &c); code != http.StatusCreated {
		t.Fatalf("create live chat = %d", code)
	}
	return c.ID
}

func sendThinkingLivePrompt(t *testing.T, server *Server, events <-chan any, chatID string) string {
	t.Helper()
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "Reply with just OK"}, nil); code != http.StatusAccepted {
		t.Fatalf("send live prompt = %d", code)
	}
	timer := time.NewTimer(3 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case raw := <-events:
			event, _ := raw.(map[string]any)
			if event["type"] != "done" {
				continue
			}
			message, _ := event["message"].(ChatMessage)
			if message.Role != "assistant" {
				t.Fatalf("live prompt returned %s: %s", message.Role, message.Content)
			}
			return strings.TrimSpace(message.Content)
		case <-timer.C:
			t.Fatal("live prompt timed out")
		}
	}
}

type thinkingLiveCodexClient struct {
	codexClient
	recordEffort func(any)
}

func (c *thinkingLiveCodexClient) Call(ctx context.Context, method string, params, out any) error {
	if method == "turn/start" {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		var values map[string]any
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		c.recordEffort(values["effort"])
	}
	return c.codexClient.Call(ctx, method, params, out)
}
