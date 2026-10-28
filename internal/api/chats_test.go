package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zalando/go-keyring"
	"patio/internal/db"
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
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
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
				if code := requestJSON(t, handler, http.MethodGet, "/api/settings/folders", nil, &list); code != http.StatusOK || len(list) != 1 || list[0].Path != canonicalRoot {
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
	allowed, err := filepath.EvalSymlinks(allowed)
	if err != nil {
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
	canonicalChild, err := filepath.EvalSymlinks(child)
	if err != nil {
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
			if test.name == "allowed child" && got.Workdir != canonicalChild {
				t.Fatalf("workdir = %q", got.Workdir)
			}
			if test.name == "default folder" {
				info, err := os.Stat(got.Workdir)
				if err != nil || !info.IsDir() || runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 || got.Folder != nil || got.Title != "New chat" {
					t.Fatalf("default chat = %#v; dir stat = %v, %v", got, info, err)
				}
				canonicalWorkdir, err := filepath.EvalSymlinks(got.Workdir)
				wantWorkdir, wantErr := filepath.EvalSymlinks(filepath.Join(root, "chats", got.ID))
				if err != nil || wantErr != nil || canonicalWorkdir != wantWorkdir {
					t.Fatalf("default workdir = %q, canonical = %q, want = %q, err = %v", got.Workdir, canonicalWorkdir, wantWorkdir, err)
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
	var listed struct {
		Chats []chat `json:"chats"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats", nil, &listed); code != http.StatusOK || len(listed.Chats) != 2 {
		t.Fatalf("list status/chats = %d, %#v", code, listed.Chats)
	}
	chats := listed.Chats
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
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats", nil, &listed); code != http.StatusOK || listed.Chats[0].ID != newest {
		t.Fatalf("equal timestamp chat order = %#v, want first %q", chats, newest)
	}
}

func TestChatListPaging(t *testing.T) {
	database, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, database)
	for i := 0; i < 3; i++ {
		var created chat
		if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated {
			t.Fatalf("create chat = %d", code)
		}
		if _, err := database.Exec(`UPDATE chat SET updated_at=? WHERE id=?`, string(rune('a'+i)), created.ID); err != nil {
			t.Fatal(err)
		}
	}
	var page struct {
		Chats []chat  `json:"chats"`
		Next  *string `json:"next_cursor"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats?limit=2", nil, &page); code != http.StatusOK || len(page.Chats) != 2 || page.Next == nil {
		t.Fatalf("page 1 = %d, %#v", code, page)
	}
	if page.Chats[0].UpdatedAt != "c" || page.Chats[1].UpdatedAt != "b" {
		t.Fatalf("page 1 order = %#v", page.Chats)
	}
	firstIDs := []string{page.Chats[0].ID, page.Chats[1].ID}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats?limit=2&cursor="+url.QueryEscape(*page.Next), nil, &page); code != http.StatusOK || len(page.Chats) != 1 || page.Next != nil || page.Chats[0].ID == firstIDs[0] || page.Chats[0].ID == firstIDs[1] {
		t.Fatalf("page 2 = %d, %#v", code, page)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats?cursor=bad", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("malformed cursor status = %d", code)
	}
}

func TestChatListRuntimeApprovalAndOtherWindow(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	server := New(database, "local", false, "dev", root, Options{WindowID: "current-window"})
	var created chat
	if code := requestJSON(t, server.handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated {
		t.Fatalf("create chat = %d", code)
	}
	if _, err := database.Exec(`INSERT INTO chat_approval(id,chat_id,tool_name,input_json,status,auto,created_at) VALUES(?,?,?,'{}','pending',0,?)`, uid(), created.ID, "tool", now()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO app_window(id,pid,heartbeat_at) VALUES(?,?,?)`, "other-window", 123, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO chat_turn(chat_id,window_id,started_at) VALUES(?,?,?)`, created.ID, "other-window", now()); err != nil {
		t.Fatal(err)
	}
	runtime := server.chats.runtime(created.ID)
	runtime.mu.Lock()
	runtime.busy = true
	runtime.mu.Unlock()
	var listed struct {
		Chats []struct {
			Running      bool `json:"running"`
			PendingCount int  `json:"pending_count"`
			OtherWindow  bool `json:"other_window"`
		} `json:"chats"`
	}
	if code := requestJSON(t, server.handler, http.MethodGet, "/api/chats", nil, &listed); code != http.StatusOK || len(listed.Chats) != 1 {
		t.Fatalf("list status/chats = %d, %#v", code, listed.Chats)
	}
	got := listed.Chats[0]
	if !got.Running || got.PendingCount != 1 || !got.OtherWindow {
		t.Fatalf("list status fields = %#v", got)
	}
}

func TestChatListSkipsLocalCapabilityProbe(t *testing.T) {
	keyring.MockInit()
	var calls atomic.Int32
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"test"}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[]}`))
		case "/api/show":
			_, _ = w.Write([]byte(`{"capabilities":["completion","tools"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ollama.Close()
	database, handler, _ := chatAPI(t)
	connectorID := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,base_url,model,created_at) VALUES(?,'local','Local',?,'qwen3:4b',?)`, connectorID, ollama.URL, now()); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := 0; i < 2; i++ {
		var created chat
		if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated {
			t.Fatalf("create status = %d", code)
		}
		ids = append(ids, created.ID)
	}
	calls.Store(0)
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats", nil, nil); code != http.StatusOK || calls.Load() != 0 {
		t.Fatalf("list status/calls = %d/%d, want 200/0", code, calls.Load())
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats/"+ids[0], nil, nil); code != http.StatusOK || calls.Load() == 0 {
		t.Fatalf("detail status/calls = %d/%d, want 200/>0", code, calls.Load())
	}
}

func TestChatListPagingDeletedCursorSameTimestamp(t *testing.T) {
	database, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, database)
	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		var created chat
		if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated {
			t.Fatalf("create chat = %d", code)
		}
		ids = append(ids, created.ID)
	}
	if _, err := database.Exec(`UPDATE chat SET updated_at='same'`); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Chats []chat  `json:"chats"`
		Next  *string `json:"next_cursor"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats?limit=1", nil, &page); code != http.StatusOK || len(page.Chats) != 1 || page.Next == nil {
		t.Fatalf("page 1 = %d, %#v", code, page)
	}
	cursorID := page.Chats[0].ID
	if code := requestJSON(t, handler, http.MethodDelete, "/api/chats/"+cursorID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete cursor chat = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats?limit=2&cursor="+url.QueryEscape(*page.Next), nil, &page); code != http.StatusOK || len(page.Chats) != 2 {
		t.Fatalf("page after deleted cursor = %d, %#v", code, page)
	}
	for _, id := range ids {
		if id == cursorID {
			continue
		}
		found := false
		for _, got := range page.Chats {
			found = found || got.ID == id
		}
		if !found {
			t.Errorf("page after deleted cursor missing chat %q: %#v", id, page.Chats)
		}
	}
}

func TestChatCreateAutoAllow(t *testing.T) {
	_, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, handler.(*Server).db)
	var defaults chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &defaults); code != http.StatusCreated || !defaults.AutoAllow {
		t.Fatalf("create default auto_allow chat (on by default) = %d, %#v", code, defaults)
	}
	var disabled chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{"connector_id": connectorID, "auto_allow": false}, &disabled); code != http.StatusCreated || disabled.AutoAllow {
		t.Fatalf("create chat with auto_allow explicitly off = %d, %#v", code, disabled)
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

func TestChatDetailVersion(t *testing.T) {
	database, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, database)
	var created chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated {
		t.Fatalf("create chat = %d", code)
	}
	path := "/api/chats/" + created.ID
	var got struct {
		Version string `json:"version"`
	}
	if code := requestJSON(t, handler, http.MethodGet, path, nil, &got); code != http.StatusOK || got.Version == "" {
		t.Fatalf("get chat version = %d, %q", code, got.Version)
	}
	if code, body := requestRaw(t, handler, http.MethodGet, path+"?version="+url.QueryEscape(got.Version), nil); code != http.StatusOK || strings.TrimSpace(body) != `{"unchanged":true}` {
		t.Fatalf("unchanged chat = %d, %s", code, body)
	}
	version := got.Version
	if _, err := InsertChatMessage(database, created.ID, "user", "new message"); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodGet, path, nil, &got); code != http.StatusOK || got.Version == version {
		t.Fatalf("message changed version = %d, %q", code, got.Version)
	}
	version = got.Version
	approvalID := uid()
	if _, err := database.Exec(`INSERT INTO chat_approval(id,chat_id,tool_name,input_json,created_at) VALUES(?,?,?, '{}',?)`, approvalID, created.ID, "write", now()); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodGet, path, nil, &got); code != http.StatusOK || got.Version == version {
		t.Fatalf("approval changed version = %d, %q", code, got.Version)
	}
	version = got.Version
	if code := requestJSON(t, handler, http.MethodPost, path+"/approvals/"+approvalID, map[string]bool{"allow": true}, nil); code != http.StatusOK {
		t.Fatalf("answer approval = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodGet, path, nil, &got); code != http.StatusOK || got.Version == version {
		t.Fatalf("answered approval changed version = %d, %q", code, got.Version)
	}
}

