package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMCPServerValidation(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	nonExecutable := filepath.Join(root, "not-executable"+filepath.Ext(executable))
	if err := os.WriteFile(nonExecutable, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pathDir := filepath.Join(root, "path")
	if err := os.Mkdir(pathDir, 0o700); err != nil {
		t.Fatal(err)
	}
	executableContents, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pathDir, "found-command"+filepath.Ext(executable)), executableContents, 0o700); err != nil {
		t.Fatal(err)
	}
	pathCommand := "found-command" + filepath.Ext(executable)
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)
	validStdio := mcpServerConfig{Name: "Shell", Kind: "stdio", Command: executable, Args: []string{}, Env: map[string]string{}, Auth: "none"}
	validHTTP := mcpServerConfig{Name: "Remote", Kind: "http", URL: "https://example.test/mcp", Args: []string{}, Env: map[string]string{}, Auth: "oauth"}
	tests := []struct {
		name   string
		config mcpServerConfig
		valid  bool
	}{
		{"valid absolute command", validStdio, true},
		{"valid PATH command", func() mcpServerConfig { c := validStdio; c.Command = pathCommand; return c }(), true},
		{"valid HTTPS", validHTTP, true},
		{"valid localhost HTTP", func() mcpServerConfig { c := validHTTP; c.URL = "http://localhost:8080/mcp"; return c }(), true},
		{"empty name", func() mcpServerConfig { c := validStdio; c.Name = ""; return c }(), false},
		{"long name", func() mcpServerConfig { c := validStdio; c.Name = strings.Repeat("a", 65); return c }(), false},
		{"reserved sanitized name", func() mcpServerConfig { c := validStdio; c.Name = "My Tickets"; return c }(), false},
		{"missing command", func() mcpServerConfig { c := validStdio; c.Command = ""; return c }(), false},
		{"absolute directory command", func() mcpServerConfig { c := validStdio; c.Command = directory; return c }(), false},
		{"absolute non-executable command", func() mcpServerConfig { c := validStdio; c.Command = nonExecutable; return c }(), false},
		{"relative command path", func() mcpServerConfig { c := validStdio; c.Command = "./test-command"; return c }(), false},
		{"unresolved bare command", func() mcpServerConfig { c := validStdio; c.Command = "not-installed"; return c }(), false},
		{"stdio auth", func() mcpServerConfig { c := validStdio; c.Auth = "bearer"; return c }(), false},
		{"unknown kind", func() mcpServerConfig { c := validStdio; c.Kind = "other"; return c }(), false},
		{"unknown auth", func() mcpServerConfig { c := validHTTP; c.Auth = "basic"; return c }(), false},
		{"missing HTTP URL", func() mcpServerConfig { c := validHTTP; c.URL = ""; return c }(), false},
		{"non-HTTP scheme", func() mcpServerConfig { c := validHTTP; c.URL = "ftp://example.test/mcp"; return c }(), false},
		{"remote HTTP", func() mcpServerConfig { c := validHTTP; c.URL = "http://example.test/mcp"; return c }(), false},
		{"URL credentials", func() mcpServerConfig { c := validHTTP; c.URL = "https://user:pass@example.test/mcp"; return c }(), false},
		{"invalid env key", func() mcpServerConfig { c := validHTTP; c.Env = map[string]string{"BAD-KEY": "value"}; return c }(), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "absolute non-executable command" && runtime.GOOS == "windows" {
				t.Skip("Windows has no execute bit")
			}
			err := validateMCPServer(tc.config)
			if (err == nil) != tc.valid {
				t.Fatalf("validateMCPServer() error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestMCPServerCRUDAndSecretSemantics(t *testing.T) {
	keyring.MockInit()
	database, handler, _ := chatAPI(t)
	body := map[string]any{
		"name": "Remote API", "kind": "http", "url": "https://example.test/mcp", "auth": "bearer",
		"bearer_token": "first-bearer-secret", "env": map[string]string{"ZED": "last-env-secret", "ALPHA": "first-env-secret"}, "enabled": true,
	}
	code, raw := requestRaw(t, handler, http.MethodPost, "/api/mcp-servers", body)
	if code != http.StatusCreated || strings.Contains(raw, "first-bearer-secret") || strings.Contains(raw, "first-env-secret") || strings.Contains(raw, "last-env-secret") {
		t.Fatalf("create status/body = %d %s", code, raw)
	}
	var created mcpServer
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "Remote API" || created.Kind != "http" || !created.Enabled || !created.HasBearerToken || created.HasOAuthToken || strings.Join(created.EnvKeys, ",") != "ALPHA,ZED" {
		t.Fatalf("created server = %#v", created)
	}
	if value, err := keyring.Get(connectorService, mcpServerSecret(created.ID, "env")); err != nil || !strings.Contains(value, "first-env-secret") {
		t.Fatalf("environment secret = %q, %v", value, err)
	}
	if value, err := keyring.Get(connectorService, mcpServerSecret(created.ID, "bearer")); err != nil || value != "first-bearer-secret" {
		t.Fatalf("bearer secret = %q, %v", value, err)
	}
	var dbValue string
	if err := database.QueryRow(`SELECT name||kind||command||args_json||env_keys_json||url||auth FROM mcp_server WHERE id=?`, created.ID).Scan(&dbValue); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dbValue, "secret") || strings.Contains(dbValue, "ALPHA\":\"first-env") {
		t.Fatalf("database contains secret value: %q", dbValue)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/mcp-servers/"+created.ID, map[string]any{"env": nil}, nil); code != http.StatusBadRequest {
		t.Fatalf("null environment update status = %d", code)
	}

	for _, tc := range []struct {
		name       string
		body       map[string]any
		wantBearer string
		wantEnv    string
		wantHas    bool
	}{
		{"omitted values keep", map[string]any{"name": "Remote Updated"}, "first-bearer-secret", "first-env-secret", true},
		{"provided env replaces and bearer replaces", map[string]any{"env": map[string]string{"NEW_KEY": "replacement-env-secret"}, "bearer_token": "replacement-bearer-secret"}, "replacement-bearer-secret", "replacement-env-secret", true},
		{"empty bearer deletes", map[string]any{"bearer_token": ""}, "", "replacement-env-secret", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, raw := requestRaw(t, handler, http.MethodPut, "/api/mcp-servers/"+created.ID, tc.body)
			for _, secret := range []string{"first-bearer-secret", "first-env-secret", "last-env-secret", "replacement-env-secret", "replacement-bearer-secret"} {
				if strings.Contains(raw, secret) {
					t.Fatalf("response leaked %q: %s", secret, raw)
				}
			}
			if code != http.StatusOK {
				t.Fatalf("update status/body = %d %s", code, raw)
			}
			var updated mcpServer
			if err := json.Unmarshal([]byte(raw), &updated); err != nil {
				t.Fatal(err)
			}
			if updated.HasBearerToken != tc.wantHas {
				t.Fatalf("has bearer = %v, want %v", updated.HasBearerToken, tc.wantHas)
			}
			bearer, err := keyring.Get(connectorService, mcpServerSecret(created.ID, "bearer"))
			if tc.wantBearer == "" && !errors.Is(err, keyring.ErrNotFound) || tc.wantBearer != "" && (err != nil || bearer != tc.wantBearer) {
				t.Fatalf("bearer after update = %q, %v", bearer, err)
			}
			env, err := keyring.Get(connectorService, mcpServerSecret(created.ID, "env"))
			if err != nil || !strings.Contains(env, tc.wantEnv) {
				t.Fatalf("env after update = %q, %v", env, err)
			}
			if tc.name == "provided env replaces and bearer replaces" && strings.Contains(env, "last-env-secret") {
				t.Fatalf("old environment value remains after replacement: %q", env)
			}
		})
	}
	if err := keyring.Set(connectorService, mcpServerSecret(created.ID, "oauth"), `{"access_token":"oauth-secret"}`); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(connectorService, mcpServerSecret(created.ID, "oauth-client"), `{"client_id":"oauth-client-secret"}`); err != nil {
		t.Fatal(err)
	}
	if code, raw := requestRaw(t, handler, http.MethodPut, "/api/mcp-servers/"+created.ID, map[string]any{"auth": "none"}); code != http.StatusOK || strings.Contains(raw, "oauth-secret") {
		t.Fatalf("auth switch status/body = %d %s", code, raw)
	}
	for _, suffix := range []string{"bearer", "oauth", "oauth-client"} {
		if _, err := keyring.Get(connectorService, mcpServerSecret(created.ID, suffix)); !errors.Is(err, keyring.ErrNotFound) {
			t.Fatalf("%s secret remains after auth switch: %v", suffix, err)
		}
	}
	if err := keyring.Set(connectorService, mcpServerSecret(created.ID, "oauth"), `{"access_token":"oauth-secret"}`); err != nil {
		t.Fatal(err)
	}
	code, raw = requestRaw(t, handler, http.MethodGet, "/api/mcp-servers", nil)
	if code != http.StatusOK || strings.Contains(raw, "oauth-secret") || strings.Contains(raw, "replacement-env-secret") || strings.Contains(raw, "first-env-secret") || strings.Contains(raw, "first-bearer-secret") {
		t.Fatalf("list status/body = %d %s", code, raw)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/mcp-servers/"+created.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete status = %d", code)
	}
	for _, suffix := range []string{"env", "bearer", "oauth", "oauth-client"} {
		if _, err := keyring.Get(connectorService, mcpServerSecret(created.ID, suffix)); !errors.Is(err, keyring.ErrNotFound) {
			t.Fatalf("deleted %s secret remains: %v", suffix, err)
		}
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/mcp-servers/missing", nil, nil); code != http.StatusNotFound {
		t.Fatalf("missing server = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/mcp-servers", map[string]any{"name": "No Token", "kind": "http", "url": "https://example.test/mcp", "auth": "bearer"}, nil); code != http.StatusBadRequest {
		t.Fatalf("bearer without token status = %d", code)
	}
}

func TestMCPServerNamesAndEnabledSpec(t *testing.T) {
	keyring.MockInit()
	database, handler, root := chatAPI(t)
	body := map[string]any{"name": "My Tickets", "kind": "http", "url": "https://example.test/mcp"}
	if code := requestJSON(t, handler, http.MethodPost, "/api/mcp-servers", body, nil); code != http.StatusBadRequest {
		t.Fatalf("reserved name status = %d", code)
	}
	var created mcpServer
	body = map[string]any{"name": "Name With Spaces", "kind": "http", "url": "https://example.test/mcp", "auth": "bearer", "bearer_token": "bearer-secret", "env": map[string]string{"TOKEN": "env-secret"}, "enabled": true}
	if code := requestJSON(t, handler, http.MethodPost, "/api/mcp-servers", body, &created); code != http.StatusCreated {
		t.Fatalf("create first status = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/mcp-servers", map[string]any{"name": "name-with-spaces", "kind": "http", "url": "https://example.test/other"}, nil); code != http.StatusBadRequest {
		t.Fatalf("sanitized duplicate status = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/mcp-servers/"+created.ID, map[string]any{"name": "NAME-WITH-SPACES"}, nil); code != http.StatusOK {
		t.Fatalf("same server sanitized rename status = %d", code)
	}
	server := New(database, "local", false, "dev", root)
	specs, err := server.enabledMCPServers()
	if err != nil || len(specs) != 1 {
		t.Fatalf("enabled specs = %#v, %v", specs, err)
	}
	if specs[0].Name != "name-with-spaces" || specs[0].Env["TOKEN"] != "env-secret" || specs[0].Headers["Authorization"] != "Bearer bearer-secret" {
		t.Fatalf("injection spec = %#v", specs[0])
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/mcp-servers/"+created.ID, map[string]any{"bearer_token": ""}, nil); code != http.StatusOK {
		t.Fatalf("clear bearer status = %d", code)
	}
	specs, err = server.enabledMCPServers()
	if err != nil || len(specs) != 1 || len(specs[0].Headers) != 0 {
		t.Fatalf("server without bearer spec = %#v, %v", specs, err)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/mcp-servers/"+created.ID, map[string]any{"enabled": false}, nil); code != http.StatusOK {
		t.Fatalf("disable status = %d", code)
	}
	specs, err = server.enabledMCPServers()
	if err != nil || len(specs) != 0 {
		t.Fatalf("disabled specs = %#v, %v", specs, err)
	}
	var second mcpServer
	if code := requestJSON(t, handler, http.MethodPost, "/api/mcp-servers", map[string]any{"name": "Second", "kind": "http", "url": "https://example.test/second"}, &second); code != http.StatusCreated {
		t.Fatalf("create second server = %d", code)
	}
	if _, err := database.Exec(`UPDATE mcp_server SET created_at='same'`); err != nil {
		t.Fatal(err)
	}
	var listed []mcpServer
	if code := requestJSON(t, handler, http.MethodGet, "/api/mcp-servers", nil, &listed); code != http.StatusOK || len(listed) != 2 || listed[0].ID != created.ID || listed[1].ID != second.ID {
		t.Fatalf("list order = %d %#v", code, listed)
	}
}
