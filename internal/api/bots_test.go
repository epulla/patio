package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestBotCRUDHistoryAndProtection(t *testing.T) {
	db, h, _ := chatAPI(t)
	connector := createTestConnector(t, db)
	input := map[string]any{"name": "Billy", "description": "Research assistant", "persona_prompt": "You are Billy. Find answers.", "color": "blue", "connector_id": connector, "model": "m1"}
	var created bot
	if code := requestJSON(t, h, http.MethodPost, "/api/bots", input, &created); code != http.StatusCreated {
		t.Fatalf("create bot=%d", code)
	}
	if created.Name != "Billy" || created.Description != "Research assistant" || created.ConnectorID == nil || *created.ConnectorID != connector {
		t.Fatalf("created=%#v", created)
	}
	if code := requestJSON(t, h, http.MethodPost, "/api/bots", map[string]any{"name": "bIlLy", "connector_id": connector}, nil); code != http.StatusBadRequest {
		t.Fatalf("duplicate status=%d", code)
	}
	input["name"] = "William"
	if code := requestJSON(t, h, http.MethodPatch, "/api/bots/"+created.ID, input, nil); code != http.StatusOK {
		t.Fatalf("update bot=%d", code)
	}
	var history []itemHistory
	if code := requestJSON(t, h, http.MethodGet, "/api/history?item_type=bot&item_id="+created.ID, nil, &history); code != http.StatusOK || len(history) != 2 {
		t.Fatalf("history status=%d rows=%d", code, len(history))
	}
	if code := requestJSON(t, h, http.MethodDelete, "/api/bots/ai", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("delete Martin=%d", code)
	}
	if code := requestJSON(t, h, http.MethodDelete, "/api/bots/"+created.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete bot=%d", code)
	}
}

func TestCreateChatUsesDefaultBotsConnectorAndModel(t *testing.T) {
	db, h, _ := chatAPI(t)
	connector := createTestConnector(t, db)
	if _, err := db.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai,connector_id,model) VALUES('billy','Billy','billy@bot',1,1,?,'b-model')`, connector); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO app_setting(key,value) VALUES('default_bot_id','"billy"') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		t.Fatal(err)
	}
	var created chat
	if code := requestJSON(t, h, http.MethodPost, "/api/chats", map[string]any{}, &created); code != http.StatusCreated {
		t.Fatalf("create chat=%d", code)
	}
	if created.BotID == nil || *created.BotID != "billy" || created.ConnectorID == nil || *created.ConnectorID != connector || created.Model != "b-model" {
		t.Fatalf("chat=%#v", created)
	}
}

func TestChatPromptUsesBotPersona(t *testing.T) {
	db, h, _ := chatAPI(t)
	if _, err := db.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai,description,persona_prompt) VALUES('billy','Billy','billy@test',1,1,'Helper','You are Billy, precise and kind.')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO chat(id,title,created_at,updated_at,bot_id) VALUES('billy-chat','Chat','now','now','billy')`); err != nil {
		t.Fatal(err)
	}
	prompt, err := ChatPrompt(db, "billy-chat")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "You are Billy, precise and kind.") || !strings.HasSuffix(prompt, "This chat's ID is billy-chat.") {
		t.Fatalf("prompt=%q", prompt)
	}
	connector := createTestConnector(t, db)
	if _, err := db.Exec(`INSERT INTO app_setting(key,value) VALUES('connector_id',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, `"`+connector+`"`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO app_setting(key,value) VALUES('default_bot_id','"none"') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		t.Fatal(err)
	}
	var plain chat
	if code := requestJSON(t, h, http.MethodPost, "/api/chats", map[string]any{}, &plain); code != http.StatusCreated {
		t.Fatalf("create plain chat=%d", code)
	}
	prompt, err = ChatPrompt(db, plain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "playful, joyful") {
		t.Fatalf("plain chat received persona: %q", prompt)
	}
}
