package api

import (
	"context"
	"net/http"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"patio/internal/db"
)

func TestCleanChatTitle(t *testing.T) {
	tests := []struct{ input, want string }{
		{`### "A useful chat title!"`, "A useful chat title"},
		{"one two three four five six seven", "one two three four five six"},
		{"first line\nsecond line", "first line"},
		{"   `  spaced   title  `   ", "spaced title"},
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

func TestGenerateTextClaudeCommand(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "title.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,created_at) VALUES('c','claude','Claude',?)`, now()); err != nil {
		t.Fatal(err)
	}
	m := &chatManager{db: database}
	oldFinder, oldRunner := titleCommandFinder, titleCommandRunner
	t.Cleanup(func() { titleCommandFinder, titleCommandRunner = oldFinder, oldRunner })
	titleCommandFinder = func(kind string) (string, error) {
		if kind != "claude" {
			t.Fatalf("kind=%s", kind)
		}
		return "claude-test", nil
	}
	titleCommandRunner = func(_ context.Context, kind, path string, args, env []string) ([]byte, error) {
		if kind != "claude" || path != "claude-test" || !reflect.DeepEqual(args, []string{"-p", "make title", "--model", "haiku"}) {
			t.Fatalf("command %q %q %v", kind, path, args)
		}
		return []byte("A title"), nil
	}
	got, err := m.generateText(context.Background(), "c", "haiku", "make title")
	if err != nil || got != "A title" {
		t.Fatalf("got %q err %v", got, err)
	}
}
