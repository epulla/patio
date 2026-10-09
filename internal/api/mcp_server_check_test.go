package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"
	"my-tickets/internal/db"
)

func TestMain(m *testing.M) {
	if os.Getenv("MY_TICKETS_MCP_TEST_SERVER") == "1" {
		server := mcp.NewServer(&mcp.Implementation{Name: "check-test-server", Version: "1"}, nil)
		server.AddTool(&mcp.Tool{Name: "zebra", Description: "last", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
		server.AddTool(&mcp.Tool{Name: "apple", Description: "first", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
		if os.Getenv("HEALTH_CHECK_TEST_ENV") != "from-keychain" {
			_, _ = os.Stderr.WriteString("missing keychain environment")
			os.Exit(1)
		}
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestMCPServerCheckStdio(t *testing.T) {
	keyring.MockInit()
	database, server := newMCPCheckServer(t, Options{})
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal([]string{"-test.run=^$"})
	if err != nil {
		t.Fatal(err)
	}
	envJSON, keysJSON, err := encodeMCPEnv(map[string]string{"HEALTH_CHECK_TEST_ENV": "from-keychain", "MY_TICKETS_MCP_TEST_SERVER": "1"})
	if err != nil {
		t.Fatal(err)
	}
	id := insertMCPCheckServer(t, database, "stdio", executable, string(args), keysJSON, "", "none", false)
	if err := keyring.Set(connectorService, mcpServerSecret(id, "env"), envJSON); err != nil {
		t.Fatal(err)
	}
	var result mcpServerCheck
	if code := requestJSON(t, server, http.MethodGet, "/api/mcp-servers/"+id+"/check", nil, &result); code != http.StatusOK {
		t.Fatalf("check status = %d, result = %#v", code, result)
	}
	if !result.OK || result.Error != "" || len(result.Tools) != 2 || result.Tools[0].Name != "apple" || result.Tools[1].Name != "zebra" {
		t.Fatalf("stdio check = %#v", result)
	}
}

func TestMCPServerCheckHTTPAuthAndDisabled(t *testing.T) {
	keyring.MockInit()
	remote := mcp.NewServer(&mcp.Implementation{Name: "http-check-test", Version: "1"}, nil)
	remote.AddTool(&mcp.Tool{Name: "zebra", Description: "last", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	remote.AddTool(&mcp.Tool{Name: "apple", Description: "first", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, nil)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer x" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		streamable.ServeHTTP(w, r)
	}))
	defer httpServer.Close()
	database, server := newMCPCheckServer(t, Options{})
	withToken := insertMCPCheckServer(t, database, "http", "", "[]", "[]", httpServer.URL, "bearer", false)
	if err := keyring.Set(connectorService, mcpServerSecret(withToken, "bearer"), "x"); err != nil {
		t.Fatal(err)
	}
	withoutToken := insertMCPCheckServer(t, database, "http", "", "[]", "[]", httpServer.URL, "bearer", false)
	oauthToken := insertMCPCheckServer(t, database, "http", "", "[]", "[]", httpServer.URL, "oauth", false)
	if err := keyring.Set(connectorService, mcpServerSecret(oauthToken, "oauth"), `{"access_token":"x"}`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id string
		ok bool
	}{
		{withToken, true},
		{withoutToken, false},
		{oauthToken, true},
	} {
		var result mcpServerCheck
		if code := requestJSON(t, server, http.MethodGet, "/api/mcp-servers/"+tc.id+"/check", nil, &result); code != http.StatusOK {
			t.Fatalf("check %q status = %d, result = %#v", tc.id, code, result)
		}
		if result.OK != tc.ok {
			t.Fatalf("check %q = %#v, want ok=%v", tc.id, result, tc.ok)
		}
		if tc.ok && (len(result.Tools) != 2 || result.Tools[0].Name != "apple" || result.Tools[1].Name != "zebra") {
			t.Fatalf("HTTP tools = %#v", result.Tools)
		}
	}
}

func TestMCPServerCheckTimeoutAndOAuthPrompt(t *testing.T) {
	keyring.MockInit()
	database, server := newMCPCheckServer(t, Options{
		mcpCheckTimeout: 30 * time.Millisecond,
		mcpConnect: func(ctx context.Context, _ mcpServerSpec) (*mcp.ClientSession, func(), error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		},
	})
	id := insertMCPCheckServer(t, database, "http", "", "[]", "[]", "http://localhost/mcp", "none", true)
	var result mcpServerCheck
	start := time.Now()
	if code := requestJSON(t, server, http.MethodGet, "/api/mcp-servers/"+id+"/check", nil, &result); code != http.StatusOK {
		t.Fatalf("timeout check status = %d, result = %#v", code, result)
	}
	if result.OK || result.Error == "" || time.Since(start) > time.Second {
		t.Fatalf("timeout check = %#v after %s", result, time.Since(start))
	}
	oauthID := insertMCPCheckServer(t, database, "http", "", "[]", "[]", "http://localhost/mcp", "oauth", false)
	if code := requestJSON(t, server, http.MethodGet, "/api/mcp-servers/"+oauthID+"/check", nil, &result); code != http.StatusOK || result.OK || result.Error != "Sign in first" {
		t.Fatalf("OAuth check = %d, %#v", code, result)
	}
}

func TestMCPServerCheckMissingID(t *testing.T) {
	keyring.MockInit()
	_, server := newMCPCheckServer(t, Options{})
	if code := requestJSON(t, server, http.MethodGet, "/api/mcp-servers/missing/check", nil, nil); code != http.StatusNotFound {
		t.Fatalf("missing server status = %d", code)
	}
}

func newMCPCheckServer(t *testing.T, options Options) (*sql.DB, *Server) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "mcp-check.db"))
	if err != nil {
		t.Fatal(err)
	}
	server := New(database, "local", false, "test", t.TempDir(), options)
	t.Cleanup(func() {
		_ = server.Close()
		_ = database.Close()
	})
	return database, server
}

func insertMCPCheckServer(t *testing.T, database *sql.DB, kind, command, args, envKeys, endpoint, auth string, enabled bool) string {
	t.Helper()
	id := uid()
	if _, err := database.Exec(`INSERT INTO mcp_server(id,name,kind,command,args_json,env_keys_json,url,auth,enabled,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, "Check "+id, kind, command, args, envKeys, endpoint, auth, enabled, now()); err != nil {
		t.Fatal(err)
	}
	return id
}
