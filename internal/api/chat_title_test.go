package api

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
		{"opencode", `{"type":"step_start"}` + "\n" + `{"type":"text","part":{"type":"text","text":"A useful title"}}` + "\n", []string{"--format", "json"}},
		{"copilot", "A useful title\n", []string{"-s", "--available-tools=", "--disable-builtin-mcps", "--no-custom-instructions", "--no-ask-user"}},
	} {
		if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES(?,?,?,?)`, tc.kind, tc.kind, tc.kind, now()); err != nil {
			t.Fatal(err)
		}
		titleCommandRunner = func(_ context.Context, kind, path string, args, env []string) ([]byte, error) {
			if path != tc.kind+"-test" || !containsArgs(args, append(tc.want, "make title")...) {
				t.Fatalf("%s args %q", tc.kind, args)
			}
			if kind == "opencode" && !containsArgs(env, "OPENCODE_CONFIG_CONTENT="+opencodeTitleConfig) {
				t.Fatal("opencode tools not disabled")
			}
			return []byte(tc.stdout), nil
		}
		if got, err := m.generateText(context.Background(), tc.kind, "", "make title"); err != nil || got != "A useful title" {
			t.Fatalf("%s: got %q err %v", tc.kind, got, err)
		}
	}
}

func TestTitleCommandRunnerIgnoresStderr(t *testing.T) {
	out, err := titleCommandRunner(context.Background(), "codex", "/bin/sh", []string{"-c", "echo 'OpenAI Codex v1 banner' >&2; echo 'A useful title'"}, nil)
	if err != nil || strings.TrimSpace(string(out)) != "A useful title" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	_, err = titleCommandRunner(context.Background(), "codex", "/bin/sh", []string{"-c", "echo 'auth failed' >&2; exit 1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("err=%v", err)
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
