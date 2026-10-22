package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestChatPayloadIncludesLinksAndMessageSender(t *testing.T) {
	db, handler, _ := chatAPI(t)
	if _, err := db.Exec(`INSERT INTO chat(id,title,created_at,updated_at,parent_chat_id) VALUES('parent','Parent','1','1',NULL),('child','Child','2','2','parent')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at,from_chat_id) VALUES('msg','parent','user','hello','3','child')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO chat_queue(id,chat_id,text,attachments,created_at,from_chat_id) VALUES('queued','parent','queued','[]','4','child')`); err != nil {
		t.Fatal(err)
	}
	_, body := requestRaw(t, handler, http.MethodGet, "/api/chats/parent", nil)
	for _, field := range []string{`"parent":null`, `"children":[{"id":"child","title":"Child"}]`, `"from_chat_id":"child"`, `"from_chat_title":"Child"`} {
		if !strings.Contains(body, field) {
			t.Fatalf("payload missing %s: %s", field, body)
		}
	}
	_, body = requestRaw(t, handler, http.MethodGet, "/api/chats/child", nil)
	if !strings.Contains(body, `"parent":{"id":"parent","title":"Parent"}`) {
		t.Fatalf("parent relationship missing: %s", body)
	}
	_, body = requestRaw(t, handler, http.MethodGet, "/api/chats/parent/queue", nil)
	if !strings.Contains(body, `"from_chat_id":"child"`) || !strings.Contains(body, `"from_chat_title":"Child"`) {
		t.Fatalf("queue sender fields missing: %s", body)
	}
}
