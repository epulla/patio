package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTicketMentionOpensBotChats(t *testing.T) {
	db, _, root := chatAPI(t)
	connector := createTestConnector(t, db)
	handler := New(db, "local", false, "dev", root)
	board := createBoard(t, handler, "TM", []string{"Todo"})
	for _, name := range []string{"Billy", "Sammy"} {
		if _, err := db.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai,connector_id) VALUES(?,?,?,1,1,?)`, strings.ToLower(name), name, strings.ToLower(name)+"@bot", connector); err != nil {
			t.Fatal(err)
		}
	}
	var processes []*pipeChatProcess
	s := New(db, "local", false, "dev", root, Options{findClaude: func() (string, error) { return "claude-test", nil }, commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil }, processFactory: func(string, []string, []string, string) (chatProcess, error) {
		p := newPipeChatProcess()
		processes = append(processes, p)
		return p, nil
	}})
	defer s.Close()
	var ticket testTicket
	if code := requestJSON(t, s, http.MethodPost, "/api/boards/"+board.ID+"/tickets", map[string]any{"title": "Mention test", "description": "@Billy do X, @Sammy do Y"}, &ticket); code != http.StatusCreated {
		t.Fatalf("create status=%d", code)
	}
	assertMentionChats(t, db, processes, ticket.ID, ticket.Code, ticket.Title, 2, []string{"Task: do X", "Task: do Y"})
	if code := requestJSON(t, s, http.MethodPut, "/api/tickets/"+ticket.ID, map[string]string{"description": "@Billy do X, @Sammy do Y, @Billy new task"}, nil); code != http.StatusOK {
		t.Fatalf("update status=%d", code)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat WHERE bot_id IN ('billy','sammy')`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("new task chats=%d err=%v", count, err)
	}
	ai := New(db, "ai", false, "dev", root, Options{findClaude: func() (string, error) { return "claude-test", nil }, commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil }, processFactory: func(string, []string, []string, string) (chatProcess, error) {
		p := newPipeChatProcess()
		processes = append(processes, p)
		return p, nil
	}})
	defer ai.Close()
	before := 0
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat WHERE bot_id IN ('billy','sammy')`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, ai, http.MethodPost, "/api/boards/"+board.ID+"/tickets", map[string]any{"title": "AI ticket", "description": "@Billy do X, @Sammy do Y"}, nil); code != http.StatusCreated {
		t.Fatalf("AI create status=%d", code)
	}
	var after int
	_ = db.QueryRow(`SELECT COUNT(*) FROM chat WHERE bot_id IN ('billy','sammy')`).Scan(&after)
	if after != before {
		t.Fatalf("AI actor opened chats: %d -> %d", before, after)
	}
}

func TestCommentMentionOpensBotChat(t *testing.T) {
	db, _, root := chatAPI(t)
	connector := createTestConnector(t, db)
	handler := New(db, "local", false, "dev", root)
	board := createBoard(t, handler, "CM", []string{"Todo"})
	if _, err := db.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai,connector_id) VALUES('billy','Billy','billy@bot',1,1,?)`, connector); err != nil {
		t.Fatal(err)
	}
	ticket := createTicket(t, handler, board.ID, "")
	var processes []*pipeChatProcess
	newServer := func(user string) *Server {
		return New(db, user, false, "dev", root, Options{findClaude: func() (string, error) { return "claude-test", nil }, commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil }, processFactory: func(string, []string, []string, string) (chatProcess, error) {
			p := newPipeChatProcess()
			processes = append(processes, p)
			return p, nil
		}})
	}
	s := newServer("local")
	defer s.Close()
	var comment comment
	if code := requestJSON(t, s, http.MethodPost, "/api/tickets/"+ticket.ID+"/comments", map[string]string{"content": "@Billy check"}, &comment); code != http.StatusCreated {
		t.Fatalf("comment status=%d", code)
	}
	assertMentionChats(t, db, processes, ticket.ID, ticket.Code, ticket.Title, 1, []string{"Task: check"})
	ai := newServer("ai")
	defer ai.Close()
	if code := requestJSON(t, ai, http.MethodPost, "/api/tickets/"+ticket.ID+"/comments", map[string]string{"content": "posted", "as_bot": "billy"}, nil); code != http.StatusCreated {
		t.Fatalf("as_bot status=%d", code)
	}
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM chat WHERE bot_id='billy'`).Scan(&count)
	if count != 1 {
		t.Fatalf("AI bot comment opened chat: %d", count)
	}
	if code := requestJSON(t, s, http.MethodPost, "/api/tickets/"+ticket.ID+"/comments", map[string]string{"content": "no", "as_bot": "billy"}, nil); code != http.StatusBadRequest {
		t.Fatalf("local as_bot status=%d", code)
	}
}

func assertMentionChats(t *testing.T, db *sql.DB, processes []*pipeChatProcess, ticketID, code, title string, want int, fragments []string) {
	t.Helper()
	deadline := time.After(time.Second)
	for len(processes) < want {
		select {
		case <-deadline:
			t.Fatal("chat runtime not started")
		case <-time.After(time.Millisecond):
		}
	}
	rows, err := db.Query(`SELECT id,bot_id,parent_chat_id FROM chat WHERE bot_id IN ('billy','sammy') AND created_at IS NOT NULL ORDER BY rowid LIMIT ?`, want)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var id, bot string
		var parent sql.NullString
		if err := rows.Scan(&id, &bot, &parent); err != nil {
			t.Fatal(err)
		}
		if parent.Valid {
			t.Fatalf("chat %s has parent %s", id, parent.String)
		}
		var text string
		if err := db.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='user' ORDER BY rowid LIMIT 1`, id).Scan(&text); err != nil {
			t.Fatal(err)
		}
		for _, part := range []string{"[Ticket " + code + ": " + title + "](/tickets/" + ticketID + ")", fragments[i], "add_comment", "chat_id " + id} {
			if !strings.Contains(text, part) {
				t.Fatalf("message missing %q: %q", part, text)
			}
		}
		select {
		case line := <-processes[i].input:
			var msg struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(line), &msg) != nil {
				t.Fatalf("bad runtime input %q", line)
			}
		case <-time.After(time.Second):
			t.Fatal("first message not delivered")
		}
		i++
	}
	if i != want {
		t.Fatalf("chats=%d want %d", i, want)
	}
}
