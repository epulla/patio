package api

import (
	"net/http"
	"testing"
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
