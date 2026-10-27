package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

type attachmentUpload struct {
	name string
	data []byte
}

func attachmentTestChat(t *testing.T) (*Server, chat) {
	t.Helper()
	database, handler, _ := chatAPI(t)
	connectorID := createTestConnector(t, database)
	var created chat
	if code := requestJSON(t, handler, http.MethodPost, "/api/chats", map[string]string{"connector_id": connectorID}, &created); code != http.StatusCreated {
		t.Fatalf("create chat status = %d", code)
	}
	return handler.(*Server), created
}

func postAttachmentFiles(t *testing.T, handler http.Handler, chatID string, files []attachmentUpload) *httptest.ResponseRecorder {
	t.Helper()
	var all []ChatAttachment
	for _, file := range files {
		request := httptest.NewRequest(http.MethodPost, "/api/chats/"+chatID+"/attachments?name="+url.QueryEscape(file.name), bytes.NewReader(file.data))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		response := recorder
		if response.Code != http.StatusCreated {
			return response
		}
		var uploaded []ChatAttachment
		if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil {
			t.Fatal(err)
		}
		all = append(all, uploaded...)
	}
	response := httptest.NewRecorder()
	response.Code = http.StatusCreated
	encoded, _ := json.Marshal(all)
	response.Body.Write(encoded)
	return response
}

func TestChatAttachmentUploadLimitsTypesAndCount(t *testing.T) {
	tests := []struct {
		name  string
		files []attachmentUpload
		want  int
	}{
		{name: "text 11 MB", files: []attachmentUpload{{"note.txt", bytes.Repeat([]byte("a"), 11<<20)}}, want: http.StatusCreated},
		{name: "image at limit", files: []attachmentUpload{{"picture.png", append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, maxChatImageAttachment-8)...)}}, want: http.StatusCreated},
		{name: "image over limit", files: []attachmentUpload{{"picture.png", append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, maxChatImageAttachment-7)...)}}, want: http.StatusRequestEntityTooLarge},
		{name: "invalid UTF-8 across chunk", files: []attachmentUpload{{"bad.txt", append(append(bytes.Repeat([]byte("a"), 512+32*1024-1), 0xc2), 0x20)}}, want: http.StatusBadRequest},
		{name: "empty rejected", files: []attachmentUpload{{"empty.png", nil}}, want: http.StatusBadRequest},
		{name: "pdf accepted", files: []attachmentUpload{{"document.pdf", []byte("%PDF-1.7\ntext")}}, want: http.StatusCreated},
		{name: "docx accepted", files: []attachmentUpload{{"document.docx", []byte("PK\x03\x04contents")}}, want: http.StatusCreated},
		{name: "xlsx accepted", files: []attachmentUpload{{"sheet.xlsx", []byte("PK\x03\x04contents")}}, want: http.StatusCreated},
		{name: "legacy xls accepted", files: []attachmentUpload{{"sheet.xls", []byte("\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1contents")}}, want: http.StatusCreated},
		{name: "zip accepted", files: []attachmentUpload{{"archive.zip", []byte("PK\x03\x04contents")}}, want: http.StatusCreated},
		{name: "CSV Windows-1252 accepted", files: []attachmentUpload{{"export.csv", []byte{'n', 'o', 'm', 'b', 'r', 'e', ';', 'a', '\xF1', 'o', '\n'}}}, want: http.StatusCreated},
		{name: "zip magic jar rejected", files: []attachmentUpload{{"archive.jar", []byte("PK\x03\x04contents")}}, want: http.StatusBadRequest},
		{name: "random binary rejected", files: []attachmentUpload{{"app.exe", []byte("MZ\x90\x00contents")}}, want: http.StatusBadRequest},
		{name: "invalid UTF-8", files: []attachmentUpload{{"bad.txt", []byte{0xff}}}, want: http.StatusBadRequest},
		{name: "NUL rejected", files: []attachmentUpload{{"bad.bin", []byte{'a', 0, 'b'}}}, want: http.StatusBadRequest},
		{name: "more than ten", files: manyAttachmentFiles(11), want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, c := attachmentTestChat(t)
			response := postAttachmentFiles(t, server, c.ID, test.files)
			if response.Code != test.want {
				t.Fatalf("upload status = %d, want %d: %s", response.Code, test.want, response.Body.String())
			}
		})
	}
	server, c := attachmentTestChat(t)
	response := postAttachmentFiles(t, server, c.ID, []attachmentUpload{{"document.pdf", []byte("%PDF-1.7\ntext")}})
	var pdf []ChatAttachment
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &pdf) != nil || len(pdf) != 1 || pdf[0].Kind != "file" || pdf[0].MIME != "application/pdf" {
		t.Fatalf("PDF upload = status %d, body %s", response.Code, response.Body.String())
	}
	server, c = attachmentTestChat(t)
	response = postAttachmentFiles(t, server, c.ID, []attachmentUpload{{"fake.txt", []byte("\x89PNG\r\n\x1a\nrest")}})
	var attachments []ChatAttachment
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &attachments) != nil || len(attachments) != 1 || attachments[0].Kind != "image" || attachments[0].MIME != "image/png" {
		t.Fatalf("fake extension upload = status %d, body %s", response.Code, response.Body.String())
	}
	for _, image := range []struct {
		name string
		data []byte
		mime string
	}{{"photo.jpg", []byte{0xff, 0xd8, 0xff, 0}, "image/jpeg"}, {"animation.gif", []byte("GIF89a"), "image/gif"}, {"photo.webp", []byte("RIFF\x04\x00\x00\x00WEBP"), "image/webp"}} {
		response := postAttachmentFiles(t, server, c.ID, []attachmentUpload{{image.name, image.data}})
		var uploaded []ChatAttachment
		if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &uploaded) != nil || len(uploaded) != 1 || uploaded[0].MIME != image.mime {
			t.Fatalf("upload %s = %d, %s", image.name, response.Code, response.Body.String())
		}
	}
}

