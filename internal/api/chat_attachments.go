package api

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxChatAttachments       = 10
	maxChatTextAttachment    = 1 << 20
	maxChatImageAttachment   = 10 << 20
	maxChatAttachmentName    = 120
	maxChatAttachmentRequest = 14 << 20
)

type ChatAttachment struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	MIME string `json:"mime"`
	Size int64  `json:"size"`
}

// ChatSendRequest is passed intact to the runtime driver's Send path. Runtime
// attachment delivery is implemented separately.
type ChatSendRequest struct {
	Text             string           `json:"text"`
	Steer            bool             `json:"-"`
	StoredText       string           `json:"-"`
	Attachments      []ChatAttachment `json:"attachments,omitempty"`
	FromChatID       *string          `json:"-"`
	ReportToChatID   *string          `json:"-"`
	AgentHopsManaged bool             `json:"-"`
}

func decodeChatAttachments(raw string) ([]ChatAttachment, error) {
	var attachments []ChatAttachment
	if err := json.Unmarshal([]byte(raw), &attachments); err != nil {
		return nil, err
	}
	if attachments == nil {
		return nil, errors.New("attachments must be a JSON array")
	}
	for _, attachment := range attachments {
		if !validChatAttachmentName(attachment.Name) || attachment.Size < 0 || attachment.Kind != "text" && attachment.Kind != "image" || attachment.MIME == "" {
			return nil, errors.New("invalid attachment descriptor")
		}
	}
	return attachments, nil
}

func sanitizeChatAttachmentName(name string) string {
	var cleaned strings.Builder
	for _, r := range name {
		if r == '/' || r == '\\' || strings.ContainsRune(`<>:"|?*`, r) || unicode.IsControl(r) {
			continue
		}
		cleaned.WriteRune(r)
	}
	name = strings.TrimRight(cleaned.String(), ". ")
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	base := strings.TrimRight(name, ". ")
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	upper := strings.ToUpper(strings.TrimRight(base, ". "))
	reserved := upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL"
	if len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && upper[3] >= '1' && upper[3] <= '9' {
		reserved = true
	}
	if reserved {
		name = "_" + name
	}
	if len(name) <= maxChatAttachmentName {
		return name
	}
	ext := filepath.Ext(name)
	if len(ext) >= maxChatAttachmentName {
		return truncateUTF8(name, maxChatAttachmentName)
	}
	return truncateUTF8(strings.TrimSuffix(name, ext), maxChatAttachmentName-len(ext)) + ext
}

func truncateUTF8(value string, max int) string {
	if len(value) <= max {
		return value
	}
	for max > 0 && !utf8.ValidString(value[:max]) {
		max--
	}
	return value[:max]
}

func (s *Server) uploadChatAttachments(w http.ResponseWriter, r *http.Request) {
	s.attachmentsMu.Lock()
	defer s.attachmentsMu.Unlock()
	c, ok := s.chatForAttachments(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatAttachmentRequest)
	var request struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeAttachmentError(w, err)
		return
	}
	var extra any
	if err := json.NewDecoder(r.Body).Decode(&extra); !errors.Is(err, io.EOF) {
		writeAttachmentError(w, errors.New("invalid JSON request"))
		return
	}
	if request.Name == "" {
		errw(w, http.StatusBadRequest, "attachment name is required")
		return
	}
	data, err := base64.StdEncoding.DecodeString(request.Data)
	if err != nil {
		errw(w, http.StatusBadRequest, "invalid base64 attachment data")
		return
	}
	kind, contentType, err := classifyChatAttachment(data)
	if err != nil {
		writeAttachmentError(w, err)
		return
	}
	pending := s.attachments[c.ID]
	if len(pending)+1 > maxChatAttachments {
		errw(w, http.StatusBadRequest, "at most 10 unsent attachments are allowed")
		return
	}
	dir, err := chatAttachmentsDir(c.Workdir, true)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := sanitizeChatAttachmentName(request.Name)
	storedName, err := writeChatAttachment(dir, name, data)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	attachment := ChatAttachment{Name: storedName, Kind: kind, MIME: contentType, Size: int64(len(data))}
	if pending == nil {
		pending = make(map[string]ChatAttachment)
		s.attachments[c.ID] = pending
	}
	pending[storedName] = attachment
	write(w, http.StatusCreated, []ChatAttachment{attachment})
}

