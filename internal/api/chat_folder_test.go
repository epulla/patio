package api

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBrowseFolders(t *testing.T) {
	_, handler, root := chatAPI(t)
	allowed := filepath.Join(root, "allowed")
	for _, dir := range []string{"b", "a/inner", ".git", "../outside"} {
		if err := os.MkdirAll(filepath.Join(allowed, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(allowed, "file.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	allowed, err := filepath.EvalSymlinks(allowed)
	if err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/settings/folders", map[string]string{"path": allowed}, nil); code != http.StatusCreated {
		t.Fatalf("add allowed folder = %d", code)
	}
	browse := func(path string, out any) int {
		return requestJSON(t, handler, http.MethodGet, "/api/settings/folders/browse?path="+url.QueryEscape(path), nil, out)
	}
	var got struct {
		Path    string          `json:"path"`
		Folders []browsedFolder `json:"folders"`
	}
	if code := browse(allowed, &got); code != http.StatusOK {
		t.Fatalf("browse root = %d", code)
	}
	want := []browsedFolder{{"a", filepath.Join(allowed, "a")}, {"b", filepath.Join(allowed, "b")}}
	if got.Path != allowed || !reflect.DeepEqual(got.Folders, want) {
		t.Fatalf("browse root = %#v", got)
	}
	if code := browse(filepath.Join(allowed, "a"), &got); code != http.StatusOK || len(got.Folders) != 1 || got.Folders[0].Name != "inner" {
		t.Fatalf("browse subfolder = %d, %#v", code, got)
	}
	for _, path := range []string{filepath.Join(root, "outside"), filepath.Join(allowed, "..", "outside"), filepath.Join(allowed, "missing"), "relative", ""} {
		if code := browse(path, nil); code != http.StatusBadRequest {
			t.Errorf("browse %q = %d", path, code)
		}
	}
}

func TestUpdateChatFolder(t *testing.T) {
	database, handler, root := chatAPI(t)
	server := handler.(*Server)
	connectorID := createTestConnector(t, database)
	allowed := filepath.Join(root, "allowed")
	sub := filepath.Join(allowed, "project")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{sub, outside} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	allowed, err := filepath.EvalSymlinks(allowed)
	if err != nil {
		t.Fatal(err)
	}
	sub = filepath.Join(allowed, "project")
	if code := requestJSON(t, handler, http.MethodPost, "/api/settings/folders", map[string]string{"path": allowed}, nil); code != http.StatusCreated {
		t.Fatalf("add allowed folder = %d", code)
	}
	var c chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]any{"connector_id": connectorID, "folder": nil}, &c); code != http.StatusCreated {
		t.Fatalf("create chat = %d", code)
	}
	scratch := c.Workdir
	if response := postAttachmentFiles(t, server, c.ID, []attachmentUpload{{"notes.txt", []byte("hello")}}); response.Code != http.StatusCreated {
		t.Fatalf("upload = %d, %s", response.Code, response.Body.String())
	}

	if response := requestRecorderJSON(server, http.MethodPut, "/api/chats/"+c.ID, map[string]any{"folder": outside}); response.Code != http.StatusBadRequest {
		t.Fatalf("outside folder = %d, %s", response.Code, response.Body.String())
	}

	var updated chat
	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+c.ID, map[string]any{"folder": sub}, &updated); code != http.StatusOK {
		t.Fatalf("move to subfolder = %d", code)
	}
	if updated.Folder == nil || *updated.Folder != sub || updated.Workdir != sub {
		t.Fatalf("updated chat = %#v", updated)
	}
	if data, err := os.ReadFile(filepath.Join(sub, "attachments", "notes.txt")); err != nil || string(data) != "hello" {
		t.Fatalf("moved attachment = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(scratch, "attachments", "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("old attachment remains: %v", err)
	}
	var got struct {
		Chat chat `json:"chat"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/chats/"+c.ID, nil, &got); code != http.StatusOK || got.Chat.Folder == nil || *got.Chat.Folder != sub {
		t.Fatalf("GET after move = %d, %#v", code, got)
	}

	// The scratch folder already has a file with the same name, so the move back fails and nothing changes.
	if err := os.WriteFile(filepath.Join(scratch, "attachments", "notes.txt"), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if response := requestRecorderJSON(server, http.MethodPut, "/api/chats/"+c.ID, map[string]any{"folder": nil}); response.Code != http.StatusConflict {
		t.Fatalf("clashing move = %d, %s", response.Code, response.Body.String())
	}
	if data, err := os.ReadFile(filepath.Join(sub, "attachments", "notes.txt")); err != nil || string(data) != "hello" {
		t.Fatalf("attachment after failed move = %q, %v", data, err)
	}
	if err := os.Remove(filepath.Join(scratch, "attachments", "notes.txt")); err != nil {
		t.Fatal(err)
	}

	if code := requestJSON(t, handler, http.MethodPut, "/api/chats/"+c.ID, map[string]any{"folder": nil}, &updated); code != http.StatusOK {
		t.Fatalf("back to scratch = %d", code)
	}
	if updated.Folder != nil || updated.Workdir != scratch {
		t.Fatalf("scratch chat = %#v", updated)
	}
	if _, err := os.Stat(filepath.Join(scratch, "attachments", "notes.txt")); err != nil {
		t.Fatalf("attachment not back in scratch: %v", err)
	}

	if _, err := InsertChatMessage(database, c.ID, "user", "hi"); err != nil {
		t.Fatal(err)
	}
	if response := requestRecorderJSON(server, http.MethodPut, "/api/chats/"+c.ID, map[string]any{"folder": sub}); response.Code != http.StatusConflict {
		t.Fatalf("after first message = %d, %s", response.Code, response.Body.String())
	}
	if response := requestRecorderJSON(server, http.MethodPut, "/api/chats/"+c.ID, map[string]any{"folder": nil, "title": "Same folder"}); response.Code != http.StatusOK {
		t.Fatalf("unchanged folder after first message = %d, %s", response.Code, response.Body.String())
	}
}