func TestChatThinkingUpdate(t *testing.T) {
	database, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, database)
	var created chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated || !created.Thinking {
		t.Fatalf("create chat = %d, %#v", code, created)
	}
	var listed struct {
		Chats []chat `json:"chats"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats", nil, &listed); code != http.StatusOK || len(listed.Chats) != 1 || !listed.Chats[0].Thinking {
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

func TestChatThinkingCapabilities(t *testing.T) {
	_, handler, _ := chatAPI(t)
	tests := []struct {
		kind      string
		supported bool
		reason    string
	}{
		{"claude", true, "Some models always think"},
		{"codex", true, ""},
		{"copilot", true, ""},
		{"local", true, ""},
		{"opencode", false, "opencode picks reasoning with model variants"},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			id := uid()
			if _, err := handler.(*Server).db.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,?,?,?)`, id, test.kind, test.kind, now()); err != nil {
				t.Fatal(err)
			}
			var created chat
			if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": id}, &created); code != http.StatusCreated {
				t.Fatalf("create chat = %d", code)
			}
			want := thinkingCapability{Supported: test.supported, Reason: test.reason}
			if created.Capabilities.Thinking != want {
				t.Fatalf("create capabilities = %#v, want %#v", created.Capabilities.Thinking, want)
			}
			var detail struct {
				Chat chat `json:"chat"`
			}
			if code := requestJSON(t, handler, http.MethodGet, "/api/chats/"+created.ID, nil, &detail); code != http.StatusOK || detail.Chat.Capabilities.Thinking != want {
				t.Fatalf("get capabilities = %d %#v, want %#v", code, detail.Chat.Capabilities.Thinking, want)
			}
		})
	}
}

func TestChatToolsRemovedFromAPI(t *testing.T) {
	database, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, database)
	var created chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated {
		t.Fatalf("create chat = %d, %#v", code, created)
	}
	data, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"tools"`) {
		t.Fatalf("chat API still exposes tools: %s", data)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+created.ID, map[string]bool{"tools": false}, nil); code != http.StatusBadRequest {
		t.Fatalf("tools-only update = %d, want %d", code, http.StatusBadRequest)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{"connector_id": connectorID, "tools": false}, nil); code != http.StatusBadRequest {
		t.Fatalf("create with removed tools field = %d, want %d", code, http.StatusBadRequest)
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

func TestChatBotName(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`INSERT INTO "user"(id,name,email,email_verified,is_ai) VALUES('billy','Billy','billy@test',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE chat SET bot_id=NULL WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	if name, err := ChatBotName(database, chatID); err != nil || name != "" {
		t.Fatalf("no bot = %q, %v", name, err)
	}
	if _, err := database.Exec(`UPDATE chat SET bot_id='billy' WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	if name, err := ChatBotName(database, chatID); err != nil || name != "Billy" {
		t.Fatalf("bot = %q, %v", name, err)
	}
}
