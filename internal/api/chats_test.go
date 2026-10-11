package api

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
	"my-tickets/internal/db"
)

func chatAPI(t *testing.T) (*sql.DB, http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	database, err := db.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	return database, New(database, "local", false, "dev", root), root
}

func createTestConnector(t *testing.T, database *sql.DB) string {
	t.Helper()
	id, created := uid(), now()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'claude','Claude',?)`, id, created); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAllowedFolderValidation(t *testing.T) {
	_, handler, root := chatAPI(t)
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path string
		want int
	}{{"relative", "relative", 400}, {"missing", filepath.Join(root, "missing"), 400}, {"file", file, 400}, {"valid", root, 201}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out allowedFolder
			if code := requestJSON(t, handler, http.MethodPost, "/api/settings/folders", map[string]string{"path": test.path}, &out); code != test.want {
				t.Fatalf("add folder status = %d, want %d", code, test.want)
			}
			if test.name == "valid" {
				if code := requestJSON(t, handler, http.MethodPost, "/api/settings/folders", map[string]string{"path": root}, nil); code != http.StatusConflict {
					t.Fatalf("duplicate folder status = %d", code)
				}
				var list []allowedFolder
				if code := requestJSON(t, handler, http.MethodGet, "/api/settings/folders", nil, &list); code != http.StatusOK || len(list) != 1 || list[0].Path != root {
					t.Fatalf("folders = %#v, status %d", list, code)
				}
				if code := requestJSON(t, handler, http.MethodDelete, "/api/settings/folders/"+out.ID, nil, nil); code != http.StatusNoContent {
					t.Fatalf("delete folder status = %d", code)
				}
			}
		})
	}
}

func TestChatCreateValidationAndListOrder(t *testing.T) {
	database, handler, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	allowed := filepath.Join(root, "allowed")
	if err := os.Mkdir(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(root, "allowed-sibling")
	if err := os.Mkdir(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/settings/folders", map[string]string{"path": allowed}, nil); code != http.StatusCreated {
		t.Fatal("add folder failed")
	}
	child := filepath.Join(allowed, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, connector string
		folder          *string
		want            int
	}{{"outside", connectorID, stringPtr(root), 400}, {"prefix sibling", connectorID, &sibling, 400}, {"unknown connector", "missing", nil, 400}, {"allowed child", connectorID, &child, 201}, {"default folder", connectorID, nil, 201}} {
		t.Run(test.name, func(t *testing.T) {
			var got chat
			body := map[string]any{"connector_id": test.connector}
			if test.folder != nil {
				body["folder"] = *test.folder
			}
			if code := requestJSON(t, handler, http.MethodPost, "/api/chats", body, &got); code != test.want {
				t.Fatalf("create status = %d, want %d", code, test.want)
			}
			if test.name == "allowed child" && got.Workdir != child {
				t.Fatalf("workdir = %q", got.Workdir)
			}
			if test.name == "default folder" {
				info, err := os.Stat(got.Workdir)
				if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || got.Folder != nil || got.Title != "New chat" {
					t.Fatalf("default chat = %#v; dir stat = %v, %v", got, info, err)
				}
				if _, err := database.Exec(`UPDATE chat SET updated_at='same' WHERE id=?`, got.ID); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	if _, err := database.Exec(`UPDATE chat SET updated_at=CASE WHEN rowid=(SELECT MIN(rowid) FROM chat) THEN '2020' ELSE '2021' END`); err != nil {
		t.Fatal(err)
	}
	var chats []chat
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats", nil, &chats); code != http.StatusOK || len(chats) != 2 {
		t.Fatalf("list status/chats = %d, %#v", code, chats)
	}
	if chats[0].UpdatedAt != "2021" {
		t.Fatalf("timestamp chat order = %#v", chats)
	}
	if _, err := database.Exec(`UPDATE chat SET updated_at='same'`); err != nil {
		t.Fatal(err)
	}
	var newest string
	if err := database.QueryRow(`SELECT id FROM chat ORDER BY rowid DESC LIMIT 1`).Scan(&newest); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats", nil, &chats); code != http.StatusOK || chats[0].ID != newest {
		t.Fatalf("equal timestamp chat order = %#v, want first %q", chats, newest)
	}
}

func TestChatCreateAutoAllow(t *testing.T) {
	_, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, handler.(*Server).db)
	var defaults chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &defaults); code != http.StatusCreated || defaults.AutoAllow {
		t.Fatalf("create default auto_allow chat = %d, %#v", code, defaults)
	}
	var enabled chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{"connector_id": connectorID, "auto_allow": true}, &enabled); code != http.StatusCreated || !enabled.AutoAllow {
		t.Fatalf("create auto_allow chat = %d, %#v", code, enabled)
	}
}

func TestChatGetMessagesDeleteAndApproval(t *testing.T) {
	database, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, database)
	var created chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated {
		t.Fatalf("create chat = %d", code)
	}
	for _, content := range []string{"first", "second"} {
		if _, err := InsertChatMessage(database, created.ID, "user", content); err != nil {
			t.Fatal(err)
		}
	}
	approvalID := uid()
	if _, err := database.Exec(`INSERT INTO chat_approval(id,chat_id,tool_name,input_json,created_at) VALUES(?,?,?, '{}',?)`, approvalID, created.ID, "write", now()); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Chat             chat          `json:"chat"`
		Messages         []ChatMessage `json:"messages"`
		PendingApprovals []struct {
			ID string `json:"id"`
		} `json:"pending_approvals"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats/"+created.ID, nil, &got); code != http.StatusOK || len(got.Messages) != 2 || got.Messages[0].Content != "first" || got.Messages[1].Content != "second" || len(got.PendingApprovals) != 1 {
		t.Fatalf("get chat status/data = %d, %#v", code, got)
	}
	approvalPath := "/api/chats/" + created.ID + "/approvals/" + approvalID
	if code := requestJSON(t, handler, http.MethodPost, approvalPath, map[string]bool{"allow": true}, nil); code != http.StatusOK {
		t.Fatalf("answer approval = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPost, approvalPath, map[string]bool{"allow": false}, nil); code != http.StatusConflict {
		t.Fatalf("answer twice = %d", code)
	}
	deniedID := uid()
	if _, err := database.Exec(`INSERT INTO chat_approval(id,chat_id,tool_name,input_json,created_at) VALUES(?,?,?, '{}',?)`, deniedID, created.ID, "delete", now()); err != nil {
		t.Fatal(err)
	}
	deniedPath := "/api/chats/" + created.ID + "/approvals/" + deniedID
	if code := requestJSON(t, handler, http.MethodPost, deniedPath, map[string]bool{"allow": false}, nil); code != http.StatusOK {
		t.Fatalf("deny approval = %d", code)
	}
	for id, want := range map[string]string{approvalID: "allowed", deniedID: "denied"} {
		var status string
		if err := database.QueryRow(`SELECT status FROM chat_approval WHERE id=?`, id).Scan(&status); err != nil || status != want {
			t.Fatalf("approval %q status = %q, %v", id, status, err)
		}
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats/"+created.ID+"/approvals/missing", map[string]bool{"allow": true}, nil); code != http.StatusNotFound {
		t.Fatalf("answer missing approval = %d", code)
	}
	if err := SetChatSessionID(database, created.ID, "session"); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/chats/"+created.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete chat = %d", code)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=?`, created.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade message count = %d, %v", count, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_approval WHERE chat_id=?`, created.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade approval count = %d, %v", count, err)
	}
	if _, err := os.Stat(created.Workdir); !os.IsNotExist(err) {
		t.Fatalf("chat directory remains: %v", err)
	}
}

