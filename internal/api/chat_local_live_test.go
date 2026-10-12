package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestLocalOllamaLive(t *testing.T) {
	if os.Getenv("PATIO_LOCAL_LIVE") != "1" {
		t.Skip("set PATIO_LOCAL_LIVE=1 for local Ollama checks")
	}
	binary := os.Getenv("PATIO_BINARY")
	if binary == "" {
		t.Fatal("PATIO_BINARY is required")
	}
	baseURL := os.Getenv("PATIO_LOCAL_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	keyring.MockInit()
	database, _, root := chatAPI(t)
	connectorID := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,base_url,model,created_at) VALUES(?,'local','Ollama',?,'qwen2.5:3b',?)`, connectorID, baseURL, now()); err != nil {
		t.Fatal(err)
	}
	events := make(chan map[string]any, 512)
	server := New(database, "local", false, "live", root, Options{
		DBPath: filepath.Join(root, "test.db"), mcpExecutable: binary,
		Emit: func(_ string, event any) { events <- event.(map[string]any) },
	})
	defer server.Close()

	for _, model := range []string{"qwen2.5:3b", "llama3.2:3b"} {
		runLiveOllamaModel(t, database, server, events, connectorID, root, model)
	}
}

func runLiveOllamaModel(t *testing.T, database *sql.DB, server *Server, events <-chan map[string]any, connectorID, root, model string) {
	t.Helper()
	shouldStop := false
	defer func() {
		if !shouldStop {
			return
		}
		output, err := exec.Command("ollama", "stop", model).CombinedOutput()
		if err != nil {
			t.Errorf("stop %s: %v: %s", model, err, strings.TrimSpace(string(output)))
		}
	}()
	chatID := liveLocalChat(t, database, connectorID, root, model)
	if err := requireLiveMemoryHeadroom(); err != nil {
		t.Fatal(err)
	}
	shouldStop = true
	started := time.Now()
	if code := sendLocalMessage(t, server, chatID, "Say hi in 3 words"); code != http.StatusAccepted {
		t.Fatalf("%s hello prompt status = %d", model, code)
	}
	hello, helloTools, _ := waitLiveLocalTurn(t, database, server, events, chatID, nil)
	helloTime := time.Since(started)
	t.Logf("LIVE model=%s hello_wall=%s reply=%q hello_tools=%v", model, helloTime.Round(time.Millisecond), strings.TrimSpace(hello), helloTools)

	if err := requireLiveMemoryHeadroom(); err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	writeCalled := liveLocalWrite(t, database, server, events, chatID, root)
	writeTime := time.Since(started)
	t.Logf("LIVE model=%s write_wall=%s file__write_file=%v", model, writeTime.Round(time.Millisecond), writeCalled)
}

func requireLiveMemoryHeadroom() error {
	output, err := exec.Command("sh", "-c", "memory_pressure | tail -1").CombinedOutput()
	if err != nil {
		return fmt.Errorf("check memory pressure: %w: %s", err, strings.TrimSpace(string(output)))
	}
	line := strings.TrimSpace(string(output))
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return fmt.Errorf("cannot parse memory pressure output %q", line)
	}
	percentage := strings.TrimSuffix(fields[len(fields)-1], "%")
	free, err := strconv.Atoi(percentage)
	if err != nil {
		return fmt.Errorf("cannot parse memory pressure output %q: %w", line, err)
	}
	if free < 30 {
		return fmt.Errorf("memory pressure free percentage %d%% is below required 30%%; stopping live checks", free)
	}
	return nil
}

func liveLocalWrite(t *testing.T, database *sql.DB, server *Server, events <-chan map[string]any, chatID, root string) bool {
	t.Helper()
	if code := sendLocalMessage(t, server, chatID, "Create a file hello.txt containing hi"); code != http.StatusAccepted {
		t.Fatalf("send write prompt = %d", code)
	}
	decision := true
	_, tools, approvalSeen := waitLiveLocalTurn(t, database, server, events, chatID, &decision)
	called := tools["file__write_file"]
	if called {
		path := filepath.Join(root, "chats", chatID, "hello.txt")
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "hi" {
			t.Fatalf("approved file = %q, %v", data, err)
		}
	} else if approvalSeen {
		t.Fatal("approval shown without file__write_file tool call")
	}
	return called
}

func liveLocalChat(t *testing.T, database *sql.DB, connectorID, root, model string) string {
	t.Helper()
	id := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET model=? WHERE id=?`, model, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func waitLiveLocalTurn(t *testing.T, database *sql.DB, server *Server, events <-chan map[string]any, chatID string, decision *bool) (string, map[string]bool, bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	tools := make(map[string]bool)
	approvalSeen := false
	answered := false
	for {
		select {
		case event := <-events:
			switch eventType(event) {
			case "tool":
				var message ChatMessage
				if raw, ok := event["message"]; ok {
					message, _ = raw.(ChatMessage)
				}
				for _, name := range []string{"file__write_file", "ticket__list_boards"} {
					if strings.Contains(message.Content, name) {
						tools[name] = true
					}
				}
			case "approval":
				approvalSeen = true
			case "error":
				t.Fatalf("live local chat error: %v", event["message"])
			case "done":
				var reply string
				if raw, ok := event["message"]; ok {
					if message, ok := raw.(ChatMessage); ok {
						reply = message.Content
					}
				}
				if reply == "" {
					_ = database.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='assistant' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&reply)
				}
				return reply, tools, approvalSeen
			}
		case <-poll.C:
			if decision == nil || answered {
				continue
			}
			var approvalID string
			err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&approvalID)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				t.Fatalf("read pending approval: %v", err)
			}
			if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/approvals/"+approvalID, map[string]bool{"allow": *decision}, nil); code != http.StatusOK {
				t.Fatalf("answer live approval = %d", code)
			}
			answered = true
		case <-deadline.C:
			t.Fatalf("live local chat %s timed out; tools=%v", chatID, tools)
		}
	}
}
