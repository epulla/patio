package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zalando/go-keyring"
)

type allowedFolder struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	CreatedAt string `json:"created_at"`
}

type chat struct {
	ID              string           `json:"id"`
	ConnectorID     *string          `json:"connector_id"`
	Title           string           `json:"title"`
	Model           string           `json:"model"`
	Folder          *string          `json:"folder"`
	SessionID       *string          `json:"session_id"`
	CreatedAt       string           `json:"created_at"`
	UpdatedAt       string           `json:"updated_at"`
	Workdir         string           `json:"workdir"`
	Running         bool             `json:"running"`
	PendingCount    int              `json:"pending_count,omitempty"`
	OtherWindow     bool             `json:"other_window,omitempty"`
	Thinking        bool             `json:"thinking"`
	ShowThinking    bool             `json:"show_thinking"`
	AutoAllow       bool             `json:"auto_allow"`
	Capabilities    chatCapabilities `json:"capabilities"`
	ScheduledTaskID *string          `json:"scheduled_task_id"`
	ScheduledLate   bool             `json:"scheduled_late"`
	ParentChatID    *string          `json:"parent_chat_id"`
}

type chatCapabilities struct {
	Thinking thinkingCapability `json:"thinking"`
}

type thinkingCapability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason"`
}

type ChatMessage struct {
	ID          string           `json:"id"`
	ChatID      string           `json:"chat_id"`
	Role        string           `json:"role"`
	Content     string           `json:"content"`
	CreatedAt   string           `json:"created_at"`
	Attachments []ChatAttachment `json:"attachments"`
	Answered    bool             `json:"answered"`
	// The result preview itself loads on demand from the tool-result endpoint.
	ToolResultSize int     `json:"tool_result_size,omitempty"`
	ToolError      bool    `json:"tool_error,omitempty"`
	FromChatID     *string `json:"from_chat_id"`
	FromChatTitle  string  `json:"from_chat_title"`
}

type chatApproval struct {
	ID         string  `json:"id"`
	ChatID     string  `json:"chat_id"`
	ToolName   string  `json:"tool_name"`
	InputJSON  string  `json:"input_json"`
	Status     string  `json:"status"`
	Auto       bool    `json:"auto"`
	CreatedAt  string  `json:"created_at"`
	AnsweredAt *string `json:"answered_at"`
}

func cleanExistingDir(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path must be a directory")
	}
	return filepath.EvalSymlinks(path)
}

func (s *Server) listAllowedFolders(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(`SELECT id,path,created_at FROM allowed_folder ORDER BY created_at,rowid`)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []allowedFolder{}
	for rows.Next() {
		var f allowedFolder
		if err := rows.Scan(&f.ID, &f.Path, &f.CreatedAt); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if normalized, err := cleanExistingDir(f.Path); err == nil {
			f.Path = normalized
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, out)
}

func (s *Server) addAllowedFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if decode(r, &in) != nil {
		errw(w, 400, "valid absolute path required")
		return
	}
	path, err := cleanExistingDir(in.Path)
	if err != nil {
		errw(w, 400, err.Error())
		return
	}
	f := allowedFolder{ID: uid(), Path: path, CreatedAt: now()}
	if _, err := s.db.Exec(`INSERT INTO allowed_folder(id,path,created_at) VALUES(?,?,?)`, f.ID, f.Path, f.CreatedAt); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: allowed_folder.path") {
			errw(w, 409, "folder already allowed")
		} else {
			errw(w, 500, err.Error())
		}
		return
	}
	write(w, 201, f)
}

func (s *Server) deleteAllowedFolder(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.Exec(`DELETE FROM allowed_folder WHERE id=?`, idParam(r, "id"))
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		errw(w, 500, err.Error())
	} else if n == 0 {
		errw(w, 404, "folder not found")
	} else {
		noContent(w)
	}
}