func classifyChatAttachment(data []byte) (string, string, error) {
	detected := http.DetectContentType(data[:min(len(data), 512)])
	if len(data) > maxChatImageAttachment {
		return "", "", errChatAttachmentTooLarge
	}
	if detected == "image/png" || detected == "image/jpeg" || detected == "image/gif" || isWebP(data) {
		contentType := detected
		if isWebP(data) {
			contentType = "image/webp"
		}
		return "image", contentType, nil
	}
	if len(data) > maxChatTextAttachment {
		return "", "", errChatAttachmentTooLarge
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", "", errChatAttachmentType
	}
	if detected == "application/pdf" || detected == "application/zip" || detected == "application/x-ole-storage" || !strings.HasPrefix(detected, "text/") && detected != "application/octet-stream" {
		return "", "", errChatAttachmentType
	}
	contentType := detected
	if contentType == "application/octet-stream" {
		contentType = "text/plain; charset=utf-8"
	}
	return "text", contentType, nil
}

func isWebP(data []byte) bool {
	return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

var (
	errChatAttachmentType     = errors.New("unsupported file type; upload UTF-8 text or PNG, JPEG, GIF, or WebP images")
	errChatAttachmentTooLarge = errors.New("attachment exceeds size limit")
	errChatAttachmentName     = errors.New("invalid attachment name")
	errChatAttachmentNotFound = errors.New("attachment not found")
	errChatAttachmentUnsafe   = errors.New("attachment path is unsafe")
)

func writeAttachmentError(w http.ResponseWriter, err error) {
	var maxBytes *http.MaxBytesError
	switch {
	case errors.Is(err, errChatAttachmentTooLarge), errors.As(err, &maxBytes):
		errw(w, http.StatusRequestEntityTooLarge, "attachment exceeds size limit")
	case errors.Is(err, errChatAttachmentType):
		errw(w, http.StatusBadRequest, err.Error())
	default:
		errw(w, http.StatusBadRequest, "invalid attachment request: "+err.Error())
	}
}

func chatAttachmentsDir(workdir string, create bool) (string, error) {
	root, err := filepath.Abs(workdir)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "attachments")
	if create {
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errChatAttachmentUnsafe
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	inside, err := pathWithin(root, resolved)
	if err != nil || !inside || filepath.Clean(resolved) != filepath.Join(root, "attachments") {
		return "", errChatAttachmentUnsafe
	}
	return resolved, nil
}

func pathWithin(root, path string) (bool, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false, err
	}
	return rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

func writeChatAttachment(dir, name string, data []byte) (string, error) {
	temp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	base, ext := strings.TrimSuffix(name, filepath.Ext(name)), filepath.Ext(name)
	for index := 1; ; index++ {
		candidate := name
		if index > 1 {
			suffix := fmt.Sprintf(" (%d)", index)
			remaining := maxChatAttachmentName - len(suffix) - len(ext)
			if remaining < 0 {
				ext = truncateUTF8(ext, maxChatAttachmentName-len(suffix))
				remaining = 0
			}
			candidate = truncateUTF8(base, remaining) + suffix + ext
		}
		if err := os.Link(tempPath, filepath.Join(dir, candidate)); err == nil {
			return candidate, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
}

func validChatAttachmentName(name string) bool {
	return name != "" && name != "." && name != ".." && !filepath.IsAbs(name) && !strings.ContainsAny(name, `/\\<>:"|?*`) && filepath.Base(name) == name && sanitizeChatAttachmentName(name) == name
}

func (s *Server) chatForAttachments(w http.ResponseWriter, r *http.Request) (chat, bool) {
	c, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, idParam(r, "id")))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "chat not found")
		return chat{}, false
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return chat{}, false
	}
	return c, true
}

func (s *Server) attachmentPath(c chat, name string) (string, error) {
	if !validChatAttachmentName(name) {
		return "", errChatAttachmentName
	}
	dir, err := chatAttachmentsDir(c.Workdir, false)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errChatAttachmentUnsafe
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	inside, err := pathWithin(dir, resolved)
	if err != nil || !inside || filepath.Dir(resolved) != dir {
		return "", errChatAttachmentUnsafe
	}
	return resolved, nil
}

