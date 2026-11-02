package api

import (
	"database/sql"
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
	for _, name := range []string{"Code-Bot", "Billy.2", "Billy Two"} {
		if code := requestJSON(t, h, http.MethodPost, "/api/bots", map[string]any{"name": name, "connector_id": connector}, nil); code != http.StatusBadRequest {
			t.Fatalf("name %q status=%d", name, code)
		}
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

func TestDeleteBotRemovesOnlyOwnedSkillsAndUnlinksShared(t *testing.T) {
	db, h, _ := chatAPI(t)
	for _, id := range []string{"bot-a", "bot-b"} {
		if _, err := db.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai) VALUES(?,?,?,1,1)`, id, id, id+"@test"); err != nil {
			t.Fatal(err)
		}
	}
	create := func(name string, ids []string) Skill {
		t.Helper()
		var skill Skill
		if code := requestJSON(t, h, http.MethodPost, "/api/skills", map[string]any{"name": name, "description": name, "bot_ids": ids}, &skill); code != http.StatusCreated {
			t.Fatalf("create %s status=%d", name, code)
		}
		return skill
	}
	only := create("only-a", []string{"bot-a"})
	shared := create("shared-ab", []string{"bot-a", "bot-b"})
	if code := requestJSON(t, h, http.MethodDelete, "/api/bots/bot-a", nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete bot status=%d", code)
	}
	if _, err := SkillByName(db, only.Name); err != sql.ErrNoRows {
		t.Fatalf("only-bot skill remains: %v", err)
	}
	remaining, err := SkillByName(db, shared.Name)
	if err != nil || len(remaining.BotIDs) != 1 || remaining.BotIDs[0] != "bot-b" {
		t.Fatalf("shared skill=%#v err=%v", remaining, err)
	}
	var action, snapshot string
	if err := db.QueryRow(`SELECT action,snapshot_json FROM item_history WHERE item_type='skill' AND item_id=? ORDER BY rowid DESC LIMIT 1`, only.ID).Scan(&action, &snapshot); err != nil || action != "deleted" || !strings.Contains(snapshot, `"bot_ids":["bot-a"]`) {
		t.Fatalf("deleted history action=%q snapshot=%s err=%v", action, snapshot, err)
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
	var none chat
	if code := requestJSON(t, h, http.MethodPost, "/api/chats", map[string]any{"bot_id": "none", "connector_id": connector}, &none); code != http.StatusCreated || none.BotID != nil {
		t.Fatalf("explicit no bot = %d %#v", code, none.BotID)
	}
	var martin chat
	if code := requestJSON(t, h, http.MethodPost, "/api/chats", map[string]any{"bot_id": "ai", "connector_id": connector}, &martin); code != http.StatusCreated || martin.BotID == nil || *martin.BotID != "ai" {
		t.Fatalf("explicit bot = %d %#v", code, martin.BotID)
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

func TestChatPromptFiltersScopedSkillsByBot(t *testing.T) {
	db, _, _ := chatAPI(t)
	for _, botID := range []string{"prompt-a", "prompt-b"} {
		if _, err := db.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai) VALUES(?,?,?,1,1)`, botID, botID, botID+"@test"); err != nil {
			t.Fatal(err)
		}
	}
	for _, chat := range []struct {
		id  string
		bot any
	}{{"chat-a", "prompt-a"}, {"chat-b", "prompt-b"}, {"chat-neutral", nil}} {
		if _, err := db.Exec(`INSERT INTO chat(id,title,created_at,updated_at,bot_id) VALUES(?,?,'now','now',?)`, chat.id, chat.id, chat.bot); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO skill(id,name,description,body,enabled,kind,created_at,updated_at) VALUES('scoped-prompt','scoped-prompt','prompt skill','ONLY_FOR_A',1,'skill','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO skill_bot(skill_id,bot_id) VALUES('scoped-prompt','prompt-a')`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		chat string
		want bool
	}{{"chat-a", true}, {"chat-b", false}, {"chat-neutral", false}} {
		prompt, err := ChatPrompt(db, tc.chat)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(prompt, "scoped-prompt") != tc.want {
			t.Errorf("chat %s scoped skill presence=%v", tc.chat, strings.Contains(prompt, "scoped-prompt"))
		}
	}
}