func (s *Server) scanChat(row interface{ Scan(...any) error }) (chat, error) {
	c, kind, baseURL, connectorModel, err := s.scanChatRow(row)
	if err != nil {
		return c, err
	}
	if kind == "local" {
		model := c.Model
		if model == "" {
			model = connectorModel
		}
		secret, secretErr := keyring.Get(connectorService, connectorSecret(*c.ConnectorID))
		if secretErr != nil {
			secret = ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		d := &localRuntime{connectorID: *c.ConnectorID, model: model, baseURL: baseURL, ollamaURL: ollamaOrigin(baseURL), secret: secret, ctx: ctx, client: &http.Client{}}
		d.detectOllama()
		if d.ollama && !d.ollamaThinkingSupported(ctx) {
			c.Capabilities.Thinking = thinkingCapability{Reason: "This model doesn't support thinking"}
		}
		cancel()
	}
	return c, nil
}

func (s *Server) scanChatRow(row interface{ Scan(...any) error }) (chat, string, string, string, error) {
	var c chat
	if err := row.Scan(&c.ID, &c.ConnectorID, &c.Title, &c.Model, &c.Folder, &c.SessionID, &c.CreatedAt, &c.UpdatedAt, &c.Thinking, &c.ShowThinking, &c.AutoAllow, &c.ScheduledTaskID, &c.ScheduledLate, &c.ParentChatID); err != nil {
		return c, "", "", "", err
	}
	c.Workdir = s.chatWorkdir(c)
	c.Capabilities = chatCapabilities{Thinking: thinkingCapability{Supported: true}}
	var kind, baseURL, connectorModel string
	if c.ConnectorID != nil {
		if err := s.db.QueryRow(`SELECT kind,base_url,model FROM connector WHERE id=?`, *c.ConnectorID).Scan(&kind, &baseURL, &connectorModel); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return c, "", "", "", err
		}
		c.Capabilities = capabilitiesForKind(kind)
	}
	return c, kind, baseURL, connectorModel, nil
}

func capabilitiesForKind(kind string) chatCapabilities {
	capability := thinkingCapability{Supported: true}
	switch kind {
	case "opencode":
		capability = thinkingCapability{Reason: "opencode picks reasoning with model variants"}
	case "claude":
		capability.Reason = "Some models always think"
	}
	return chatCapabilities{Thinking: capability}
}

func (s *Server) chatWorkdir(c chat) string {
	if c.Folder != nil {
		return *c.Folder
	}
	return filepath.Join(s.dbDir, "chats", c.ID)
}

const chatColumns = `id,connector_id,title,model,folder,session_id,created_at,updated_at,thinking,show_thinking,auto_allow,scheduled_task_id,COALESCE((SELECT late FROM scheduled_task_run WHERE chat_id=chat.id),0),parent_chat_id`

func (s *Server) listChats(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			errw(w, 400, "invalid limit")
			return
		}
		limit = max(1, min(value, 1000))
	}
	query := `SELECT ` + chatColumns + ` FROM chat`
	args := []any{}
	if values, present := r.URL.Query()["cursor"]; present {
		cursor := values[0]
		updatedAt, id, ok := strings.Cut(cursor, "|")
		if !ok {
			errw(w, 400, "invalid cursor")
			return
		}
		var rowid int64
		err := s.db.QueryRow(`SELECT rowid FROM chat WHERE id=?`, id).Scan(&rowid)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			errw(w, 500, err.Error())
			return
		}
		if err == nil {
			query += ` WHERE updated_at < ? OR (updated_at = ? AND rowid < ?)`
			args = append(args, updatedAt, updatedAt, rowid)
		} else {
			query += ` WHERE updated_at <= ?`
			args = append(args, updatedAt)
		}
	}
	query += ` ORDER BY updated_at DESC,rowid DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []chat{}
	for rows.Next() {
		c, _, _, _, err := s.scanChatRow(rows)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		c.Running = s.chats.Running(c.ID)
		if err := s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM chat_approval WHERE chat_id=? AND status='pending'), EXISTS(SELECT 1 FROM chat_turn t JOIN app_window w ON w.id=t.window_id WHERE t.chat_id=? AND t.window_id<>? AND w.heartbeat_at>=?)`, c.ID, c.ID, s.windowID, nowMinusWindowStale()).Scan(&c.PendingCount, &c.OtherWindow); err != nil {
			errw(w, 500, err.Error())
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		write(w, 200, map[string]any{"chats": out, "next_cursor": last.UpdatedAt + "|" + last.ID})
		return
	}
	write(w, 200, map[string]any{"chats": out, "next_cursor": nil})
}

