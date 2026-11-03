package api

import (
	"bytes"
	"database/sql"
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
)

var testPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\x0bIDAT\x08\xd7c\xf8\xcf\xc0\xf0\x1f\x00\x05\x00\x01\xff\x89\x99=\x1d\x00\x00\x00\x00IEND\xaeB`\x82")

func ticketAttachmentFixture(t *testing.T) (*sql.DB, http.Handler, testBoard, testTicket, string) {
	t.Helper()
	database, h := newAPI(t)
	b := createBoard(t, h, "ATT", nil)
	ticket := createTicket(t, h, b.ID, "")
	var dbPath string
	if err := database.QueryRow("PRAGMA database_list").Scan(new(int), new(string), &dbPath); err != nil {
		t.Fatal(err)
	}
	return database, h, b, ticket, filepath.Dir(dbPath)
}

func postTicketAttachment(t *testing.T, h http.Handler, key, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/tickets/"+key+"/attachments?name="+url.QueryEscape(name), bytes.NewReader(data))
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	return res
}

func TestTicketAttachmentUploadServeAndCode(t *testing.T) {
	db, h, _, ticket, _ := ticketAttachmentFixture(t)
	res := postTicketAttachment(t, h, ticket.ID, "image.png", testPNG)
	if res.Code != 201 {
		t.Fatalf("upload %d: %s", res.Code, res.Body)
	}
	var a ticketAttachment
	if err := json.Unmarshal(res.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest("GET", "/api/tickets/"+ticket.Code+"/attachments/"+url.PathEscape(a.Name), nil))
	if get.Code != 200 || !bytes.Equal(get.Body.Bytes(), testPNG) || !strings.HasPrefix(get.Header().Get("Content-Disposition"), "inline") || get.Header().Get("X-Content-Type-Options") != "nosniff" || get.Header().Get("Content-Security-Policy") != "default-src 'none'; sandbox" {
		t.Fatalf("serve status=%d headers=%v", get.Code, get.Header())
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM ticket_attachment WHERE ticket_id=?", ticket.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows=%d err=%v", n, err)
	}
}

func TestTicketAttachmentImageLimit(t *testing.T) {
	_, h, _, ticket, _ := ticketAttachmentFixture(t)
	data := append([]byte(nil), testPNG...)
	data = append(data, bytes.Repeat([]byte{0}, 10<<20)...)
	r := postTicketAttachment(t, h, ticket.ID, "large.png", data)
	if r.Code != 413 {
		t.Fatalf("status=%d body=%s", r.Code, r.Body)
	}
}

func TestTicketAttachmentMaxCount(t *testing.T) {
	db, h, _, ticket, _ := ticketAttachmentFixture(t)
	for i := 0; i < 20; i++ {
		if _, e := db.Exec(`INSERT INTO ticket_attachment(id,ticket_id,name,original_name,mime,kind,size,created_at) VALUES(?,?,?,?,?,?,?,?)`, uid(), ticket.ID, fmt.Sprintf("f%d.txt", i), "f.txt", "text/plain", "text", 1, now()); e != nil {
			t.Fatal(e)
		}
	}
	r := postTicketAttachment(t, h, ticket.ID, "21.txt", []byte("ok"))
	if r.Code != 400 {
		t.Fatalf("status=%d", r.Code)
	}
}

func TestTicketAttachmentUnsupportedType(t *testing.T) {
	_, h, _, ticket, _ := ticketAttachmentFixture(t)
	r := postTicketAttachment(t, h, ticket.ID, "x.bin", []byte{0, 1, 2, 3, 0xff})
	if r.Code != 400 {
		t.Fatalf("status=%d", r.Code)
	}
}

func TestTicketAttachmentTextValidation(t *testing.T) {
	for name, body := range map[string][]byte{"NUL": {'a', 0, 'b'}, "invalid UTF-8": {'a', 0xff}} {
		t.Run(name, func(t *testing.T) {
			_, h, _, ticket, _ := ticketAttachmentFixture(t)
			if r := postTicketAttachment(t, h, ticket.ID, "invalid.txt", body); r.Code != 400 {
				t.Fatalf("status=%d body=%s", r.Code, r.Body)
			}
		})
	}
}

func TestTicketAttachmentTraversalNameSafe(t *testing.T) {
	_, h, _, ticket, root := ticketAttachmentFixture(t)
	r := postTicketAttachment(t, h, ticket.ID, "../escape.txt", []byte("safe"))
	if r.Code != 201 {
		t.Fatalf("status=%d %s", r.Code, r.Body)
	}
	var a ticketAttachment
	if err := json.Unmarshal(r.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	// The server reports resolved paths; Windows runners use 8.3 short temp dirs, so resolve root too.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	// sanitizeChatAttachmentName drops path separators, so the name stays inside the ticket folder.
	if a.Name != "..escape.txt" || a.Path != filepath.Join(root, "attachments", ticket.ID, "..escape.txt") {
		t.Fatalf("unsafe stored name/path: %q %q", a.Name, a.Path)
	}
	if _, err := os.Stat(a.Path); err != nil {
		t.Fatal(err)
	}
	for _, outside := range []string{filepath.Join(root, "attachments", "escape.txt"), filepath.Join(root, "escape.txt")} {
		if _, err := os.Stat(outside); !os.IsNotExist(err) {
			t.Fatalf("file written outside ticket folder: %s", outside)
		}
	}
}

func TestTicketAttachmentGetTicketListing(t *testing.T) {
	_, h, _, ticket, root := ticketAttachmentFixture(t)
	var empty map[string]json.RawMessage
	requestJSON(t, h, "GET", "/api/tickets/"+ticket.ID, nil, &empty)
	var items []ticketAttachment
	if err := json.Unmarshal(empty["attachments"], &items); err != nil || items == nil || len(items) != 0 {
		t.Fatalf("empty=%s err=%v", empty["attachments"], err)
	}
	r := postTicketAttachment(t, h, ticket.ID, "a.txt", []byte("hello"))
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body)
	}
	var item ticketAttachment
	if err := json.Unmarshal(r.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	requestJSON(t, h, "GET", "/api/tickets/"+ticket.ID, nil, &got)
	if err := json.Unmarshal(got["attachments"], &items); err != nil || len(items) != 1 || !filepath.IsAbs(items[0].Path) || filepath.Dir(items[0].Path) != filepath.Join(root, "attachments", ticket.ID) {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestTicketAttachmentTicketDeleteCleansFilesAndRows(t *testing.T) {
	db, h, _, ticket, root := ticketAttachmentFixture(t)
	r := postTicketAttachment(t, h, ticket.ID, "a.txt", []byte("hello"))
	if r.Code != 201 {
		t.Fatal(r.Code)
	}
	dir := filepath.Join(root, "attachments", ticket.ID)
	if code := requestJSON(t, h, http.MethodDelete, "/api/tickets/"+ticket.ID, nil, nil); code != 204 {
		t.Fatalf("delete=%d", code)
	}
	if _, e := os.Stat(dir); !os.IsNotExist(e) {
		t.Fatalf("folder remains: %v", e)
	}
	var n int
	if e := db.QueryRow("SELECT count(*) FROM ticket_attachment WHERE ticket_id=?", ticket.ID).Scan(&n); e != nil || n != 0 {
		t.Fatalf("rows=%d err=%v", n, e)
	}
}

func TestTicketAttachmentDeleteDoesNotFollowAttachmentsSymlink(t *testing.T) {
	_, h, _, ticket, root := ticketAttachmentFixture(t)
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, ticket.ID), 0700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(outside, ticket.ID, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "attachments")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if code := requestJSON(t, h, http.MethodDelete, "/api/tickets/"+ticket.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete=%d", code)
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "keep" {
		t.Fatalf("outside file data=%q err=%v", data, err)
	}
}

func TestTicketAttachmentConcurrentUploadsRespectLimit(t *testing.T) {
	db, h, _, ticket, _ := ticketAttachmentFixture(t)
	for i := 0; i < 19; i++ {
		if _, err := db.Exec(`INSERT INTO ticket_attachment(id,ticket_id,name,original_name,mime,kind,size,created_at) VALUES(?,?,?,?,?,?,?,?)`, uid(), ticket.ID, fmt.Sprintf("existing-%d.txt", i), "existing.txt", "text/plain", "text", 1, now()); err != nil {
			t.Fatal(err)
		}
	}
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses <- postTicketAttachment(t, h, ticket.ID, fmt.Sprintf("concurrent-%d.txt", i), []byte("ok")).Code
		}(i)
	}
	wg.Wait()
	close(statuses)
	success, rejected := 0, 0
	for code := range statuses {
		if code == http.StatusCreated {
			success++
		} else if code == http.StatusBadRequest {
			rejected++
		} else {
			t.Fatalf("unexpected status %d", code)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM ticket_attachment WHERE ticket_id=?`, ticket.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if success != 1 || rejected != 1 || count != 20 {
		t.Fatalf("success=%d rejected=%d count=%d", success, rejected, count)
	}
}

func TestTicketAttachmentInsertFailureRemovesFile(t *testing.T) {
	db, h, _, ticket, root := ticketAttachmentFixture(t)
	if _, err := db.Exec(`CREATE TRIGGER fail_ticket_attachment BEFORE INSERT ON ticket_attachment BEGIN SELECT RAISE(FAIL,'forced'); END`); err != nil {
		t.Fatal(err)
	}
	r := postTicketAttachment(t, h, ticket.ID, "a.txt", []byte("hello"))
	if r.Code != 500 {
		t.Fatalf("status=%d", r.Code)
	}
	dir := filepath.Join(root, "attachments", ticket.ID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("left files: %v", entries)
	}
}

func TestTicketAttachmentSymlinkPathRejected(t *testing.T) {
	_, h, _, ticket, root := ticketAttachmentFixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "attachments")); err != nil {
		t.Skip(err)
	}
	r := postTicketAttachment(t, h, ticket.ID, "x.txt", []byte("safe"))
	if r.Code < 400 {
		t.Fatalf("status=%d", r.Code)
	}
}
