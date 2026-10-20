package api

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/defaults", nil, &defaults); code != http.StatusOK || defaults.ConnectorID != nil || defaults.Folder != nil || !defaults.AutoAllow || !defaults.Thinking {
		t.Fatalf("initial defaults = %d, %#v", code, defaults)
	}
	if code, raw := requestRaw(t, handler, http.MethodGet, "/api/settings/defaults", nil); code != http.StatusOK || strings.Contains(raw, `"tools"`) {
		t.Fatalf("defaults API still exposes tools: %d %s", code, raw)
	}
	for _, invalid := range []settingsDefaults{
		{ConnectorID: stringPtr("missing"), Folder: &allowed, AutoAllow: true, Thinking: true},
		{ConnectorID: &connectorID, Folder: stringPtr(filepath.Join(root, "outside")), AutoAllow: true, Thinking: true},
	} {
		if code := requestJSON(t, handler, http.MethodPut, "/api/settings/defaults", invalid, nil); code != http.StatusBadRequest {
			t.Fatalf("invalid defaults status = %d for %#v", code, invalid)
		}
	}
	want := settingsDefaults{Language: "system", ConnectorID: &connectorID, Folder: &allowed, AutoAllow: false, Thinking: true}
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
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/defaults", settingsDefaults{Language: "system", ConnectorID: &connectorID, Folder: &allowed, AutoAllow: true, Thinking: true}, nil); code != http.StatusOK {
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
	want := settingsDefaults{Language: "system", ConnectorID: &connectorID, Folder: &canonical, AutoAllow: true, Thinking: true}
	input := settingsDefaults{Language: "system", ConnectorID: &connectorID, Folder: &alias, AutoAllow: true, Thinking: true}
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
	want := settingsDefaults{Language: "system", Model: "default-model", ConnectorID: &first, Folder: &allowed, AutoAllow: false, Thinking: true, MartinPersona: true}
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/defaults", want, nil); code != http.StatusOK {
		t.Fatalf("save defaults = %d", code)
	}
	var created chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{}, &created); code != http.StatusCreated {
		t.Fatalf("create defaults chat = %d, %#v", code, created)
	}
	if created.ConnectorID == nil || *created.ConnectorID != first || created.Model != "default-model" || created.Folder == nil || *created.Folder != allowed || created.AutoAllow || !created.Thinking {
		t.Fatalf("created chat didn't use defaults: %#v", created)
	}
	var explicit chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{
		"connector_id": second, "folder": nil, "auto_allow": true, "thinking": false,
	}, &explicit); code != http.StatusCreated {
		t.Fatalf("create explicit chat = %d, %#v", code, explicit)
	}
	if explicit.ConnectorID == nil || *explicit.ConnectorID != second || explicit.Folder != nil || !explicit.AutoAllow || explicit.Thinking {
		t.Fatalf("explicit chat values lost: %#v", explicit)
	}
	var fallback chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{"auto_allow": true}, &fallback); code != http.StatusCreated || fallback.ConnectorID == nil || *fallback.ConnectorID != first {
		t.Fatalf("unset connector fallback = %d, %#v", code, fallback)
	}
	var other chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{"connector_id": second}, &other); code != http.StatusCreated || other.Model != "" {
		t.Fatalf("other connector inherited default model = %d, %#v", code, other)
	}
	var chosen chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{"model": "explicit-model"}, &chosen); code != http.StatusCreated || chosen.Model != "explicit-model" {
		t.Fatalf("explicit model did not win = %d, %#v", code, chosen)
	}
}

func TestSettingsDefaultsClearModelWhenConnectorChanges(t *testing.T) {
	database, handler, _ := chatAPI(t)
	first := createTestConnector(t, database)
	second := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,'local','Local',?)`, second, now()); err != nil {
		t.Fatal(err)
	}
	put := func(body map[string]any) settingsDefaults {
		t.Helper()
		var got settingsDefaults
		if code := requestJSON(t, handler, http.MethodPut, "/api/settings/defaults", body, &got); code != http.StatusOK {
			t.Fatalf("put defaults = %d", code)
		}
		return got
	}
	got := put(map[string]any{"connector_id": first, "model": "m1"})
	if got.Model != "m1" {
		t.Fatalf("model round trip = %q", got.Model)
	}
	got = put(map[string]any{"connector_id": second})
	if got.Model != "" {
		t.Fatalf("model after connector change = %q", got.Model)
	}
	got = put(map[string]any{"model": "m2"})
	if got.Model != "m2" {
		t.Fatalf("partial model update = %q", got.Model)
	}
}

func TestSpanishNotificationTitleAndLanguageValidation(t *testing.T) {
	database, handler, _ := chatAPI(t)
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/defaults", map[string]string{"language": "es"}, nil); code != http.StatusOK {
		t.Fatalf("set Spanish language = %d", code)
	}
	s := handler.(*Server)
	if err := s.Notify("chat", "Reply finished: Test", "", "/ai/chats/test", false); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := database.QueryRow(`SELECT title FROM notification ORDER BY rowid DESC LIMIT 1`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Respuesta finalizada: Test" {
		t.Fatalf("Spanish title = %q", title)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/defaults", map[string]string{"language": "fr"}, nil); code != http.StatusBadRequest {
		t.Fatalf("invalid language status = %d", code)
	}
}

func TestSettingsAboutAndReveal(t *testing.T) {
	database, handler, root := chatAPI(t)
	server := handler.(*Server)
	server.showHidden = true
	server.logPath = filepath.Join(root, "patio.log")
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
	aboutDBPath, pathErr := filepath.EvalSymlinks(about.DBPath)
	aboutDataDir, dirErr := filepath.EvalSymlinks(about.DataDir)
	if pathErr != nil || dirErr != nil || about.Version != "dev" || about.OS != runtime.GOOS || about.Arch != runtime.GOARCH || aboutDBPath != canonicalDBPath || aboutDataDir != canonicalRoot || about.LogPath != server.logPath || about.DataDirBytes < info.Size() || about.Approximate || about.Counts != (settingsCounts{Boards: 1, Tickets: 1, Chats: 1, Connectors: 1, MCPServers: 1, Skills: 1}) || about.SchemaVersion != schema || !about.ShowHidden || about.SecretsStore == "" || about.RepoURL != "https://github.com/epulla/patio" {
		t.Fatalf("about fields = %#v", about)
	}
	var revealed []string
	server.revealFile = func(path string) error { revealed = append(revealed, path); return nil }
	for _, target := range []string{"database", "data_dir", "logs"} {
		if code := requestJSON(t, handler, http.MethodPost, "/api/settings/reveal", map[string]string{"target": target}, nil); code != http.StatusNoContent {
			t.Fatalf("reveal %s status = %d", target, code)
		}
	}
	if len(revealed) != 3 {
		t.Fatalf("revealed paths = %#v", revealed)
	}
	revealedDBPath, dbErr := filepath.EvalSymlinks(revealed[0])
	revealedDataDir, dirErr := filepath.EvalSymlinks(revealed[1])
	if len(revealed) != 3 || dbErr != nil || dirErr != nil || revealedDBPath != canonicalDBPath || revealedDataDir != canonicalRoot || revealed[2] != server.logPath {
		t.Fatalf("revealed paths = %#v", revealed)
	}
	for _, body := range []any{map[string]string{}, map[string]string{"target": "chat"}} {
		if code := requestJSON(t, handler, http.MethodPost, "/api/settings/reveal", body, nil); code != http.StatusBadRequest {
			t.Fatalf("invalid reveal target status = %d", code)
		}
	}
}
