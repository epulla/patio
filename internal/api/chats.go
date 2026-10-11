package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type allowedFolder struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	CreatedAt string `json:"created_at"`
}

type chat struct {
	ID          string  `json:"id"`
	ConnectorID *string `json:"connector_id"`
	Title       string  `json:"title"`
	Model       string  `json:"model"`
	Folder      *string `json:"folder"`
	SessionID   *string `json:"session_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	Workdir     string  `json:"workdir"`
	Running     bool    `json:"running"`
	Thinking    bool    `json:"thinking"`
	Tools       bool    `json:"tools"`
	AutoAllow   bool    `json:"auto_allow"`
}

type ChatMessage struct {
	ID        string `json:"id"`
	ChatID    string `json:"chat_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
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
	var c chat
	if err := row.Scan(&c.ID, &c.ConnectorID, &c.Title, &c.Model, &c.Folder, &c.SessionID, &c.CreatedAt, &c.UpdatedAt, &c.Thinking, &c.Tools, &c.AutoAllow); err != nil {
		return c, err
	}
	c.Workdir = s.chatWorkdir(c)
	return c, nil
}

func (s *Server) chatWorkdir(c chat) string {
	if c.Folder != nil {
		return *c.Folder
	}
	return filepath.Join(s.dbDir, "chats", c.ID)
}

const chatColumns = `id,connector_id,title,model,folder,session_id,created_at,updated_at,thinking,tools,auto_allow`

func (s *Server) listChats(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(`SELECT ` + chatColumns + ` FROM chat ORDER BY updated_at DESC,rowid DESC`)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []chat{}
	for rows.Next() {
		c, err := s.scanChat(rows)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, out)
}

