package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseMentions(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []mention
	}{
		{"@Billy implement X, @Sammy watch for Y", []mention{{"Billy", "implement X"}, {"Sammy", "watch for Y"}}},
		{"@billy do it", []mention{{"Billy", "do it"}}},
		{"@Billy do @Bob later", []mention{{"Billy", "do @Bob later"}}},
		{"a@Billy.com ignored", nil},
		{"@Billy do\n\nnext", []mention{{"Billy", "do"}}},
		{"@Billy, @Sammy work", []mention{{"Sammy", "work"}}},
	} {
		got := parseMentions(tc.text, []string{"Billy", "Sammy"})
		if len(got) != len(tc.want) {
			t.Fatalf("parseMentions(%q)=%#v want %#v", tc.text, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("parseMentions(%q)=%#v want %#v", tc.text, got, tc.want)
			}
		}
	}
}

func TestChatMentionOpensBotChats(t *testing.T) {
	db, _, root := chatAPI(t)
	connector := createTestConnector(t, db)
	parent := createDriverTestChat(t, db, connector, root)
	for _, bot := range []string{"Billy", "Sammy"} {
		if _, err := db.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai,connector_id) VALUES(?,?,?,1,1,?)`, strings.ToLower(bot), bot, strings.ToLower(bot)+"@bot", connector); err != nil {
			t.Fatal(err)
		}
	}
	fake := newPipeChatProcess()
	s := New(db, "local", false, "dev", root, Options{findClaude: func() (string, error) { return "claude-test", nil }, commandBuilder: func(_, _, _, _, _ string) ([]string, error) { return []string{"fake"}, nil }, processFactory: func(string, []string, []string, string) (chatProcess, error) { return fake, nil }})
	defer s.Close()
	if code := requestJSON(t, s, http.MethodPost, "/api/chats/"+parent+"/messages", map[string]string{"text": "@Billy implement X, @Sammy watch for Y"}, nil); code != http.StatusAccepted {
		t.Fatalf("send status=%d", code)
	}
	var delivered string
	select {
	case line := <-fake.input:
		var message struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			t.Fatal(err)
		}
		delivered = message.Message.Content
	case <-time.After(time.Second):
		t.Fatal("parent did not receive message")
	}
	var children []struct{ ID, BotID, Parent string }
	rows, err := db.Query(`SELECT id,bot_id,parent_chat_id FROM chat WHERE parent_chat_id=?`, parent)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var c struct{ ID, BotID, Parent string }
		if err := rows.Scan(&c.ID, &c.BotID, &c.Parent); err != nil {
			t.Fatal(err)
		}
		children = append(children, c)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 {
		t.Fatalf("children=%#v", children)
	}
	for _, child := range children {
		var name string
		if err := db.QueryRow(`SELECT name FROM "user" WHERE id=?`, child.BotID).Scan(&name); err != nil {
			t.Fatal(err)
		}
		if child.Parent != parent || !strings.Contains(delivered, "/ai/chats/"+child.ID) {
			t.Fatalf("child=%#v delivered=%q", child, delivered)
		}
		var reportTo string
		if err := db.QueryRow(`SELECT report_to_chat_id FROM chat_queue WHERE chat_id=?`, child.ID).Scan(&reportTo); err != nil || reportTo != parent {
			t.Fatalf("child %s report_to=%q err=%v", child.ID, reportTo, err)
		}
		if !strings.Contains(delivered, "@"+name+"](/ai/chats/"+child.ID+")") {
			t.Fatalf("missing link for %s: %q", name, delivered)
		}
	}
	if _, err := db.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES('agent-sender','Agent sender','0','0')`); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, s, http.MethodPost, "/api/chats/"+parent+"/agent-messages", map[string]string{"from_chat_id": "agent-sender", "text": "@Billy do it"}, nil); code != http.StatusOK {
		t.Fatalf("agent message status=%d", code)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat WHERE parent_chat_id=?`, parent).Scan(&count); err != nil || count != 2 {
		t.Fatalf("children after agent message=%d err=%v", count, err)
	}
}