func (s *Server) getChatAttachment(w http.ResponseWriter, r *http.Request) {
	c, ok := s.chatForAttachments(w, r)
	if !ok {
		return
	}
	name := idParam(r, "name")
	path, err := s.attachmentPath(c, name)
	if err != nil {
		s.writeAttachmentPathError(w, err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		s.writeAttachmentPathError(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	var sniff [512]byte
	n, err := file.Read(sniff[:])
	if err != nil && !errors.Is(err, io.EOF) {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	contentType := http.DetectContentType(sniff[:n])
	if isWebP(sniff[:n]) {
		contentType = "image/webp"
	}
	image := strings.HasPrefix(contentType, "image/")
	contentDisposition := "attachment"
	if image {
		contentDisposition = "inline"
	}
	if value := mime.FormatMediaType(contentDisposition, map[string]string{"filename": name}); value != "" {
		w.Header().Set("Content-Disposition", value)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func (s *Server) deleteChatAttachment(w http.ResponseWriter, r *http.Request) {
	c, ok := s.chatForAttachments(w, r)
	if !ok {
		return
	}
	name := idParam(r, "name")
	if !validChatAttachmentName(name) {
		errw(w, http.StatusBadRequest, errChatAttachmentName.Error())
		return
	}
	s.attachmentsMu.Lock()
	defer s.attachmentsMu.Unlock()
	sent, err := s.chatAttachmentWasSent(c.ID, name)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sent {
		errw(w, http.StatusConflict, "sent attachments cannot be deleted")
		return
	}
	path, err := s.attachmentPath(c, name)
	if err != nil {
		s.writeAttachmentPathError(w, err)
		return
	}
	if _, ok := s.attachments[c.ID][name]; !ok {
		errw(w, http.StatusNotFound, errChatAttachmentNotFound.Error())
		return
	}
	if err := os.Remove(path); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	delete(s.attachments[c.ID], name)
	noContent(w)
}

func (s *Server) revealChatAttachment(w http.ResponseWriter, r *http.Request) {
	c, ok := s.chatForAttachments(w, r)
	if !ok {
		return
	}
	name := idParam(r, "name")
	if !validChatAttachmentName(name) {
		errw(w, http.StatusBadRequest, errChatAttachmentName.Error())
		return
	}
	path, err := revealPath(c.Workdir, filepath.Join("attachments", name))
	if err != nil {
		s.writeAttachmentPathError(w, err)
		return
	}
	if _, err := s.attachmentPath(c, name); err != nil {
		s.writeAttachmentPathError(w, err)
		return
	}
	if err := s.revealFile(path); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	noContent(w)
}

func (s *Server) writeAttachmentPathError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errChatAttachmentName), errors.Is(err, errChatAttachmentUnsafe), errors.Is(err, errPathOutsideWorkdir):
		errw(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, os.ErrNotExist):
		errw(w, http.StatusNotFound, errChatAttachmentNotFound.Error())
	default:
		errw(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) chatAttachmentWasSent(chatID, name string) (bool, error) {
	rows, err := s.db.Query(`SELECT attachments FROM chat_message WHERE chat_id=? AND role='user'`, chatID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return false, err
		}
		attachments, err := decodeChatAttachments(raw)
		if err != nil {
			return false, fmt.Errorf("invalid chat message attachments: %w", err)
		}
		for _, attachment := range attachments {
			if attachment.Name == name {
				return true, nil
			}
		}
	}
	return false, rows.Err()
}

type movedFile struct{ from, to string }

// movePendingAttachments moves unsent attachments into workdir's attachments
// folder. On failure it moves back whatever it already moved. Caller holds
// attachmentsMu.
func (s *Server) movePendingAttachments(c chat, workdir string) ([]movedFile, error) {
	pending := s.attachments[c.ID]
	if len(pending) == 0 {
		return nil, nil
	}
	dir, err := chatAttachmentsDir(workdir, true)
	if err != nil {
		return nil, err
	}
	var moved []movedFile
	for name := range pending {
		from, err := s.attachmentPath(c, name)
		if err == nil {
			to := filepath.Join(dir, name)
			if err = moveFile(from, to); err == nil {
				moved = append(moved, movedFile{from, to})
				continue
			}
			if errors.Is(err, os.ErrExist) {
				err = fmt.Errorf("attachments/%s already exists in the new folder: %w", name, os.ErrExist)
			}
		}
		return nil, errors.Join(err, restoreMovedFiles(moved))
	}
	return moved, nil
}

func restoreMovedFiles(moved []movedFile) error {
	var joined error
	for _, file := range moved {
		joined = errors.Join(joined, moveFile(file.to, file.from))
	}
	return joined
}

// moveFile never overwrites to. It hard-links when possible and falls back
// to copy and delete, e.g. across drives.
func moveFile(from, to string) error {
	err := os.Link(from, to)
	if errors.Is(err, os.ErrExist) {
		return err
	}
	if err != nil {
		if err = copyNewFile(from, to); err != nil {
			return err
		}
	}
	if err := os.Remove(from); err != nil {
		return errors.Join(err, os.Remove(to))
	}
	return nil
}

func copyNewFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(dst, src); err == nil {
		err = dst.Sync()
	}
	if closeErr := dst.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return errors.Join(err, os.Remove(to))
	}
	return nil
}

func (s *Server) markChatAttachmentsSent(c chat, selected map[string]bool) error {
	pending := s.attachments[c.ID]
	var joined error
	for name := range pending {
		if !selected[name] {
			if path, err := s.attachmentPath(c, name); err == nil {
				joined = errors.Join(joined, os.Remove(path))
			} else if !errors.Is(err, os.ErrNotExist) {
				joined = errors.Join(joined, err)
			}
		}
		delete(pending, name)
	}
	delete(s.attachments, c.ID)
	return joined
}