func TestChatThinkingUpdate(t *testing.T) {
	database, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, database)
	var created chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated || created.Thinking || !created.Tools {
		t.Fatalf("create chat = %d, %#v", code, created)
	}
	var listed []chat
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats", nil, &listed); code != http.StatusOK || len(listed) != 1 || listed[0].Thinking {
		t.Fatalf("list chats = %d, %#v", code, listed)
	}
	updated := true
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+created.ID, map[string]bool{"thinking": updated}, &created); code != http.StatusOK || !created.Thinking {
		t.Fatalf("enable thinking = %d, %#v", code, created)
	}
	var got struct {
		Chat chat `json:"chat"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats/"+created.ID, nil, &got); code != http.StatusOK || !got.Chat.Thinking {
		t.Fatalf("get chat thinking = %d, %#v", code, got.Chat)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+created.ID, map[string]string{"title": ""}, nil); code != http.StatusBadRequest {
		t.Fatalf("invalid update status = %d", code)
	}
	runtime := handler.(*Server).chats.runtime(created.ID)
	runtime.mu.Lock()
	runtime.busy = true
	runtime.mu.Unlock()
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+created.ID, map[string]bool{"thinking": true}, nil); code != http.StatusOK {
		t.Fatalf("unchanged thinking while running = %d", code)
	}
	changed := false
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+created.ID, map[string]bool{"thinking": changed}, nil); code != http.StatusConflict {
		t.Fatalf("thinking change while running = %d, want %d", code, http.StatusConflict)
	}
	runtime.mu.Lock()
	runtime.busy = false
	runtime.mu.Unlock()
}

func TestChatToolsUpdate(t *testing.T) {
	database, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, database)
	var created chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated || !created.Tools {
		t.Fatalf("create chat = %d, %#v", code, created)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+created.ID, map[string]bool{"tools": false}, &created); code != http.StatusOK || created.Tools {
		t.Fatalf("disable tools = %d, %#v", code, created)
	}
	var listed []chat
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats", nil, &listed); code != http.StatusOK || len(listed) != 1 || listed[0].Tools {
		t.Fatalf("list chat tools = %d, %#v", code, listed)
	}
	runtime := handler.(*Server).chats.runtime(created.ID)
	runtime.mu.Lock()
	runtime.busy = true
	runtime.mu.Unlock()
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+created.ID, map[string]bool{"tools": false}, nil); code != http.StatusOK {
		t.Fatalf("unchanged tools while running = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+created.ID, map[string]bool{"tools": true}, nil); code != http.StatusConflict {
		t.Fatalf("tools change while running = %d, want %d", code, http.StatusConflict)
	}
	runtime.mu.Lock()
	runtime.busy = false
	runtime.mu.Unlock()
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+created.ID, map[string]bool{"tools": true}, &created); code != http.StatusOK || !created.Tools {
		t.Fatalf("enable tools = %d, %#v", code, created)
	}
}

func TestDeleteConnectorKeepsChat(t *testing.T) {
	keyring.MockInit()
	database, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, database)
	var created chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated {
		t.Fatalf("create chat = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/connectors/"+connectorID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete connector = %d", code)
	}
	var connector sql.NullString
	if err := database.QueryRow(`SELECT connector_id FROM chat WHERE id=?`, created.ID).Scan(&connector); err != nil || connector.Valid {
		t.Fatalf("chat connector = %#v, %v", connector, err)
	}
}

func stringPtr(value string) *string { return &value }
