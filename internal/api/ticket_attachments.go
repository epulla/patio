package api

import (
	"database/sql"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	ticketImageLimit     = 10 << 20
	ticketFileLimit      = 20 << 20
	maxTicketAttachments = 20
)

type ticketAttachment struct {
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	Kind         string `json:"kind"`
	MIME         string `json:"mime"`
	Size         int64  `json:"size"`
	CreatedAt    string `json:"created_at"`
	Path         string `json:"path"`
}

func (s *Server) ticketID(key string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM ticket WHERE id=? OR code=? OR id IN (SELECT ticket_id FROM old_ticket_code WHERE old_ticket_code=?)`, key, key, key).Scan(&id)
	return id, err
}
func (s *Server) ticketAttachmentRows(id string) ([]ticketAttachment, error) {
	rows, err := s.db.Query(`SELECT name,original_name,kind,mime,size,created_at FROM ticket_attachment WHERE ticket_id=? ORDER BY created_at,rowid`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ticketAttachment, 0)
	for rows.Next() {
		var a ticketAttachment
		if err = rows.Scan(&a.Name, &a.OriginalName, &a.Kind, &a.MIME, &a.Size, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Path = filepath.Join(s.dbDir, "attachments", id, a.Name)
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Server) listTicketAttachments(w http.ResponseWriter, r *http.Request) {
	id, e := s.ticketID(idParam(r, "id"))
	if errors.Is(e, sql.ErrNoRows) {
		errw(w, 404, "ticket not found")
		return
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	a, e := s.ticketAttachmentRows(id)
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	write(w, 200, a)
}
func (s *Server) uploadTicketAttachment(w http.ResponseWriter, r *http.Request) {
	id, e := s.ticketID(idParam(r, "id"))
	if errors.Is(e, sql.ErrNoRows) {
		errw(w, 404, "ticket not found")
		return
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	original := r.URL.Query().Get("name")
	if original == "" {
		errw(w, 400, "attachment name is required")
		return
	}
	var count int
	if e = s.db.QueryRow(`SELECT count(*) FROM ticket_attachment WHERE ticket_id=?`, id).Scan(&count); e != nil {
		errw(w, 500, e.Error())
		return
	}
	if count >= maxTicketAttachments {
		errw(w, 400, "at most 20 attachments are allowed")
		return
	}
	prefix, e := io.ReadAll(io.LimitReader(r.Body, 512))
	if e != nil {
		writeAttachmentError(w, e)
		return
	}
	if len(prefix) == 0 {
		errw(w, 400, "attachment is empty")
		return
	}
	kind, contentType, e := detectAttachmentType(original, prefix)
	if e != nil {
		writeAttachmentError(w, errChatAttachmentType)
		return
	}
	root, e := chatAttachmentsDir(s.dbDir, true)
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	dir := filepath.Join(root, id)
	if e = os.Mkdir(dir, 0700); e != nil && !errors.Is(e, os.ErrExist) {
		errw(w, 500, e.Error())
		return
	}
	info, e := os.Lstat(dir)
	if e != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		errw(w, 500, errChatAttachmentUnsafe.Error())
		return
	}
	safe := sanitizeChatAttachmentName(original)
	temp, e := os.CreateTemp(dir, ".upload-*")
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	limit := int64(ticketFileLimit)
	if kind == "image" {
		limit = ticketImageLimit
	}
	stored, size, e := streamChatAttachment(temp, dir, safe, prefix, r.Body, kind, ticketFileLimit, limit)
	if e != nil {
		writeAttachmentError(w, e)
		return
	}
	t := now()
	result, e := s.db.Exec(`INSERT INTO ticket_attachment(id,ticket_id,name,original_name,mime,kind,size,created_at)
		SELECT ?,?,?,?,?,?,?,? WHERE (SELECT count(*) FROM ticket_attachment WHERE ticket_id=?) < ?`, uid(), id, stored, original, contentType, kind, size, t, id, maxTicketAttachments)
	if e != nil {
		_ = os.Remove(filepath.Join(dir, stored))
		errw(w, 500, e.Error())
		return
	}
	inserted, e := result.RowsAffected()
	if e != nil {
		_ = os.Remove(filepath.Join(dir, stored))
		errw(w, 500, e.Error())
		return
	}
	if inserted == 0 {
		_ = os.Remove(filepath.Join(dir, stored))
		errw(w, 400, "at most 20 attachments are allowed")
		return
	}
	write(w, 201, ticketAttachment{stored, original, kind, contentType, size, t, filepath.Join(dir, stored)})
}
func (s *Server) ticketAttachmentPath(w http.ResponseWriter, id, name string) string {
	var exists int
	if s.db.QueryRow(`SELECT 1 FROM ticket_attachment WHERE ticket_id=? AND name=?`, id, name).Scan(&exists) != nil {
		return ""
	}
	root, err := chatAttachmentsDir(s.dbDir, false)
	if err != nil {
		return ""
	}
	p, err := attachmentPathIn(root, id, name)
	if err != nil {
		return ""
	}
	return p
}
func (s *Server) getTicketAttachment(w http.ResponseWriter, r *http.Request) {
	id, e := s.ticketID(idParam(r, "id"))
	if errors.Is(e, sql.ErrNoRows) {
		errw(w, 404, "ticket not found")
		return
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	name := idParam(r, "name")
	path := s.ticketAttachmentPath(w, id, name)
	if path == "" {
		errw(w, 404, "attachment not found")
		return
	}
	f, e := os.Open(path)
	if e != nil {
		errw(w, 404, "attachment not found")
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	var sniff [512]byte
	n, _ := f.Read(sniff[:])
	ct := http.DetectContentType(sniff[:n])
	disposition := "attachment"
	if strings.HasPrefix(ct, "image/") {
		disposition = "inline"
	}
	if v := mime.FormatMediaType(disposition, map[string]string{"filename": name}); v != "" {
		w.Header().Set("Content-Disposition", v)
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = f.Seek(0, 0)
	http.ServeContent(w, r, name, info.ModTime(), f)
}
func (s *Server) deleteTicketAttachment(w http.ResponseWriter, r *http.Request) {
	id, e := s.ticketID(idParam(r, "id"))
	if errors.Is(e, sql.ErrNoRows) {
		errw(w, 404, "ticket not found")
		return
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	name := idParam(r, "name")
	path := s.ticketAttachmentPath(w, id, name)
	if path == "" {
		errw(w, 404, "attachment not found")
		return
	}
	if e = os.Remove(path); e != nil {
		errw(w, 500, e.Error())
		return
	}
	_, e = s.db.Exec(`DELETE FROM ticket_attachment WHERE ticket_id=? AND name=?`, id, name)
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	noContent(w)
}
func (s *Server) revealTicketAttachment(w http.ResponseWriter, r *http.Request) {
	id, e := s.ticketID(idParam(r, "id"))
	if errors.Is(e, sql.ErrNoRows) {
		errw(w, 404, "ticket not found")
		return
	}
	if e != nil {
		errw(w, 500, e.Error())
		return
	}
	path := s.ticketAttachmentPath(w, id, idParam(r, "name"))
	if path == "" {
		errw(w, 404, "attachment not found")
		return
	}
	if e = s.revealFile(path); e != nil {
		errw(w, 500, e.Error())
		return
	}
	noContent(w)
}
