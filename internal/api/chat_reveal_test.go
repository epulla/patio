package api

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"my-tickets/internal/db"
)

func TestRevealChatFile(t *testing.T) {
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
	workdir := filepath.Join(root, "work", "chat")
	if err := os.MkdirAll(workdir, 0o700); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(workdir, "inside.txt")
	if err := os.WriteFile(inside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cleanInside, err := filepath.EvalSymlinks(inside)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "work", "outside.txt")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(root, "work", "chat-sibling")
	if err := os.Mkdir(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	siblingFile := filepath.Join(sibling, "inside.txt")
	if err := os.WriteFile(siblingFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	escapingLink := filepath.Join(workdir, "escape.txt")
	if err := os.Symlink(outside, escapingLink); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	connectorID := createTestConnector(t, database)
	chatID := uid()
	if _, err := database.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at) VALUES(?,?,?, ?,?,?,?)`, chatID, connectorID, "Reveal test", "", workdir, now(), now()); err != nil {
		t.Fatal(err)
	}
	server := New(database, "local", false, "dev", root)
	defer func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	}()
	var opened []string
	server.revealFile = func(path string) error {
		opened = append(opened, path)
		return nil
	}
	fileURL := (&url.URL{Scheme: "file", Path: inside}).String()
	tests := []struct {
		name, path, wantPath, wantError string
		wantStatus                      int
	}{
		{name: "relative", path: "nested/../inside.txt", wantPath: cleanInside, wantStatus: http.StatusNoContent},
		{name: "absolute", path: inside, wantPath: cleanInside, wantStatus: http.StatusNoContent},
		{name: "file URL", path: fileURL, wantPath: cleanInside, wantStatus: http.StatusNoContent},
		{name: "traversal", path: filepath.Join("..", "outside.txt"), wantError: errPathOutsideWorkdir.Error(), wantStatus: http.StatusBadRequest},
		{name: "sibling prefix", path: siblingFile, wantError: errPathOutsideWorkdir.Error(), wantStatus: http.StatusBadRequest},
		{name: "missing", path: filepath.Join(workdir, "missing.txt"), wantError: "file not found", wantStatus: http.StatusNotFound},
		{name: "symlink escape", path: escapingLink, wantError: errPathOutsideWorkdir.Error(), wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opened = nil
			code, body := requestRaw(t, server, http.MethodPost, "/api/chats/"+chatID+"/reveal", map[string]string{"path": test.path})
			if code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", code, test.wantStatus, body)
			}
			if test.wantError != "" && !strings.Contains(body, test.wantError) {
				t.Fatalf("error body = %q, want %q", body, test.wantError)
			}
			if test.wantPath != "" {
				if len(opened) != 1 || opened[0] != test.wantPath {
					t.Fatalf("opener paths = %#v, want [%q]", opened, test.wantPath)
				}
			} else if len(opened) != 0 {
				t.Fatalf("opener called for rejected path: %#v", opened)
			}
		})
	}
	if code, body := requestRaw(t, server, http.MethodPost, "/api/chats/missing/reveal", map[string]string{"path": inside}); code != http.StatusNotFound || !strings.Contains(body, "chat not found") {
		t.Fatalf("unknown chat status/body = %d %s", code, body)
	}
}
