package api

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestSettingsDefaultsRoundTripAndValidation(t *testing.T) {
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
	if code := requestJSON(t, handler, http.MethodPost, "/api/settings/folders", map[string]string{"path": allowed}, nil); code != http.StatusCreated {
		t.Fatalf("add allowed folder = %d", code)
	}
	var defaults settingsDefaults
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/defaults", nil, &defaults); code != http.StatusOK || defaults.ConnectorID != nil || defaults.Folder != nil || !defaults.AutoAllow || !defaults.Tools || defaults.Thinking {
		t.Fatalf("initial defaults = %d, %#v", code, defaults)
	}
	for _, invalid := range []settingsDefaults{
		{ConnectorID: stringPtr("missing"), Folder: &allowed, AutoAllow: true, Tools: true},
		{ConnectorID: &connectorID, Folder: stringPtr(filepath.Join(root, "outside")), AutoAllow: true, Tools: true},
	} {
		if code := requestJSON(t, handler, http.MethodPut, "/api/settings/defaults", invalid, nil); code != http.StatusBadRequest {
			t.Fatalf("invalid defaults status = %d for %#v", code, invalid)
		}
	}
	want := settingsDefaults{ConnectorID: &connectorID, Folder: &allowed, AutoAllow: false, Tools: false, Thinking: true}
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/defaults", want, &defaults); code != http.StatusOK || !reflect.DeepEqual(defaults, want) {
		t.Fatalf("save defaults = %d, %#v, want %#v", code, defaults, want)
	}
	defaults = settingsDefaults{}
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/defaults", nil, &defaults); code != http.StatusOK || !reflect.DeepEqual(defaults, want) {
		t.Fatalf("read defaults = %d, %#v, want %#v", code, defaults, want)
	}
}

func TestSettingsDefaultsDeletedReferencesFallBack(t *testing.T) {
	database, handler, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	allowed := filepath.Join(root, "allowed")
	if err := os.Mkdir(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	var folder allowedFolder
	if code := requestJSON(t, handler, http.MethodPost, "/api/settings/folders", map[string]string{"path": allowed}, &folder); code != http.StatusCreated {
		t.Fatalf("add allowed folder = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/defaults", settingsDefaults{ConnectorID: &connectorID, Folder: &allowed, AutoAllow: true, Tools: true}, nil); code != http.StatusOK {
		t.Fatalf("save defaults = %d", code)
	}
	if _, err := database.Exec(`DELETE FROM connector WHERE id=?`, connectorID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/settings/folders/"+folder.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete folder = %d", code)
	}
	var got settingsDefaults
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/defaults", nil, &got); code != http.StatusOK || got.ConnectorID != nil || got.Folder != nil {
		t.Fatalf("deleted defaults fallback = %d, %#v", code, got)
	}
}

func TestSettingsDefaultsNormalizeFolderAliases(t *testing.T) {
	database, handler, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	canonical := filepath.Join(root, "allowed")
	alias := filepath.Join(root, "alias")
	if err := os.Mkdir(canonical, 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, alias); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/settings/folders", map[string]string{"path": canonical}, nil); code != http.StatusCreated {
		t.Fatalf("add allowed folder = %d", code)
	}
	var folders []allowedFolder
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/folders", nil, &folders); code != http.StatusOK || len(folders) != 1 || folders[0].Path != canonical {
		t.Fatalf("allowed folders = %d, %#v", code, folders)
	}
	want := settingsDefaults{ConnectorID: &connectorID, Folder: &canonical, AutoAllow: true, Tools: true}
	input := settingsDefaults{ConnectorID: &connectorID, Folder: &alias, AutoAllow: true, Tools: true}
	var got settingsDefaults
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/defaults", input, &got); code != http.StatusOK || !reflect.DeepEqual(got, want) {
		t.Fatalf("save aliased defaults = %d, %#v; want %#v", code, got, want)
	}
	got = settingsDefaults{}
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/defaults", nil, &got); code != http.StatusOK || !reflect.DeepEqual(got, want) {
		t.Fatalf("read normalized defaults = %d, %#v; want %#v", code, got, want)
	}
}

