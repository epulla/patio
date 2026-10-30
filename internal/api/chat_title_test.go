package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	connectorcli "patio/internal/connector"
	"patio/internal/db"
)

func TestCleanChatTitle(t *testing.T) {
	tests := []struct{ input, want string }{
		{`### "A useful chat title!"`, "A useful chat title"},
		{"one two three four five six seven", "one two three four five six"},
		{"first line\nsecond line", ""},
		{"   `  spaced   title  `   ", ""},
		{strings.Repeat("a", 59) + "é extra words", ""},
		{"one two", ""},
		{strings.Repeat("long ", 30), "long long long long long long"},
		{"Café résumé ideas para niños", "Café résumé ideas para niños"},
		{strings.Repeat("é", 25) + " " + strings.Repeat("ü", 25) + " ab cd efg", strings.Repeat("é", 25) + " " + strings.Repeat("ü", 25) + " ab cd"},
	}
	for _, tt := range tests {
		if got := cleanChatTitle(tt.input); got != tt.want {
			t.Errorf("cleanChatTitle(%q)=%q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestChatTitleGenerationFirstReplyAndSettings(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "title-hook.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	manager := &chatManager{db: database, chats: map[string]*chatRuntime{}, queueLocks: map[string]*sync.Mutex{}, emitEvent: func(string, any) {}}
	old := titleTextGenerator
	t.Cleanup(func() { titleTextGenerator = old })
	calls := 0
	titleTextGenerator = func(context.Context, *chatManager, string, string, string) (string, error) {
		calls++
		return "A generated title", nil
	}
	for _, tc := range []struct {
		id        string
		locked    bool
		disabled  bool
		wantCalls int
	}{
		{"normal", false, false, 1}, {"locked", true, false, 0}, {"disabled", false, true, 0},
	} {
		_, err := database.Exec(`INSERT INTO chat(id,title,title_locked,created_at,updated_at) VALUES(?,?,?,?,?)`, tc.id, "Initial", tc.locked, now(), now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at) VALUES(?,?, 'user','question',?)`, uid(), tc.id, now()); err != nil {
			t.Fatal(err)
		}
		if tc.disabled {
			if _, err := database.Exec(`INSERT INTO app_setting(key,value) VALUES('ai_titles','false') ON CONFLICT(key) DO UPDATE SET value='false'`); err != nil {
				t.Fatal(err)
			}
		} else {
			_, _ = database.Exec(`DELETE FROM app_setting WHERE key='ai_titles'`)
		}
		before := calls
		if err := manager.finishTurn(tc.id, "reply", nil); err != nil {
			t.Fatal(err)
		}
		manager.background.Wait()
		if err := manager.finishTurn(tc.id, "second reply", nil); err != nil {
			t.Fatal(err)
		}
		manager.background.Wait()
		var title string
		if err := database.QueryRow(`SELECT title FROM chat WHERE id=?`, tc.id).Scan(&title); err != nil {
			t.Fatal(err)
		}
		wantTitle := "Initial"
		if tc.wantCalls > 0 {
			wantTitle = "A generated title"
		}
		if title != wantTitle || calls-before != tc.wantCalls {
			t.Fatalf("%s: title=%q calls=%d want %q/%d", tc.id, title, calls-before, wantTitle, tc.wantCalls)
		}
	}
}

func TestOpencodeTitleModelFallback(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "title-opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	manager := &chatManager{db: database, chats: map[string]*chatRuntime{}, queueLocks: map[string]*sync.Mutex{}, emitEvent: func(string, any) {}}
	oldGen, oldList := titleTextGenerator, opencodeModelLister
	t.Cleanup(func() { titleTextGenerator, opencodeModelLister = oldGen, oldList })
	var gotModel string
	titleTextGenerator = func(_ context.Context, _ *chatManager, _, model, _ string) (string, error) {
		gotModel = model
		return "A generated title", nil
	}
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES('oc','opencode','OpenCode',?)`, now()); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		listed []string
		want   string
	}{
		{[]string{"openai/gpt-5", opencodeTitleModel}, opencodeTitleModel},
		{[]string{"openai/gpt-5"}, "anthropic/chat-model"},
	} {
		opencodeModelLister = func(context.Context, string) ([]string, error) { return tc.listed, nil }
		id := fmt.Sprintf("chat-%d", i)
		if _, err := database.Exec(`INSERT INTO chat(id,title,connector_id,model,created_at,updated_at) VALUES(?,'Initial','oc','anthropic/chat-model',?,?)`, id, now(), now()); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at) VALUES(?,?,'user','question',?)`, uid(), id, now()); err != nil {
			t.Fatal(err)
		}
		manager.maybeGenerateChatTitle(id, "reply")
		manager.background.Wait()
		if gotModel != tc.want {
			t.Fatalf("listed %v: model=%q want %q", tc.listed, gotModel, tc.want)
		}
	}
}

func TestRenameChatLocksTitle(t *testing.T) {
	database, handler := newAPI(t)
	id := uid()
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES(?,?,?,?)`, id, "Before", now(), now()); err != nil {
		t.Fatal(err)
	}
	var locked bool
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+id, map[string]any{"title": "First message", "title_auto": true}, nil); code != http.StatusOK {
		t.Fatalf("auto title status=%d", code)
	}
	if err := database.QueryRow(`SELECT title_locked FROM chat WHERE id=?`, id).Scan(&locked); err != nil || locked {
		t.Fatalf("auto title locked=%v err=%v", locked, err)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+id, map[string]string{"title": "Renamed"}, nil); code != http.StatusOK {
		t.Fatalf("rename status=%d", code)
	}
	if err := database.QueryRow(`SELECT title_locked FROM chat WHERE id=?`, id).Scan(&locked); err != nil || !locked {
		t.Fatalf("title_locked=%v err=%v", locked, err)
	}
}