func (s *Server) createChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConnectorID  json.RawMessage `json:"connector_id"`
		Title        string          `json:"title"`
		Model        string          `json:"model"`
		Folder       json.RawMessage `json:"folder"`
		AutoAllow    *bool           `json:"auto_allow"`
		Thinking     *bool           `json:"thinking"`
		ShowThinking *bool           `json:"show_thinking"`
	}
	if decode(r, &in) != nil {
		errw(w, 400, "valid chat required")
		return
	}
	defaults, err := s.chatDefaults()
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	var connectorID string
	if len(in.ConnectorID) > 0 {
		if err := json.Unmarshal(in.ConnectorID, &connectorID); err != nil || !required(connectorID) {
			errw(w, 400, "connector_id required")
			return
		}
	} else if defaults.ConnectorID != nil {
		connectorID = *defaults.ConnectorID
	} else if err := s.db.QueryRow(`SELECT id FROM connector ORDER BY created_at,rowid LIMIT 1`).Scan(&connectorID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			errw(w, 400, "connector_id required")
		} else {
			errw(w, 500, err.Error())
		}
		return
	}
	folder := defaults.Folder
	if len(in.Folder) > 0 {
		if string(in.Folder) == "null" {
			folder = nil
		} else {
			var value string
			if err := json.Unmarshal(in.Folder, &value); err != nil {
				errw(w, 400, "folder must be an allowed folder or null")
				return
			}
			folder = &value
		}
	}
	autoAllow, thinking, showThinking := defaults.AutoAllow, defaults.Thinking, defaults.ShowThinking
	if in.AutoAllow != nil {
		autoAllow = *in.AutoAllow
	}
	if in.Thinking != nil {
		thinking = *in.Thinking
	}
	if in.ShowThinking != nil {
		showThinking = *in.ShowThinking
	}
	var kind string
	if err := s.db.QueryRow(`SELECT kind FROM connector WHERE id=?`, connectorID).Scan(&kind); errors.Is(err, sql.ErrNoRows) {
		errw(w, 400, "connector not found")
		return
	} else if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if in.Title == "" {
		in.Title = "New chat"
	}
	if in.Model == "" && defaults.ConnectorID != nil && connectorID == *defaults.ConnectorID {
		in.Model = defaults.Model
	}
	if !required(in.Title) {
		errw(w, 400, "title required")
		return
	}
	if folder != nil {
		cleaned, allowed, err := s.allowedFolderPath(*folder)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		if !allowed {
			errw(w, 400, "folder is not allowed")
			return
		}
		folder = &cleaned
	}
	id, created := uid(), now()
	c := chat{ID: id, ConnectorID: &connectorID, Title: in.Title, Model: in.Model, Folder: folder, CreatedAt: created, UpdatedAt: created, Capabilities: capabilitiesForKind(kind)}
	c.Thinking, c.ShowThinking, c.AutoAllow = thinking, showThinking, autoAllow
	if folder == nil {
		dir := s.chatWorkdir(c)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			errw(w, 500, cleanupConnectorDir(dir, err).Error())
			return
		}
	}
	if _, err := s.db.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at,thinking,show_thinking,auto_allow) VALUES(?,?,?,?,?,?,?,?,?,?)`, c.ID, c.ConnectorID, c.Title, c.Model, c.Folder, c.CreatedAt, c.UpdatedAt, c.Thinking, c.ShowThinking, c.AutoAllow); err != nil {
		if folder == nil {
			err = cleanupConnectorDir(s.chatWorkdir(c), err)
		}
		errw(w, 500, err.Error())
		return
	}
	c.Workdir = s.chatWorkdir(c)
	write(w, 201, c)
}

func (s *Server) getChatToolResult(w http.ResponseWriter, r *http.Request) {
	var result sql.NullString
	err := s.db.QueryRow(`SELECT tool_result FROM chat_message WHERE id=? AND chat_id=? AND role='tool'`, idParam(r, "messageID"), idParam(r, "id")).Scan(&result)
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "tool message not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, map[string]string{"tool_result": result.String})
}

func (s *Server) getChat(w http.ResponseWriter, r *http.Request) {
	c, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, idParam(r, "id")))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "chat not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	c.Running = s.chats.Running(c.ID)
	messages := []ChatMessage{}
	rows, err := s.db.Query(`SELECT m.id,m.chat_id,m.role,m.content,m.created_at,m.attachments,m.answered,m.tool_result_size,m.tool_error,m.from_chat_id,COALESCE(sender.title,'') FROM chat_message m LEFT JOIN chat sender ON sender.id=m.from_chat_id WHERE m.chat_id=? ORDER BY m.created_at,m.rowid`, c.ID)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	for rows.Next() {
		var m ChatMessage
		var rawAttachments string
		if err := rows.Scan(&m.ID, &m.ChatID, &m.Role, &m.Content, &m.CreatedAt, &rawAttachments, &m.Answered, &m.ToolResultSize, &m.ToolError, &m.FromChatID, &m.FromChatTitle); err == nil {
			if m.Attachments, err = decodeChatAttachments(rawAttachments); err != nil {
				_ = rows.Close()
				errw(w, 500, "invalid chat message attachments: "+err.Error())
				return
			}
		} else {
			if closeErr := rows.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
			errw(w, 500, err.Error())
			return
		}
		messages = append(messages, m)
	}
	rowsErr := rows.Err()
	if err := rows.Close(); err != nil {
		rowsErr = errors.Join(rowsErr, err)
	}
	if rowsErr != nil {
		errw(w, 500, rowsErr.Error())
		return
	}
	approvals := []chatApproval{}
	rows, err = s.db.Query(`SELECT id,chat_id,tool_name,input_json,status,auto,created_at,answered_at FROM chat_approval WHERE chat_id=? AND status='pending' ORDER BY created_at,rowid`, c.ID)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	for rows.Next() {
		var a chatApproval
		if err := rows.Scan(&a.ID, &a.ChatID, &a.ToolName, &a.InputJSON, &a.Status, &a.Auto, &a.CreatedAt, &a.AnsweredAt); err != nil {
			_ = rows.Close()
			errw(w, 500, err.Error())
			return
		}
		approvals = append(approvals, a)
	}
	rowsErr = rows.Err()
	if err := rows.Close(); err != nil {
		rowsErr = errors.Join(rowsErr, err)
	}
	if rowsErr != nil {
		errw(w, 500, rowsErr.Error())
		return
	}
	autoApprovals := []chatApproval{}
	rows, err = s.db.Query(`SELECT id,chat_id,tool_name,input_json,status,auto,created_at,answered_at FROM chat_approval WHERE chat_id=? AND auto=1 ORDER BY created_at,rowid`, c.ID)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	for rows.Next() {
		var a chatApproval
		if err := rows.Scan(&a.ID, &a.ChatID, &a.ToolName, &a.InputJSON, &a.Status, &a.Auto, &a.CreatedAt, &a.AnsweredAt); err != nil {
			_ = rows.Close()
			errw(w, 500, err.Error())
			return
		}
		autoApprovals = append(autoApprovals, a)
	}
	rowsErr = rows.Err()
	if err := rows.Close(); err != nil {
		rowsErr = errors.Join(rowsErr, err)
	}
	if rowsErr != nil {
		errw(w, 500, rowsErr.Error())
		return
	}
	children := []map[string]string{}
	var parent *map[string]string
	if c.ParentChatID != nil {
		var parentTitle string
		if err := s.db.QueryRow(`SELECT title FROM chat WHERE id=?`, *c.ParentChatID).Scan(&parentTitle); err != nil && !errors.Is(err, sql.ErrNoRows) {
			errw(w, 500, err.Error())
			return
		} else if err == nil {
			linked := map[string]string{"id": *c.ParentChatID, "title": parentTitle}
			parent = &linked
		}
	}
	childRows, err := s.db.Query(`SELECT id,title FROM chat WHERE parent_chat_id=? ORDER BY created_at,rowid`, c.ID)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	for childRows.Next() {
		var child map[string]string
		var id, title string
		if err := childRows.Scan(&id, &title); err != nil {
			childRows.Close()
			errw(w, 500, err.Error())
			return
		}
		child = map[string]string{"id": id, "title": title}
		children = append(children, child)
	}
	if err := childRows.Close(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	response := map[string]any{"chat": c, "messages": messages, "parent": parent, "children": children, "pending_approvals": approvals, "auto_approvals": autoApprovals}
	data, err := json.Marshal(response)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	digest := sha256.Sum256(data)
	version := hex.EncodeToString(digest[:8])
	if r.URL.Query().Get("version") == version && version != "" {
		write(w, 200, map[string]bool{"unchanged": true})
		return
	}
	response["version"] = version
	write(w, 200, response)
}

func (s *Server) revealChatFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if decode(r, &in) != nil || in.Path == "" {
		errw(w, http.StatusBadRequest, "path required")
		return
	}
	c, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, idParam(r, "id")))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "chat not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	path, err := revealPath(c.Workdir, in.Path)
	if errors.Is(err, os.ErrNotExist) {
		errw(w, http.StatusNotFound, "file not found")
		return
	}
	if errors.Is(err, errInvalidRevealPath) {
		errw(w, http.StatusBadRequest, errInvalidRevealPath.Error())
		return
	}
	if err != nil {
		if errors.Is(err, errPathOutsideWorkdir) {
			errw(w, http.StatusBadRequest, errPathOutsideWorkdir.Error())
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if err := s.revealFile(path); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	noContent(w)
}

var errPathOutsideWorkdir = errors.New("path is outside this chat's folder")
var errInvalidRevealPath = errors.New("valid path required")

func revealPath(workdir, path string) (string, error) {
	if strings.HasPrefix(strings.ToLower(path), "file://") {
		parsed, err := url.Parse(path)
		if err != nil || parsed.Scheme != "file" || parsed.Path == "" || parsed.Host != "" && parsed.Host != "localhost" {
			return "", errInvalidRevealPath
		}
		path = parsed.Path
		if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
		path = filepath.FromSlash(path)
	}
	workdir, err := filepath.Abs(workdir)
	if err != nil {
		return "", err
	}
	workdir, err = filepath.EvalSymlinks(filepath.Clean(workdir))
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workdir, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(workdir, path)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errPathOutsideWorkdir
	}
	return path, nil
}

func openInFileManager(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{path}
		if !info.IsDir() {
			args = []string{"-R", path}
		}
	case "windows":
		name, args = "explorer", []string{path}
		if !info.IsDir() {
			args = []string{"/select," + path}
		}
	case "linux":
		dir := path
		if !info.IsDir() {
			dir = filepath.Dir(path)
		}
		name, args = "xdg-open", []string{dir}
	default:
		return fmt.Errorf("unsupported operating system %q", runtime.GOOS)
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (s *Server) sendChatMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text        string   `json:"text"`
		Attachments []string `json:"attachments"`
	}
	if decode(r, &in) != nil {
		errw(w, http.StatusBadRequest, "valid text required")
		return
	}
	if len(in.Attachments) > maxChatAttachments {
		errw(w, http.StatusBadRequest, "at most 10 attachments are allowed")
		return
	}
	id := idParam(r, "id")
	c, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "chat not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.ConnectorID == nil {
		errw(w, http.StatusBadRequest, errChatConnectorMissing.Error())
		return
	}
	kind, dir, err := s.chats.connectorConfig(*c.ConnectorID)
	if errors.Is(err, errChatConnectorMissing) {
		errw(w, http.StatusBadRequest, errChatConnectorMissing.Error())
		return
	} else if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, ok := runtimes[kind]; !ok {
		errw(w, http.StatusBadRequest, errChatUnsupported.Error())
		return
	}
	s.attachmentsMu.Lock()
	defer s.attachmentsMu.Unlock()
	if strings.TrimSpace(in.Text) == "" && len(in.Attachments) == 0 {
		errw(w, http.StatusBadRequest, "text or attachments required")
		return
	}
	selected := make([]ChatAttachment, 0, len(in.Attachments))
	seen := make(map[string]bool, len(in.Attachments))
	for _, name := range in.Attachments {
		if seen[name] {
			errw(w, http.StatusBadRequest, "duplicate attachment")
			return
		}
		seen[name] = true
		attachment, ok := s.attachments[id][name]
		if !ok {
			errw(w, http.StatusBadRequest, "attachment is not an unsent upload")
			return
		}
		if _, err := s.attachmentPath(c, name); err != nil {
			s.writeAttachmentPathError(w, err)
			return
		}
		selected = append(selected, attachment)
	}
	message, err := s.chats.Send(c, kind, dir, ChatSendRequest{Text: in.Text, Attachments: selected})
	if message.ID != "" {
		if err := s.markChatAttachmentsSent(c, seen); err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if errors.Is(err, errChatTurnRunning) {
		s.queueChatMessage(w, id, in.Text, selected)
		return
	}
	if errors.Is(err, errChatTurnOtherWindow) {
		s.queueChatMessage(w, id, in.Text, selected)
		return
	}
	if err != nil {
		if errors.Is(err, errChatUnsupported) || errors.Is(err, errChatConnectorMissing) {
			errw(w, http.StatusBadRequest, err.Error())
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	write(w, http.StatusAccepted, message)
}

func (s *Server) queueChatMessage(w http.ResponseWriter, chatID, text string, attachments []ChatAttachment) {
	encoded, err := json.Marshal(attachments)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	item := chatQueueItem{ID: uid(), Text: text, Attachments: attachments, CreatedAt: now()}
	if _, err := s.db.Exec(`INSERT INTO chat_queue(id,chat_id,text,attachments,created_at) VALUES(?,?,?,?,?)`, item.ID, chatID, text, string(encoded), item.CreatedAt); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.chats.emit(chatID, map[string]any{"type": "queue"})
	write(w, http.StatusAccepted, map[string]any{"queued": item})
}

func (s *Server) stopChat(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM chat WHERE id=?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "chat not found")
		return
	} else if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.chats.Stop(id); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	noContent(w)
}

func (s *Server) updateChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title        *string         `json:"title"`
		Model        *string         `json:"model"`
		Thinking     *bool           `json:"thinking"`
		ShowThinking *bool           `json:"show_thinking"`
		AutoAllow    *bool           `json:"auto_allow"`
		Folder       json.RawMessage `json:"folder"`
	}
	if decode(r, &in) != nil || in.Title == nil && in.Model == nil && in.Thinking == nil && in.ShowThinking == nil && in.AutoAllow == nil && len(in.Folder) == 0 {
		errw(w, 400, "title, model, thinking, auto_allow, or folder required")
		return
	}
	if in.Title != nil && !required(*in.Title) {
		errw(w, 400, "title required")
		return
	}
	var folderIn *string
	if len(in.Folder) > 0 && string(in.Folder) != "null" {
		if err := json.Unmarshal(in.Folder, &folderIn); err != nil {
			errw(w, 400, "folder must be an allowed folder or null")
			return
		}
		cleaned, allowed, err := s.allowedFolderPath(*folderIn)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		if !allowed {
			errw(w, 400, "folder is not allowed")
			return
		}
		folderIn = &cleaned
	}
	if len(in.Folder) > 0 {
		// Uploads and sends hold this lock, so the message count and
		// pending attachments can't change while the folder moves.
		s.attachmentsMu.Lock()
		defer s.attachmentsMu.Unlock()
	}
	id := idParam(r, "id")
	c, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "chat not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	title, model, thinking, showThinking, autoAllow, folder := c.Title, c.Model, c.Thinking, c.ShowThinking, c.AutoAllow, c.Folder
	folderChanged := len(in.Folder) > 0 && (folderIn == nil) != (c.Folder == nil) || folderIn != nil && c.Folder != nil && *folderIn != *c.Folder
	var moved []movedFile
	if folderChanged {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_message WHERE chat_id=?`, id).Scan(&count); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if count > 0 {
			errw(w, http.StatusConflict, errChatFolderLocked.Error())
			return
		}
		next := c
		next.Folder = folderIn
		next.Workdir = s.chatWorkdir(next)
		if folderIn == nil {
			if err := os.MkdirAll(next.Workdir, 0o700); err != nil {
				errw(w, 500, err.Error())
				return
			}
		}
		if moved, err = s.movePendingAttachments(c, next.Workdir); err != nil {
			if errors.Is(err, os.ErrExist) {
				errw(w, http.StatusConflict, err.Error())
			} else {
				errw(w, 500, err.Error())
			}
			return
		}
		folder = folderIn
	}
	if in.Title != nil {
		title = *in.Title
	}
	if in.Model != nil {
		model = *in.Model
	}
	if in.Thinking != nil {
		thinking = *in.Thinking
	}
	if in.ShowThinking != nil {
		showThinking = *in.ShowThinking
	}
	if in.AutoAllow != nil {
		autoAllow = *in.AutoAllow
	}
	updated := now()
	writeUpdate := func(exec interface {
		Exec(string, ...any) (sql.Result, error)
	}) error {
		res, err := exec.Exec(`UPDATE chat SET title=?,model=?,thinking=?,show_thinking=?,auto_allow=?,session_id=CASE WHEN folder IS ? THEN session_id ELSE NULL END,folder=?,updated_at=? WHERE id=?`, title, model, thinking, showThinking, autoAllow, folder, folder, updated, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	}
	committed := false
	update := func() error {
		var err error
		if in.AutoAllow == nil {
			err = writeUpdate(s.db)
		} else {
			err = txrun(s.db, func(tx *sql.Tx) error {
				if err := writeUpdate(tx); err != nil {
					return err
				}
				if autoAllow {
					_, err := tx.Exec(`UPDATE chat_approval SET status='allowed',auto=1,answered_at=? WHERE chat_id=? AND status='pending'`, updated, id)
					return err
				}
				return nil
			})
		}
		committed = err == nil
		return err
	}
	if folderChanged {
		err = s.chats.ChangeFolder(id, update)
	} else if in.Model != nil && model != c.Model {
		err = s.chats.ChangeModel(id, update)
	} else if in.Thinking != nil && thinking != c.Thinking {
		err = s.chats.UpdateChat(id, update)
	} else {
		err = update()
	}
	if err != nil && !committed {
		err = errors.Join(err, restoreMovedFiles(moved))
	}
	if errors.Is(err, errChatModelChangeBusy) || errors.Is(err, errChatThinkingChangeBusy) || errors.Is(err, errChatFolderChangeBusy) {
		errw(w, http.StatusConflict, err.Error())
	} else if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "chat not found")
	} else if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
	} else {
		c.Title, c.Model, c.Thinking, c.AutoAllow, c.UpdatedAt = title, model, thinking, autoAllow, updated
		if folderChanged {
			c.Folder, c.SessionID = folder, nil
			c.Workdir = s.chatWorkdir(c)
		}
		write(w, http.StatusOK, c)
	}
}