func TestChatAttachmentRawValidation(t *testing.T) {
	server, c := attachmentTestChat(t)
	request := httptest.NewRequest(http.MethodPost, "/api/chats/"+c.ID+"/attachments", strings.NewReader("text"))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "attachment name is required") {
		t.Fatalf("missing name status = %d: %s", response.Code, response.Body.String())
	}
}

func manyAttachmentFiles(count int) []attachmentUpload {
	files := make([]attachmentUpload, count)
	for index := range files {
		files[index] = attachmentUpload{name: fmt.Sprintf("%d.txt", index), data: []byte("ok")}
	}
	return files
}

func TestSanitizeChatAttachmentName(t *testing.T) {
	tests := []struct{ input, want string }{
		{"CON", "_CON"}, {"nul.txt", "_nul.txt"}, {"com1.log", "_com1.log"}, {"LPT9.md", "_LPT9.md"},
		{"folder/name.txt", "foldername.txt"}, {`folder\name.txt`, "foldername.txt"},
		{"trailing. ", "trailing"}, {"圖像.png", "圖像.png"}, {"", "file"},
		{"a\x01b.txt", "ab.txt"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			if got := sanitizeChatAttachmentName(test.input); got != test.want {
				t.Fatalf("sanitize(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
	server, c := attachmentTestChat(t)
	response := postAttachmentFiles(t, server, c.ID, []attachmentUpload{{"folder/name.txt", []byte("safe")}})
	var sanitized []ChatAttachment
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &sanitized) != nil || len(sanitized) != 1 || sanitized[0].Name != "foldername.txt" {
		t.Fatalf("path-ish name upload = %d, %s", response.Code, response.Body.String())
	}
	long := sanitizeChatAttachmentName(strings.Repeat("é", 100) + ".md")
	if len(long) > maxChatAttachmentName || !utf8.ValidString(long) || !strings.HasSuffix(long, ".md") {
		t.Fatalf("truncated unicode name = %q (%d bytes)", long, len(long))
	}
	response = postAttachmentFiles(t, server, c.ID, []attachmentUpload{{"same.txt", []byte("one")}, {"same.txt", []byte("two")}})
	var got []ChatAttachment
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &got) != nil || len(got) != 2 || got[0].Name != "same.txt" || got[1].Name != "same (2).txt" {
		t.Fatalf("clashing upload = %d, %s", response.Code, response.Body.String())
	}
}

func TestChatAttachmentServeDeleteAndPathSafety(t *testing.T) {
	server, c := attachmentTestChat(t)
	response := postAttachmentFiles(t, server, c.ID, []attachmentUpload{{"preview.png", []byte("\x89PNG\r\n\x1a\nrest")}, {"notes.txt", []byte("hello")}})
	var attachments []ChatAttachment
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &attachments) != nil {
		t.Fatalf("upload = %d, %s", response.Code, response.Body.String())
	}
	for _, test := range []struct {
		name, contentType, disposition string
	}{{"preview.png", "image/png", "inline"}, {"notes.txt", "text/plain; charset=utf-8", "attachment"}} {
		request := httptest.NewRequest(http.MethodGet, "/api/chats/"+c.ID+"/attachments/"+test.name, nil)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != test.contentType || !strings.HasPrefix(recorder.Header().Get("Content-Disposition"), test.disposition) || recorder.Header().Get("X-Content-Type-Options") != "nosniff" || recorder.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("GET %s: status=%d headers=%v", test.name, recorder.Code, recorder.Header())
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req := httptest.NewRequest(method, "/", nil)
		req.SetPathValue("id", c.ID)
		req.SetPathValue("name", "../outside.txt")
		recorder := httptest.NewRecorder()
		if method == http.MethodGet {
			server.getChatAttachment(recorder, req)
		} else {
			server.deleteChatAttachment(recorder, req)
		}
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s escape status = %d", method, recorder.Code)
		}
	}
	attachmentDir := filepath.Join(c.Workdir, "attachments")
	if err := os.Symlink(outside, filepath.Join(attachmentDir, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req := httptest.NewRequest(method, "/", nil)
		req.SetPathValue("id", c.ID)
		req.SetPathValue("name", "escape.txt")
		recorder := httptest.NewRecorder()
		if method == http.MethodGet {
			server.getChatAttachment(recorder, req)
		} else {
			server.deleteChatAttachment(recorder, req)
		}
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s symlink status = %d", method, recorder.Code)
		}
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "outside" {
		t.Fatalf("outside file changed: %q, %v", data, err)
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/chats/"+c.ID+"/attachments/notes.txt", nil)
	deleteRes := httptest.NewRecorder()
	server.ServeHTTP(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("delete unsent = %d: %s", deleteRes.Code, deleteRes.Body.String())
	}
}

type attachmentTestDriver struct {
	mu      sync.Mutex
	request ChatSendRequest
}

func (d *attachmentTestDriver) send(request ChatSendRequest) error {
	d.mu.Lock()
	d.request = request
	d.mu.Unlock()
	return nil
}
func (*attachmentTestDriver) stop() error  { return nil }
func (*attachmentTestDriver) close() error { return nil }

func TestChatSendStoresAttachmentsAndCleansUnsent(t *testing.T) {
	server, c := attachmentTestChat(t)
	driver := &attachmentTestDriver{}
	previous, existed := runtimes["claude"]
	runtimes["claude"] = func(*chatManager, chat, string, *chatRuntime) (chatRuntimeDriver, error) { return driver, nil }
	t.Cleanup(func() {
		if existed {
			runtimes["claude"] = previous
		} else {
			delete(runtimes, "claude")
		}
	})
	response := postAttachmentFiles(t, server, c.ID, []attachmentUpload{{"sent.txt", []byte("sent")}, {"left.txt", []byte("left")}})
	var uploaded []ChatAttachment
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &uploaded) != nil {
		t.Fatalf("upload = %d, %s", response.Code, response.Body.String())
	}
	selected := uploaded[0]
	response = requestRecorderJSON(server, http.MethodPost, "/api/chats/"+c.ID+"/messages", map[string]any{"text": "", "attachments": []string{selected.Name}})
	var message ChatMessage
	if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &message) != nil {
		t.Fatalf("send = %d, %s", response.Code, response.Body.String())
	}
	if message.Content != "" || len(message.Attachments) != 1 || message.Attachments[0] != selected {
		t.Fatalf("sent message = %#v", message)
	}
	driver.mu.Lock()
	request := driver.request
	driver.mu.Unlock()
	if request.Text != "" || len(request.Attachments) != 1 || request.Attachments[0] != selected {
		t.Fatalf("driver request = %#v", request)
	}
	if _, err := os.Stat(filepath.Join(c.Workdir, "attachments", selected.Name)); err != nil {
		t.Fatalf("sent attachment disappeared: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Workdir, "attachments", uploaded[1].Name)); !os.IsNotExist(err) {
		t.Fatalf("unsent attachment remains: %v", err)
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/chats/"+c.ID+"/attachments/"+selected.Name, nil)
	deleteRes := httptest.NewRecorder()
	server.ServeHTTP(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusConflict {
		t.Fatalf("delete sent attachment = %d: %s", deleteRes.Code, deleteRes.Body.String())
	}
	var got struct {
		Messages []ChatMessage `json:"messages"`
	}
	if code := requestJSON(t, server, http.MethodGet, "/api/chats/"+c.ID, nil, &got); code != http.StatusOK || len(got.Messages) != 1 || len(got.Messages[0].Attachments) != 1 {
		t.Fatalf("GET message attachments status/data = %d, %#v", code, got)
	}
}

func requestRecorderJSON(handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestChatAttachmentCorruptMessageJSONAndMigrationDefault(t *testing.T) {
	server, c := attachmentTestChat(t)
	m, err := InsertChatMessage(server.db, c.ID, "user", "no attachment")
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := server.db.QueryRow(`SELECT attachments FROM chat_message WHERE id=?`, m.ID).Scan(&raw); err != nil || raw != "[]" {
		t.Fatalf("default attachments = %q, %v", raw, err)
	}
	if _, err := server.db.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at) VALUES(?,?,?,?,?)`, uid(), c.ID, "user", "migration default", now()); err != nil {
		t.Fatal(err)
	}
	if err := server.db.QueryRow(`SELECT attachments FROM chat_message WHERE content='migration default'`).Scan(&raw); err != nil || raw != "[]" {
		t.Fatalf("migration default attachments = %q, %v", raw, err)
	}
	if _, err := server.db.Exec(`UPDATE chat_message SET attachments='not-json' WHERE id=?`, m.ID); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/chats/"+c.ID, nil))
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "invalid chat message attachments") {
		t.Fatalf("corrupt attachments response = %d, %s", response.Code, response.Body.String())
	}
}

func TestConcurrentChatAttachmentUploads(t *testing.T) {
	server, c := attachmentTestChat(t)
	const count = 8
	responses := make(chan []ChatAttachment, count)
	errors := make(chan string, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response := postAttachmentFiles(t, server, c.ID, []attachmentUpload{{"same.txt", []byte("data")}})
			var uploaded []ChatAttachment
			if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &uploaded) != nil {
				errors <- fmt.Sprintf("upload status %d: %s", response.Code, response.Body.String())
				return
			}
			responses <- uploaded
		}()
	}
	wg.Wait()
	close(responses)
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	seen := make(map[string]bool)
	for uploaded := range responses {
		if len(uploaded) != 1 || seen[uploaded[0].Name] {
			t.Fatalf("duplicate/invalid concurrent upload: %#v", uploaded)
		}
		seen[uploaded[0].Name] = true
	}
	if len(seen) != count {
		t.Fatalf("stored %d unique uploads, want %d", len(seen), count)
	}
}
