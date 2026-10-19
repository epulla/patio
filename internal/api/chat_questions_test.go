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
