package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func addChatMCPTestServers(t *testing.T, handler http.Handler) {
	t.Helper()
	keyring.MockInit()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []map[string]any{
		{"name": "stdio-test", "kind": "stdio", "command": exe, "args": []string{"--fixture"}, "env": map[string]string{"TOKEN": "stdio-secret"}},
		{"name": "http-test", "kind": "http", "url": "https://mcp.example.test/mcp", "auth": "bearer", "bearer_token": "http-secret"},
		{"name": "disabled-test", "kind": "stdio", "command": exe, "args": []string{}, "enabled": false},
	} {
		if code := requestJSON(t, handler, http.MethodPost, "/api/mcp-servers", input, nil); code != http.StatusCreated {
			t.Fatalf("create MCP test server %q = %d", input["name"], code)
		}
	}
}

func TestChatDriverMCPConfigShapes(t *testing.T) {
	stdio := mcpServerSpec{Name: "stdio-test", Kind: "stdio", Command: "/bin/test", Args: []string{"--fixture"}, Env: map[string]string{"TOKEN": "stdio-secret"}}
	token := "Bearer http-secret"
	httpServer := mcpServerSpec{Name: "http-test", Kind: "http", URL: "https://mcp.example.test/mcp", OAuthHeader: &token}
	ticket := map[string]any{"command": "/app/my-tickets", "args": []string{"mcp", "-d", "/data/test.db"}}
	external := []mcpServerSpec{stdio, httpServer}

	claude := claudeMCPServers(ticket, external)
	claudeStdio := claude["stdio-test"].(map[string]any)
	if claudeStdio["command"] != "/bin/test" || !reflect.DeepEqual(claudeStdio["args"], stdio.Args) || !reflect.DeepEqual(claudeStdio["env"], stdio.Env) {
		t.Fatalf("Claude stdio config = %#v", claudeStdio)
	}
	claudeHTTP := claude["http-test"].(map[string]any)
	if claudeHTTP["type"] != "http" || claudeHTTP["url"] != httpServer.URL || claudeHTTP["headers"].(map[string]string)["Authorization"] != token {
		t.Fatalf("Claude HTTP config = %#v", claudeHTTP)
	}

	codex := codexMCPServers(ticket, external)
	codexStdio := codex["stdio-test"].(map[string]any)
	if codexStdio["command"] != "/bin/test" || !reflect.DeepEqual(codexStdio["args"], stdio.Args) || !reflect.DeepEqual(codexStdio["env"], stdio.Env) {
		t.Fatalf("Codex stdio config = %#v", codexStdio)
	}
	codexHTTP := codex["http-test"].(map[string]any)
	if codexHTTP["url"] != httpServer.URL || codexHTTP["http_headers"].(map[string]string)["Authorization"] != token {
		t.Fatalf("Codex HTTP config = %#v", codexHTTP)
	}

	copilot := copilotMCPServers(ticket, external)
	if len(copilot) != 3 || copilot[0].(map[string]any)["name"] != "my-tickets" {
		t.Fatalf("Copilot servers = %#v", copilot)
	}
	copilotStdio := copilot[1].(map[string]any)
	if copilotStdio["name"] != "stdio-test" || copilotStdio["command"] != "/bin/test" || !reflect.DeepEqual(copilotStdio["args"], stdio.Args) || !reflect.DeepEqual(copilotStdio["env"], []any{map[string]string{"name": "TOKEN", "value": "stdio-secret"}}) {
		t.Fatalf("Copilot stdio config = %#v", copilotStdio)
	}
	copilotHTTP := copilot[2].(map[string]any)
	if copilotHTTP["type"] != "http" || copilotHTTP["name"] != "http-test" || copilotHTTP["url"] != httpServer.URL || !reflect.DeepEqual(copilotHTTP["headers"], []any{map[string]string{"name": "Authorization", "value": token}}) {
		t.Fatalf("Copilot HTTP config = %#v", copilotHTTP)
	}

	opencode := opencodeMCPServers("/app/my-tickets", ticket["args"].([]string), external)
	opencodeStdio := opencode["stdio-test"].(map[string]any)
	if opencodeStdio["type"] != "local" || !reflect.DeepEqual(opencodeStdio["command"], []string{"/bin/test", "--fixture"}) || !reflect.DeepEqual(opencodeStdio["environment"], stdio.Env) {
		t.Fatalf("opencode stdio config = %#v", opencodeStdio)
	}
	opencodeHTTP := opencode["http-test"].(map[string]any)
	if opencodeHTTP["type"] != "remote" || opencodeHTTP["url"] != httpServer.URL || opencodeHTTP["headers"].(map[string]string)["Authorization"] != token {
		t.Fatalf("opencode HTTP config = %#v", opencodeHTTP)
	}
}

func TestClaudeMCPConfigIsPrivateAndRemoved(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	process := newPipeChatProcess()
	var configPath string
	server := New(database, "local", false, "test", root, Options{
		DBPath:     filepath.Join(root, "test.db"),
		findClaude: func() (string, error) { return "claude-test", nil },
		processFactory: func(_ string, args, _ []string, _ string) (chatProcess, error) {
			index := slicesIndex(args, "--mcp-config")
			if index < 0 || index+1 >= len(args) {
				return nil, os.ErrInvalid
			}
			configPath = args[index+1]
			return process, nil
		},
	})
	addChatMCPTestServers(t, server)
	if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]string{"text": "hello"}, nil); code != http.StatusAccepted {
		t.Fatalf("send = %d", code)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Servers) != 3 || config.Servers["my-tickets"] == nil || config.Servers["disabled-test"] != nil {
		t.Fatalf("Claude configured servers = %#v", config.Servers)
	}
	stdio := config.Servers["stdio-test"]
	if stdio["command"] == nil || stdio["env"].(map[string]any)["TOKEN"] != "stdio-secret" {
		t.Fatalf("Claude stdio config = %#v", stdio)
	}
	httpServer := config.Servers["http-test"]
	if httpServer["type"] != "http" || httpServer["headers"].(map[string]any)["Authorization"] != "Bearer http-secret" {
		t.Fatalf("Claude HTTP config = %#v", httpServer)
	}
	if !strings.Contains(configPath, filepath.Join("chats", ".tmp")) {
		t.Fatalf("Claude config path = %q", configPath)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("Claude config permissions = %o", info.Mode().Perm())
		}
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("Claude config remains after close: %v", err)
	}
}

func slicesIndex(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}
