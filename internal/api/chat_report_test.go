package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentMessageMetadataExistsWhenTurnEndsImmediately(t *testing.T) {
	db, _, root := chatAPI(t)
	connector := createTestConnector(t, db)
	a := createDriverTestChat(t, db, connector, root)
	b := createDriverTestChat(t, db, connector, root)
	pipes := map[string]*pipeChatProcess{a: newPipeChatProcess(), b: newPipeChatProcess()}
	s := New(db, "local", false, "dev", root, Options{findClaude: func() (string, error) { return "claude-test", nil }, commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil }, processFactory: func(_ string, _, _ []string, cwd string) (chatProcess, error) { return pipes[filepath.Base(cwd)], nil }})
	defer s.Close()
	if code := requestJSON(t, s, http.MethodPost, "/api/chats/"+b+"/agent-messages", map[string]any{"from_chat_id": a, "text": "work", "report_back": true}, nil); code != 200 {
		t.Fatalf("agent delivery=%d", code)
	}
	var from, report string
	if err := db.QueryRow(`SELECT from_chat_id,report_to_chat_id FROM chat_message WHERE chat_id=? AND role='user' ORDER BY rowid DESC LIMIT 1`, b).Scan(&from, &report); err != nil || from != a || report != a {
		t.Fatalf("agent metadata from=%q report=%q err=%v", from, report, err)
	}
	<-pipes[b].input
	if _, err := io.WriteString(pipes[b].stdoutW, `{"type":"result","is_error":false,"result":"fast result"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-pipes[a].input:
		var in struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &in); err != nil || !strings.Contains(in.Message.Content, "fast result") {
			t.Fatalf("report=%s err=%v", line, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("report-back missing after immediate finish")
	}
}

func TestFailedHandoffDeletesChild(t *testing.T) {
	db, _, root := chatAPI(t)
	connector := createTestConnector(t, db)
	caller := createDriverTestChat(t, db, connector, root)
	s := New(db, "local", false, "dev", root, Options{findClaude: func() (string, error) { return "", errors.New("missing runtime") }})
	defer s.Close()
	if code := requestJSON(t, s, http.MethodPost, "/api/chats/"+caller+"/handoff", map[string]any{"connector_id": connector, "prompt": "cannot start"}, nil); code != 500 {
		t.Fatalf("handoff failure status=%d", code)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat WHERE parent_chat_id=?`, caller).Scan(&count); err != nil || count != 0 {
		t.Fatalf("children remaining=%d err=%v", count, err)
	}
}

func TestReportBackDeliversFinalAndFailureButNotWhenDisabled(t *testing.T) {
	for _, tc := range []struct {
		name    string
		report  bool
		failure bool
		want    string
	}{{"success", true, false, "result"}, {"off", false, false, ""}, {"failure", true, true, "Turn failed: boom"}} {
		t.Run(tc.name, func(t *testing.T) {
			db, _, root := chatAPI(t)
			connector := createTestConnector(t, db)
			a := createDriverTestChat(t, db, connector, root)
			b := createDriverTestChat(t, db, connector, root)
			var report any
			if tc.report {
				report = a
			}
			if _, err := db.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at,from_chat_id,report_to_chat_id) VALUES('incoming',?,'user','work',?, ?,?)`, b, now(), a, report); err != nil {
				t.Fatal(err)
			}
			pipes := map[string]*pipeChatProcess{a: newPipeChatProcess(), b: newPipeChatProcess()}
			s := New(db, "local", false, "dev", root, Options{findClaude: func() (string, error) { return "claude-test", nil }, commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil }, processFactory: func(_ string, _, _ []string, cwd string) (chatProcess, error) { return pipes[filepath.Base(cwd)], nil }})
			defer s.Close()
			var turnErr error
			if tc.failure {
				turnErr = errors.New("boom")
			}
			if err := s.chats.finishTurn(b, "result", turnErr); err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				select {
				case <-pipes[a].input:
					t.Fatal("unexpected report-back")
				case <-time.After(100 * time.Millisecond):
				}
				return
			}
			select {
			case line := <-pipes[a].input:
				var in struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				}
				if err := json.Unmarshal([]byte(line), &in); err != nil || !strings.Contains(in.Message.Content, tc.want) || !strings.Contains(in.Message.Content, `title "New chat"`) {
					t.Fatalf("report input=%s err=%v", line, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("report-back did not wake sender")
			}
		})
	}
}

func TestAgentStartedChatQuestionAndApprovalNotify(t *testing.T) {
	db, _, root := chatAPI(t)
	parent := uid()
	child := uid()
	if _, err := db.Exec(`INSERT INTO chat(id,title,created_at,updated_at,parent_chat_id) VALUES(?,?,?, ?,NULL),(?,?,?, ?,?)`, parent, "Parent", now(), now(), child, "Child", now(), now(), parent); err != nil {
		t.Fatal(err)
	}
	if err := NotifyScheduledQuestion(db, child); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification WHERE link=?`, "/ai/chats/"+child).Scan(new(int)); err != nil {
		t.Fatal(err)
	}
	s := New(db, "local", false, "dev", root)
	s.chats.approvalTimeout = time.Millisecond
	s.chats.approvalPollInterval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _, _ = s.chats.requestApproval(ctx, child, "write", []byte(`{}`)) }()
	waitFor(t, func() bool {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM notification WHERE link=? AND kind='approval'`, "/ai/chats/"+child).Scan(&n)
		return n > 0
	})
	cancel()
	<-done
	_ = s.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification WHERE link=? AND (kind='chat' OR kind='approval')`, "/ai/chats/"+child).Scan(&count); err != nil || count < 2 {
		t.Fatalf("agent chat notifications=%d err=%v", count, err)
	}
}
