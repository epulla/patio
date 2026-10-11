package api

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestLocalOllamaLive(t *testing.T) {
	if os.Getenv("MY_TICKETS_LOCAL_LIVE") != "1" {
		t.Skip("set MY_TICKETS_LOCAL_LIVE=1 for local Ollama checks")
	}
	binary := os.Getenv("MY_TICKETS_BINARY")
	if binary == "" {
		t.Fatal("MY_TICKETS_BINARY is required")
	}
	baseURL := os.Getenv("MY_TICKETS_LOCAL_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	keyring.MockInit()
	database, _, root := chatAPI(t)
	connectorID := uid()
	dbPath := filepath.Join(root, "test.db")
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,base_url,model,created_at) VALUES(?,'local','Ollama',?,'qwen3:4b',?)`, connectorID, baseURL, now()); err != nil {
		t.Fatal(err)
	}
	events := make(chan map[string]any, 512)
	server := New(database, "local", false, "live", root, Options{
		DBPath: dbPath, mcpExecutable: binary,
		Emit: func(_ string, event any) { events <- event.(map[string]any) },
	})
	defer server.Close()

	plainChat := liveLocalChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET model='' WHERE id=?`, plainChat); err != nil {
		t.Fatal(err)
	}
	if code := sendLocalMessage(t, server, plainChat, "Call ticket__list_boards, then reply with just OK"); code != http.StatusAccepted {
		t.Fatalf("send plain prompt = %d", code)
	}
	plain, tools, _ := waitLiveLocalTurn(t, database, server, events, plainChat, nil)
	if strings.TrimSpace(plain) != "OK" {
		t.Fatalf("plain reply = %q, want OK", plain)
	}
	if !tools["ticket__list_boards"] {
		t.Fatal("qwen3:4b did not call ticket__list_boards")
	}

	allowed := liveLocalWrite(t, database, server, events, connectorID, root, true)
	if !allowed {
		t.Fatal("qwen3:4b did not call file__write_file after three attempts")
	}
	denied := liveLocalWrite(t, database, server, events, connectorID, root, false)
	if !denied {
		t.Fatal("qwen3:4b did not call file__write_file for denied action")
	}
}

func liveLocalWrite(t *testing.T, database *sql.DB, server *Server, events <-chan map[string]any, connectorID, root string, allow bool) bool {
	t.Helper()
	chatID := liveLocalChat(t, database, connectorID, root)
	if code := sendLocalMessage(t, server, chatID, "Create a file hello.txt containing hi"); code != http.StatusAccepted {
		t.Fatalf("send write prompt = %d", code)
	}
	_, tools, approvalSeen := waitLiveLocalTurn(t, database, server, events, chatID, &allow)
	if tools["file__write_file"] {
		path := filepath.Join(root, "chats", chatID, "hello.txt")
		_, err := os.Stat(path)
		if allow && err != nil {
			t.Fatalf("approved file missing: %v", err)
		}
		if !allow && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("denied file exists or stat failed: %v", err)
		}
		if allow {
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "hi" {
				t.Fatalf("approved file = %q, %v", data, err)
			}
		}
		return true
	}
	if approvalSeen {
		t.Fatalf("approval shown without file__write_file tool call (allow=%v)", allow)
	}
	return false
}

func liveLocalChat(t *testing.T, database *sql.DB, connectorID, root string) string {
	t.Helper()
	id := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET model='qwen3:4b' WHERE id=?`, id); err != nil {
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
