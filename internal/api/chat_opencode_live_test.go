package api

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	connectorcli "patio/internal/connector"
)

func TestOpenCodeV2Live(t *testing.T) {
	if os.Getenv("PATIO_OPENCODE_V2_LIVE") != "1" {
		t.Skip("set PATIO_OPENCODE_V2_LIVE=1 for sandboxed OpenCode v2 checks")
	}
	sandbox := os.Getenv("OPENCODE_V2_SANDBOX")
	if sandbox == "" {
		t.Fatal("OPENCODE_V2_SANDBOX is required")
	}
	home := filepath.Join(sandbox, "home")
	opencodePath := filepath.Join(home, ".opencode", "bin", "opencode")
	if got := os.Getenv("HOME"); got != home {
		t.Fatalf("sandbox HOME is not set")
	}
	for key, suffix := range map[string]string{
		"XDG_CONFIG_HOME": ".config",
		"XDG_DATA_HOME":   filepath.Join(".local", "share"),
		"XDG_CACHE_HOME":  filepath.Join(".cache"),
		"XDG_STATE_HOME":  filepath.Join(".local", "state"),
	} {
		if got, want := os.Getenv(key), filepath.Join(home, suffix); got != want {
			t.Fatalf("sandbox %s is not set", key)
		}
	}

	binary := os.Getenv("PATIO_BINARY")
	if binary == "" {
		t.Fatal("PATIO_BINARY is required")
	}
	database, _, root := chatAPI(t)
	dbPath := filepath.Join(root, "test.db")
	connectorID := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'opencode','OpenCode',?)`, connectorID, now()); err != nil {
		t.Fatal(err)
	}
	events := make(chan liveOpenCodeEvent, 64)
	server := New(database, "local", false, "live", root, Options{
		DBPath: dbPath,
		Emit: func(topic string, event any) {
			events <- liveOpenCodeEvent{chatID: strings.TrimPrefix(topic, "chat:"), data: event.(map[string]any)}
		},
		mcpExecutable: binary,
		findOpencode:  func() (string, error) { return opencodePath, nil },
	})
	defer func() {
		if err := server.Close(); err != nil {
			t.Errorf("close live OpenCode server: %v", err)
		}
	}()

	var status connectorcli.Status
	if code := requestJSON(t, server, http.MethodGet, "/api/connectors/"+connectorID+"/status", nil, &status); code != http.StatusOK {
		t.Fatalf("connector status = %d", code)
	}
	if status.LoggedIn == nil || !*status.LoggedIn || status.Account != "OpenAI" {
		t.Fatalf("sandbox connector login was not recognized")
	}

	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET model='openai/gpt-5.5' WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	reply, requestedApproval := liveOpenCodePrompt(t, server, database, chatID, "Reply with just OK", nil, events)
	if requestedApproval {
		t.Fatal("reply prompt unexpectedly requested approval")
	}
	if reply != "OK" {
		t.Fatalf("streamed assistant reply = %q", reply)
	}
	var assistant string
	if err := database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='assistant' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&assistant); err != nil || assistant != "OK" {
		t.Fatalf("stored assistant reply = %q", assistant)
	}

	allowedChat := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET model='openai/gpt-5.5' WHERE id=?`, allowedChat); err != nil {
		t.Fatal(err)
	}
	if _, requestedApproval := liveOpenCodePrompt(t, server, database, allowedChat, "Create a file hello.txt containing hi", boolPointer(true), events); !requestedApproval {
		t.Fatal("allowed file prompt completed without approval")
	}
	allowedPath := filepath.Join(root, "chats", allowedChat, "hello.txt")
	content, err := os.ReadFile(allowedPath)
	if err != nil || !strings.Contains(string(content), "hi") {
		t.Fatalf("allowed file content = %q", content)
	}

	deniedChat := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET model='openai/gpt-5.5' WHERE id=?`, deniedChat); err != nil {
		t.Fatal(err)
	}
	if _, requestedApproval := liveOpenCodePrompt(t, server, database, deniedChat, "Create a file hello.txt containing hi", boolPointer(false), events); !requestedApproval {
		t.Fatal("denied file prompt completed without approval")
	}
	deniedPath := filepath.Join(root, "chats", deniedChat, "hello.txt")
	if _, err := os.Stat(deniedPath); !os.IsNotExist(err) {
		t.Fatalf("denied file exists or stat failed: %v", err)
	}
}

type liveOpenCodeEvent struct {
	chatID string
	data   map[string]any
}

func liveOpenCodePrompt(t *testing.T, server *Server, database *sql.DB, chatID, prompt string, approval *bool, events <-chan liveOpenCodeEvent) (string, bool) {
	t.Helper()
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": prompt}, nil); code != http.StatusAccepted {
		t.Fatalf("send OpenCode prompt = %d", code)
	}
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	var deltas strings.Builder
	requestedApproval := false
	for {
		select {
		case event := <-events:
			if event.chatID != chatID {
				continue
			}
			switch event.data["type"] {
			case "delta":
				if text, ok := event.data["text"].(string); ok {
					deltas.WriteString(text)
				}
			case "approval":
				requestedApproval = true
				if approval == nil {
					t.Fatalf("unexpected OpenCode approval for %q", prompt)
				}
				var approvalID string
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&approvalID); err == nil {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if approvalID == "" {
					t.Fatal("OpenCode approval row was not created")
				}
				if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": *approval}, nil); code != http.StatusOK {
					t.Fatalf("answer OpenCode approval = %d", code)
				}
			case "done":
				if approval != nil && !requestedApproval {
					t.Fatalf("file prompt completed without approval")
				}
				return deltas.String(), requestedApproval
			case "error":
				t.Fatalf("OpenCode live turn failed")
			}
		case <-deadline.C:
			t.Fatalf("OpenCode prompt timed out")
		}
	}
}
