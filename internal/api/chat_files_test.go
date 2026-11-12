package api

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func chatFileRequest(t *testing.T, s *Server, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestChatFilesList(t *testing.T) {
	s, c := attachmentTestChat(t)
	root := c.Workdir
	if err := os.MkdirAll(filepath.Join(root, "zdir"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"z.txt", "A.txt", ".hidden"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	w := chatFileRequest(t, s, "GET", "/api/chats/"+c.ID+"/files", "")
	if w.Code != 200 {
		t.Fatalf("list status %d: %s", w.Code, w.Body.String())
	}
	var files []chatFile
	if err := json.Unmarshal(w.Body.Bytes(), &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files[0].Name != "zdir" || files[1].Name != "A.txt" || files[2].Name != "z.txt" {
		t.Fatalf("list = %#v", files)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	w = chatFileRequest(t, s, "GET", "/api/chats/"+c.ID+"/files", "")
	if w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatalf("missing root = %d %q", w.Code, w.Body.String())
	}
}

func TestChatFilesTraversalRejected(t *testing.T) {
	s, c := attachmentTestChat(t)
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "x")
	if err := os.WriteFile(outsideFile, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(outside, filepath.Join(c.Workdir, "outside-dir"))
	_ = os.Symlink(outsideFile, filepath.Join(c.Workdir, "outside-file"))
	for _, path := range []string{"../x", "/etc/passwd", "a/../../x", "outside-dir/x", "outside-dir", "outside-file"} {
		for _, tc := range []struct{ method, route string }{{"GET", ""}, {"GET", "/raw"}, {"DELETE", ""}} {
			target := "/api/chats/" + c.ID + "/files" + tc.route + "?path=" + path
			w := chatFileRequest(t, s, tc.method, target, "")
			// Deleting an escaping symlink removes only the link.
			if (path == "outside-dir" || path == "outside-file") && tc.method == "DELETE" {
				if _, err := os.Lstat(filepath.Join(c.Workdir, path)); w.Code != 204 || !os.IsNotExist(err) {
					t.Errorf("delete link %q status %d, link err %v", path, w.Code, err)
				}
				continue
			}
			if w.Code != 400 {
				t.Errorf("%s %s path %q status %d", tc.method, tc.route, path, w.Code)
			}
		}
	}
	if _, err := os.Stat(outsideFile); err != nil {
		t.Fatal("outside file removed")
	}
}

func TestChatFilesRawSecurityHeaders(t *testing.T) {
	s, c := attachmentTestChat(t)
	for name, data := range map[string]string{"page.html": "<script>x</script>", "doc.pdf": "%PDF-1.4"} {
		if err := os.WriteFile(filepath.Join(c.Workdir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		csp  bool
	}{{"page.html", true}, {"doc.pdf", false}} {
		w := chatFileRequest(t, s, "GET", "/api/chats/"+c.ID+"/files/raw?path="+tc.name, "")
		if w.Code != 200 || w.Header().Get("X-Content-Type-Options") != "nosniff" || (w.Header().Get("Content-Security-Policy") != "") != tc.csp {
			t.Errorf("%s: status=%d headers=%v", tc.name, w.Code, w.Header())
		}
	}
}

func TestChatFilesDelete(t *testing.T) {
	s, c := attachmentTestChat(t)
	root := c.Workdir
	_ = os.Mkdir(filepath.Join(root, "folder"), 0700)
	_ = os.WriteFile(filepath.Join(root, "folder", "x"), []byte("x"), 0600)
	_ = os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0600)
	for _, path := range []string{"f", "folder"} {
		w := chatFileRequest(t, s, "DELETE", "/api/chats/"+c.ID+"/files?path="+path, "")
		if w.Code != 204 {
			t.Errorf("delete %s = %d", path, w.Code)
		}
	}
	for _, path := range []string{"", ".", "./"} {
		w := chatFileRequest(t, s, "DELETE", "/api/chats/"+c.ID+"/files?path="+path, "")
		if w.Code != 400 {
			t.Errorf("root delete %q = %d", path, w.Code)
		}
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("root removed for %q: %v", path, err)
		}
	}
	inside := filepath.Join(root, "inside")
	_ = os.WriteFile(inside, []byte("inside"), 0600)
	link := filepath.Join(root, "link")
	_ = os.Symlink(inside, link)
	w := chatFileRequest(t, s, "DELETE", "/api/chats/"+c.ID+"/files?path=link", "")
	if w.Code != 204 {
		t.Fatalf("symlink delete = %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(inside); err != nil {
		t.Fatal("symlink target removed")
	}
	_ = os.Mkdir(filepath.Join(root, "attachments"), 0700)
	_ = os.WriteFile(filepath.Join(root, "attachments", "pending.txt"), []byte("pending"), 0600)
	s.attachments[c.ID] = map[string]ChatAttachment{"pending.txt": {Name: "pending.txt"}}
	w = chatFileRequest(t, s, "DELETE", "/api/chats/"+c.ID+"/files?path=attachments/pending.txt", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("pending delete = %d", w.Code)
	}
	_ = os.Symlink("attachments", filepath.Join(root, "alias"))
	_ = os.Symlink(".", filepath.Join(root, "here"))
	for _, path := range []string{"alias/pending.txt", "here/attachments"} {
		if w = chatFileRequest(t, s, "DELETE", "/api/chats/"+c.ID+"/files?path="+path, ""); w.Code != http.StatusConflict {
			t.Fatalf("pending delete via %s = %d", path, w.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "attachments", "pending.txt")); err != nil {
		t.Fatal("pending attachment removed")
	}
	_ = os.WriteFile(filepath.Join(root, "pending.txt"), []byte("other"), 0600)
	if w = chatFileRequest(t, s, "DELETE", "/api/chats/"+c.ID+"/files?path=pending.txt", ""); w.Code != 204 {
		t.Fatalf("same-name delete outside attachments = %d", w.Code)
	}
}

func TestChatFilesZipListing(t *testing.T) {
	s, c := attachmentTestChat(t)
	name := filepath.Join(c.Workdir, "a.zip")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	entry, _ := z.Create("one.txt")
	_, _ = entry.Write([]byte("123"))
	_ = z.Close()
	_ = f.Close()
	w := chatFileRequest(t, s, "GET", "/api/chats/"+c.ID+"/files/zip?path=a.zip", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"one.txt"`) {
		t.Fatalf("zip = %d %s", w.Code, w.Body.String())
	}
}