func (s *Server) createChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConnectorID json.RawMessage `json:"connector_id"`
		Title       string          `json:"title"`
		Model       string          `json:"model"`
		Folder      json.RawMessage `json:"folder"`
		AutoAllow   *bool           `json:"auto_allow"`
		Tools       *bool           `json:"tools"`
		Thinking    *bool           `json:"thinking"`
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
	autoAllow, tools, thinking := defaults.AutoAllow, defaults.Tools, defaults.Thinking
	if in.AutoAllow != nil {
		autoAllow = *in.AutoAllow
	}
	if in.Tools != nil {
		tools = *in.Tools
	}
	if in.Thinking != nil {
		thinking = *in.Thinking
	}
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM connector WHERE id=?`, connectorID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		errw(w, 400, "connector not found")
		return
	} else if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if in.Title == "" {
		in.Title = "New chat"
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
	c := chat{ID: id, ConnectorID: &connectorID, Title: in.Title, Model: in.Model, Folder: folder, CreatedAt: created, UpdatedAt: created}
	c.Tools, c.Thinking, c.AutoAllow = tools, thinking, autoAllow
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
	if _, err := s.db.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at,thinking,tools,auto_allow) VALUES(?,?,?,?,?,?,?,?,?,?)`, c.ID, c.ConnectorID, c.Title, c.Model, c.Folder, c.CreatedAt, c.UpdatedAt, c.Thinking, c.Tools, c.AutoAllow); err != nil {
		if folder == nil {
			err = cleanupConnectorDir(s.chatWorkdir(c), err)
		}
		errw(w, 500, err.Error())
		return
	}
	c.Workdir = s.chatWorkdir(c)
	write(w, 201, c)
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
	rows, err := s.db.Query(`SELECT id,chat_id,role,content,created_at FROM chat_message WHERE chat_id=? ORDER BY created_at,rowid`, c.ID)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.ChatID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
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
	write(w, 200, map[string]any{"chat": c, "messages": messages, "pending_approvals": approvals, "auto_approvals": autoApprovals})
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
		Text string `json:"text"`
	}
	if decode(r, &in) != nil {
		errw(w, http.StatusBadRequest, "valid text required")
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
	message, err := s.chats.Send(c, kind, dir, in.Text)
	if errors.Is(err, errChatTurnRunning) {
		errw(w, http.StatusConflict, err.Error())
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
		Title     *string `json:"title"`
		Model     *string `json:"model"`
		Thinking  *bool   `json:"thinking"`
		Tools     *bool   `json:"tools"`
		AutoAllow *bool   `json:"auto_allow"`
	}
	if decode(r, &in) != nil || in.Title == nil && in.Model == nil && in.Thinking == nil && in.Tools == nil && in.AutoAllow == nil {
		errw(w, 400, "title, model, thinking, tools, or auto_allow required")
		return
	}
	if in.Title != nil && !required(*in.Title) {
		errw(w, 400, "title required")
		return
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
	title, model, thinking, tools, autoAllow := c.Title, c.Model, c.Thinking, c.Tools, c.AutoAllow
	if in.Title != nil {
		title = *in.Title
	}
	if in.Model != nil {
		model = *in.Model
	}
	if in.Thinking != nil {
		thinking = *in.Thinking
	}
	if in.Tools != nil {
		tools = *in.Tools
	}
	if in.AutoAllow != nil {
		autoAllow = *in.AutoAllow
	}
	updated := now()
	writeUpdate := func(exec interface {
		Exec(string, ...any) (sql.Result, error)
	}) error {
		res, err := exec.Exec(`UPDATE chat SET title=?,model=?,thinking=?,tools=?,auto_allow=?,updated_at=? WHERE id=?`, title, model, thinking, tools, autoAllow, updated, id)
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
	update := func() error {
		if in.AutoAllow == nil {
			return writeUpdate(s.db)
		}
		return txrun(s.db, func(tx *sql.Tx) error {
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
	if in.Model != nil && model != c.Model {
		err = s.chats.ChangeModel(id, update)
	} else if (in.Thinking != nil && thinking != c.Thinking) || (in.Tools != nil && tools != c.Tools) {
		err = s.chats.UpdateChat(id, update)
	} else {
		err = update()
	}
	if errors.Is(err, errChatModelChangeBusy) || errors.Is(err, errChatThinkingChangeBusy) {
		errw(w, http.StatusConflict, err.Error())
	} else if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "chat not found")
	} else if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
	} else {
		c.Title, c.Model, c.Thinking, c.Tools, c.AutoAllow, c.UpdatedAt = title, model, thinking, tools, autoAllow, updated
		write(w, http.StatusOK, c)
	}
}

func (s *Server) deleteChat(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	var folder sql.NullString
	if err := s.db.QueryRow(`SELECT folder FROM chat WHERE id=?`, id).Scan(&folder); errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "chat not found")
		return
	} else if err != nil {
		errw(w, 500, err.Error())
		return
	}
	res, err := s.db.Exec(`DELETE FROM chat WHERE id=?`, id)
	if err == nil {
		var n int64
		n, err = res.RowsAffected()
		if err == nil && n == 0 {
			err = sql.ErrNoRows
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, 404, "chat not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if err := s.chats.CloseChat(id); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !folder.Valid {
		dir := filepath.Join(s.dbDir, "chats", id)
		if insideConnectorDir(filepath.Join(s.dbDir, "chats"), dir) {
			if err := os.RemoveAll(dir); err != nil {
				errw(w, 500, err.Error())
				return
			}
		}
	}
	noContent(w)
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
	if role != "user" && role != "assistant" && role != "tool" && role != "error" {
		return ChatMessage{}, errors.New("invalid chat message role")
	}
	m := ChatMessage{ID: uid(), ChatID: chatID, Role: role, Content: content, CreatedAt: now()}
	err := txrun(database, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at) VALUES(?,?,?,?,?)`, m.ID, m.ChatID, m.Role, m.Content, m.CreatedAt); err != nil {
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

func InsertChatToolMessage(database *sql.DB, chatID, summary, result string) (ChatMessage, error) {
	m := ChatMessage{ID: uid(), ChatID: chatID, Role: "tool", Content: summary, CreatedAt: now()}
	err := txrun(database, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO chat_message(id,chat_id,role,content,tool_result,created_at) VALUES(?,?,?,?,?,?)`, m.ID, m.ChatID, m.Role, m.Content, result, m.CreatedAt); err != nil {
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