func (s *Server) deleteChat(w http.ResponseWriter, r *http.Request) {
	err := s.removeChat(idParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "chat not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	noContent(w)
}

func (s *Server) removeChat(id string) error {
	s.attachmentsMu.Lock()
	defer s.attachmentsMu.Unlock()
	res, err := s.db.Exec(`DELETE FROM chat WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return sql.ErrNoRows
	}
	if err := s.chats.CloseChat(id); err != nil {
		return err
	}
	delete(s.attachments, id)
	// A chat may have left its scratch folder for a user folder, so always clean it up.
	dir := filepath.Join(s.dbDir, "chats", id)
	if insideConnectorDir(filepath.Join(s.dbDir, "chats"), dir) {
		return os.RemoveAll(dir)
	}
	return nil
}

func (s *Server) answerChatApproval(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Allow *bool `json:"allow"`
	}
	if decode(r, &in) != nil || in.Allow == nil {
		errw(w, 400, "allow required")
		return
	}
	status := "denied"
	if *in.Allow {
		status = "allowed"
	}
	res, err := s.db.Exec(`UPDATE chat_approval SET status=?,answered_at=? WHERE id=? AND chat_id=? AND status='pending'`, status, now(), idParam(r, "approval_id"), idParam(r, "id"))
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if n == 1 {
		write(w, 200, map[string]string{"status": status})
		return
	}
	var exists int
	err = s.db.QueryRow(`SELECT 1 FROM chat_approval WHERE id=? AND chat_id=?`, idParam(r, "approval_id"), idParam(r, "id")).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "approval not found")
	} else if err != nil {
		errw(w, 500, err.Error())
	} else {
		errw(w, 409, "approval already answered")
	}
}

func (s *Server) answerChatQuestion(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Answers []struct {
			Header   string   `json:"header"`
			Selected []string `json:"selected"`
			Other    string   `json:"other,omitempty"`
		} `json:"answers"`
	}
	if decode(r, &in) != nil || len(in.Answers) == 0 {
		errw(w, 400, "answers required")
		return
	}
	id, messageID := idParam(r, "id"), idParam(r, "messageID")
	var answered bool
	var content string
	err := s.db.QueryRow(`SELECT answered,content FROM chat_message WHERE id=? AND chat_id=? AND role='question'`, messageID, id).Scan(&answered, &content)
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "question not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if answered {
		errw(w, 409, "question already answered")
		return
	}
	if s.chats.Running(id) {
		errw(w, 409, errChatTurnRunning.Error())
		return
	}
	var questions struct {
		Questions []struct {
			Header   string `json:"header"`
			Multiple bool   `json:"multiple"`
			Options  []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(content), &questions); err != nil {
		errw(w, 500, "invalid stored question")
		return
	}
	var lines []string
	for _, answer := range in.Answers {
		var matched *struct {
			Header   string `json:"header"`
			Multiple bool   `json:"multiple"`
			Options  []struct {
				Label string `json:"label"`
			} `json:"options"`
		}
		for i := range questions.Questions {
			if questions.Questions[i].Header == answer.Header {
				matched = &questions.Questions[i]
				break
			}
		}
		if matched == nil {
			errw(w, 400, "unknown question header")
			return
		}
		if !matched.Multiple && len(answer.Selected) > 1 {
			errw(w, 400, "question allows one selection")
			return
		}
		for _, selected := range answer.Selected {
			valid := false
			for _, option := range matched.Options {
				if option.Label == selected {
					valid = true
					break
				}
			}
			if !valid {
				errw(w, 400, "unknown selected option")
				return
			}
		}
		value := strings.Join(answer.Selected, ", ")
		if answer.Other != "" {
			if value != "" {
				value += ", "
			}
			value += "Other — " + answer.Other
		}
		if value == "" {
			errw(w, 400, "answer selection required")
			return
		}
		lines = append(lines, answer.Header+": "+value)
	}
	c, err := s.scanChat(s.db.QueryRow(`SELECT `+chatColumns+` FROM chat WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "chat not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if c.ConnectorID == nil {
		errw(w, 400, errChatConnectorMissing.Error())
		return
	}
	kind, dir, err := s.chats.connectorConfig(*c.ConnectorID)
	if err != nil {
		errw(w, 400, err.Error())
		return
	}
	message, err := s.chats.Send(c, kind, dir, ChatSendRequest{Text: strings.Join(lines, "\n")})
	if errors.Is(err, errChatTurnRunning) || errors.Is(err, errChatTurnOtherWindow) {
		errw(w, 409, err.Error())
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, http.StatusAccepted, message)
}

func ChatExists(database *sql.DB, id string) (bool, error) {
	var exists int
	err := database.QueryRow(`SELECT 1 FROM chat WHERE id=?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func CreateChatApproval(database *sql.DB, chatID, toolName string, input json.RawMessage) (string, bool, error) {
	id := uid()
	created := now()
	var auto bool
	err := database.QueryRow(`INSERT INTO chat_approval(id,chat_id,tool_name,input_json,status,created_at,answered_at,auto)
		SELECT ?,?,?,?,CASE WHEN auto_allow=1 THEN 'allowed' ELSE 'pending' END,?,CASE WHEN auto_allow=1 THEN ? ELSE NULL END,auto_allow
		FROM chat WHERE id=? RETURNING auto`, id, chatID, toolName, string(input), created, created, chatID).Scan(&auto)
	return id, auto, err
}

func ChatApprovalStatus(database *sql.DB, chatID, approvalID string) (string, error) {
	var status string
	err := database.QueryRow(`SELECT status FROM chat_approval WHERE id=? AND chat_id=?`, approvalID, chatID).Scan(&status)
	return status, err
}

func ExpireChatApproval(database *sql.DB, chatID, approvalID string) (string, error) {
	_, err := database.Exec(`UPDATE chat_approval SET status='expired',answered_at=? WHERE id=? AND chat_id=? AND status='pending'`, now(), approvalID, chatID)
	if err != nil {
		return "", err
	}
	return ChatApprovalStatus(database, chatID, approvalID)
}

func InsertChatMessage(database *sql.DB, chatID, role, content string) (ChatMessage, error) {
	return InsertChatMessageWithAttachments(database, chatID, role, content, nil)
}

func InsertChatMessageWithAttachments(database *sql.DB, chatID, role, content string, attachments []ChatAttachment) (ChatMessage, error) {
	return insertChatMessage(database, chatID, role, content, attachments, nil, nil)
}

func insertChatMessage(database *sql.DB, chatID, role, content string, attachments []ChatAttachment, fromChatID, reportToChatID *string) (ChatMessage, error) {
	if role != "user" && role != "assistant" && role != "tool" && role != "error" && role != "thinking" && role != "question" {
		return ChatMessage{}, errors.New("invalid chat message role")
	}
	if role != "user" && len(attachments) != 0 {
		return ChatMessage{}, errors.New("only user messages can have attachments")
	}
	if attachments == nil {
		attachments = []ChatAttachment{}
	}
	encoded, err := json.Marshal(attachments)
	if err != nil {
		return ChatMessage{}, err
	}
	m := ChatMessage{ID: uid(), ChatID: chatID, Role: role, Content: content, CreatedAt: now(), Attachments: attachments, FromChatID: fromChatID}
	err = txrun(database, func(tx *sql.Tx) error {
		if role == "user" {
			if _, err := tx.Exec(`UPDATE chat_message SET answered=1 WHERE chat_id=? AND role='question' AND answered=0`, chatID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at,attachments,from_chat_id,report_to_chat_id) VALUES(?,?,?,?,?,?,?,?)`, m.ID, m.ChatID, m.Role, m.Content, m.CreatedAt, string(encoded), fromChatID, reportToChatID); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE chat SET updated_at=? WHERE id=?`, m.CreatedAt, chatID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	return m, err
}

func InsertChatToolMessage(database *sql.DB, chatID, summary, result string, isError bool) (ChatMessage, error) {
	m := ChatMessage{ID: uid(), ChatID: chatID, Role: "tool", Content: summary, CreatedAt: now(), Attachments: []ChatAttachment{}, ToolResultSize: utf8.RuneCountInString(result), ToolError: isError}
	err := txrun(database, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO chat_message(id,chat_id,role,content,tool_result,tool_result_size,tool_error,created_at) VALUES(?,?,?,?,?,?,?,?)`, m.ID, m.ChatID, m.Role, m.Content, capStoredToolResult(result), m.ToolResultSize, m.ToolError, m.CreatedAt); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE chat SET updated_at=? WHERE id=?`, m.CreatedAt, chatID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	return m, err
}

func SetChatSessionID(database *sql.DB, chatID, sessionID string) error {
	res, err := database.Exec(`UPDATE chat SET session_id=? WHERE id=?`, sessionID, chatID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