func TestCreateChatUsesDefaultsAndExplicitValuesWin(t *testing.T) {
	database, handler, root := chatAPI(t)
	first := createTestConnector(t, database)
	second := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'local','Local',?)`, second, now()); err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(root, "allowed")
	if err := os.Mkdir(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	allowed, err := filepath.EvalSymlinks(allowed)
	if err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/settings/folders", map[string]string{"path": allowed}, nil); code != http.StatusCreated {
		t.Fatalf("add allowed folder = %d", code)
	}
	want := settingsDefaults{ConnectorID: &first, Folder: &allowed, AutoAllow: false, Tools: false, Thinking: true}
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/defaults", want, nil); code != http.StatusOK {
		t.Fatalf("save defaults = %d", code)
	}
	var created chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{}, &created); code != http.StatusCreated {
		t.Fatalf("create defaults chat = %d, %#v", code, created)
	}
	if created.ConnectorID == nil || *created.ConnectorID != first || created.Folder == nil || *created.Folder != allowed || created.AutoAllow || created.Tools || !created.Thinking {
		t.Fatalf("created chat didn't use defaults: %#v", created)
	}
	var explicit chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{
		"connector_id": second, "folder": nil, "auto_allow": true, "tools": true, "thinking": false,
	}, &explicit); code != http.StatusCreated {
		t.Fatalf("create explicit chat = %d, %#v", code, explicit)
	}
	if explicit.ConnectorID == nil || *explicit.ConnectorID != second || explicit.Folder != nil || !explicit.AutoAllow || !explicit.Tools || explicit.Thinking {
		t.Fatalf("explicit chat values lost: %#v", explicit)
	}
	var fallback chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{"auto_allow": true}, &fallback); code != http.StatusCreated || fallback.ConnectorID == nil || *fallback.ConnectorID != first {
		t.Fatalf("unset connector fallback = %d, %#v", code, fallback)
	}
}

func TestSettingsAboutAndReveal(t *testing.T) {
	database, handler, root := chatAPI(t)
	server := handler.(*Server)
	server.showHidden = true
	dbPath := filepath.Join(root, "test.db")
	canonicalDBPath, err := filepath.EvalSymlinks(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	connectorID := createTestConnector(t, database)
	if _, err := database.Exec(`INSERT INTO mcp_server(id,name,kind,args_json,env_keys_json,auth,created_at) VALUES(?, 'test', 'stdio', '[]', '[]', 'none', ?)`, uid(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO skill(id,name,description,body,created_at,updated_at) VALUES(?, 'test', '', '', ?, ?)`, uid(), now(), now()); err != nil {
		t.Fatal(err)
	}
	boardID, statusID, userID, ticketID := uid(), uid(), "local", uid()
	if _, err := database.Exec(`INSERT INTO board(id,name,short_name) VALUES(?, 'Test board', 'T')`, boardID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO status(id,name,type,board_id) VALUES(?, 'Todo', 'classic', ?)`, statusID, boardID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO ticket(id,title,description,status_id,board_id,reporter_id) VALUES(?, 'Test ticket', '', ?, ?, ?)`, ticketID, statusID, boardID, userID); err != nil {
		t.Fatal(err)
	}
	var created chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated {
		t.Fatalf("create chat = %d", code)
	}
	var about settingsAbout
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/about", nil, &about); code != http.StatusOK {
		t.Fatalf("about status = %d", code)
	}
	var schema int
	if err := database.QueryRow(`PRAGMA user_version`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if about.Version != "dev" || about.OS != runtime.GOOS || about.Arch != runtime.GOARCH || about.DBPath != canonicalDBPath || about.DataDir != canonicalRoot || about.DataDirBytes < info.Size() || about.Approximate || about.Counts != (settingsCounts{Boards: 1, Tickets: 1, Chats: 1, Connectors: 1, MCPServers: 1, Skills: 1}) || about.SchemaVersion != schema || !about.ShowHidden || about.SecretsStore == "" || about.RepoURL != "https://github.com/epulla/my-tickets" {
		t.Fatalf("about fields = %#v", about)
	}
	var revealed []string
	server.revealFile = func(path string) error { revealed = append(revealed, path); return nil }
	for _, target := range []string{"database", "data_dir"} {
		if code := requestJSON(t, handler, http.MethodPost, "/api/settings/reveal", map[string]string{"target": target}, nil); code != http.StatusNoContent {
			t.Fatalf("reveal %s status = %d", target, code)
		}
	}
	if len(revealed) != 2 || revealed[0] != canonicalDBPath || revealed[1] != canonicalRoot {
		t.Fatalf("revealed paths = %#v", revealed)
	}
	for _, body := range []any{map[string]string{}, map[string]string{"target": "chat"}} {
		if code := requestJSON(t, handler, http.MethodPost, "/api/settings/reveal", body, nil); code != http.StatusBadRequest {
			t.Fatalf("invalid reveal target status = %d", code)
		}
	}
}