func TestGenerateTextIsolatesCLIs(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "title.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	m := &chatManager{db: database}
	oldFinder, oldRunner := titleCommandFinder, titleCommandRunner
	t.Cleanup(func() { titleCommandFinder, titleCommandRunner = oldFinder, oldRunner })
	titleCommandFinder = func(kind string) (string, error) { return kind + "-test", nil }
	for _, tc := range []struct {
		kind, stdout string
		want         []string
	}{
		{"claude", "A useful title\n", []string{"--tools", "", "--strict-mcp-config", "--disable-slash-commands", "--no-session-persistence", `{"disableAllHooks":true}`, "text"}},
		{"codex", "A useful title\n", []string{"--ephemeral", "read-only", "shell_tool", "unified_exec", "apps", "plugins", "hooks", "mcp_servers={}", `web_search="disabled"`}},
		{"opencode", `{"type":"step_start"}` + "\n" + `{"type":"text","part":{"type":"text","text":"A useful title"}}` + "\n", []string{"--standalone", "--format", "json"}},
		{"copilot", "A useful title\n", []string{"-s", "--available-tools=", "--disable-builtin-mcps", "--no-custom-instructions", "--no-ask-user"}},
	} {
		if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,?,?,?)`, tc.kind, tc.kind, tc.kind, now()); err != nil {
			t.Fatal(err)
		}
		titleCommandRunner = func(_ context.Context, kind, path string, args, env []string) ([]byte, error) {
			if path != tc.kind+"-test" || !containsArgs(args, append(tc.want, "make title")...) {
				t.Fatalf("%s args %q", tc.kind, args)
			}
			return []byte(tc.stdout), nil
		}
		if got, err := m.generateText(context.Background(), tc.kind, "", "make title"); err != nil || got != "A useful title" {
			t.Fatalf("%s: got %q err %v", tc.kind, got, err)
		}
	}
}

func TestTitleCommandRunnerIgnoresStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	out, err := titleCommandRunner(context.Background(), "codex", "/bin/sh", []string{"-c", "echo 'OpenAI Codex v1 banner' >&2; echo 'A useful title'"}, nil)
	if err != nil || strings.TrimSpace(string(out)) != "A useful title" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	_, err = titleCommandRunner(context.Background(), "codex", "/bin/sh", []string{"-c", "echo 'auth failed' >&2; exit 1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("err=%v", err)
	}
}

func TestTitleCommandRunnerIsolatesOpencode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	env := append(os.Environ(), "OPENCODE_CONFIG=/user/opencode.json", "OPENCODE_CONFIG_DIR=/user/opencode", "PWD=/user/project")
	script := `[ "$(pwd -P)" = "$(cd "$PWD" && pwd -P)" ] && echo "$PWD|$XDG_CONFIG_HOME|$OPENCODE_CONFIG|$OPENCODE_CONFIG_DIR|$OPENCODE_DISABLE_PROJECT_CONFIG|$OPENCODE_CONFIG_CONTENT"`
	out, err := titleCommandRunner(context.Background(), "opencode", "/bin/sh", []string{"-c", script}, env)
	if err != nil {
		t.Fatal(err)
	}
	dir := strings.SplitN(string(out), "|", 2)[0]
	if !strings.HasPrefix(filepath.Base(dir), "patio-title-") {
		t.Fatalf("cwd %q is not a fresh title dir", out)
	}
	if want := dir + "|" + filepath.Join(dir, "config") + "|||1|" + opencodeTitleConfig; strings.TrimSpace(string(out)) != want {
		t.Fatalf("env %q, want %q", out, want)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("title dir not removed: %v", err)
	}
}

// Starts the same private server `opencode run --standalone` spawns, with the title env and cwd,
// and checks the effective config: deny-all permissions, no user plugins or MCP servers.
func TestOpencodeTitleEffectiveConfigLive(t *testing.T) {
	if os.Getenv("PATIO_OPENCODE_V2_LIVE") != "1" {
		t.Skip("set PATIO_OPENCODE_V2_LIVE=1 for OpenCode v2 checks")
	}
	path, err := connectorcli.Find("opencode")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, path, "serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port))
	cmd.Dir = dir
	cmd.Env = append(opencodeTitleEnv(os.Environ(), dir), "OPENCODE_SERVER_PASSWORD=patio-test")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	get := func(route string, v any) {
		t.Helper()
		var lastErr error
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+route, nil)
			req.SetBasicAuth("opencode", "patio-test")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				lastErr = err
				continue
			}
			if resp.StatusCode == http.StatusServiceUnavailable {
				resp.Body.Close()
				lastErr = fmt.Errorf("status %d", resp.StatusCode)
				continue
			}
			err = json.NewDecoder(resp.Body).Decode(v)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK || err != nil {
				t.Fatalf("%s: status %d err %v", route, resp.StatusCode, err)
			}
			return
		}
		t.Fatalf("%s: %v", route, lastErr)
	}
	var sources []struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}
	get("/api/config", &sources)
	for _, s := range sources {
		if s.Path != "" && !strings.HasPrefix(s.Path, dir) {
			t.Errorf("config source outside title dir: %s", s.Path)
		}
	}
	var plugins struct {
		Data []struct {
			ID     string `json:"id"`
			Source struct {
				Type string `json:"type"`
			} `json:"source"`
		} `json:"data"`
	}
	get("/api/plugin", &plugins)
	for _, p := range plugins.Data {
		if p.Source.Type != "builtin" {
			t.Errorf("non-builtin plugin loaded: %s", p.ID)
		}
	}
	var mcp struct {
		Data []json.RawMessage `json:"data"`
	}
	get("/api/mcp", &mcp)
	if len(mcp.Data) != 0 {
		t.Errorf("MCP servers loaded: %d", len(mcp.Data))
	}
	var agents struct {
		Data []struct {
			ID          string `json:"id"`
			Permissions []struct {
				Action, Resource, Effect string
			} `json:"permissions"`
		} `json:"data"`
	}
	// Agents can report built-in defaults for a moment after startup, before config is applied.
	var open []string
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(500 * time.Millisecond) {
		get("/api/agent", &agents)
		open = nil
		for _, a := range agents.Data {
			denied := false
			for _, p := range a.Permissions {
				if p.Action == "*" && p.Resource == "*" {
					denied = p.Effect == "deny"
				} else if p.Effect != "deny" {
					denied = false
				}
			}
			if !denied {
				open = append(open, a.ID)
			}
		}
		if (len(agents.Data) > 0 && len(open) == 0) || time.Now().After(deadline) {
			break
		}
	}
	if len(agents.Data) == 0 || len(open) > 0 {
		t.Fatalf("agents without deny-all permissions: %v (of %d)", open, len(agents.Data))
	}
}

func TestOpencodeRunText(t *testing.T) {
	if _, err := opencodeRunText([]byte(`{"type":"error","error":{"message":"API key expired"}}`)); err == nil || !strings.Contains(err.Error(), "API key expired") {
		t.Fatalf("err=%v", err)
	}
	if _, err := opencodeRunText([]byte("plain diagnostics\n")); err == nil {
		t.Fatal("non-JSON output must not become a title")
	}
}

func containsArgs(args []string, expected ...string) bool {
	for _, e := range expected {
		found := false
		for _, a := range args {
			if a == e {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
