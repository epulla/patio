package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
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
	return path, nil
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
	if err := row.Scan(&c.ID, &c.ConnectorID, &c.Title, &c.Model, &c.Folder, &c.SessionID, &c.CreatedAt, &c.UpdatedAt); err != nil {
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

const chatColumns = `id,connector_id,title,model,folder,session_id,created_at,updated_at`

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
		ConnectorID string  `json:"connector_id"`
		Title       string  `json:"title"`
		Model       string  `json:"model"`
		Folder      *string `json:"folder"`
	}
	if decode(r, &in) != nil || !required(in.ConnectorID) {
		errw(w, 400, "connector_id required")
		return
	}
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM connector WHERE id=?`, in.ConnectorID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
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
	if in.Folder != nil {
		folder, err := cleanExistingDir(*in.Folder)
		if err != nil {
			errw(w, 400, err.Error())
			return
		}
		rows, err := s.db.Query(`SELECT path FROM allowed_folder`)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		allowed := false
		for rows.Next() {
			var root string
			if err := rows.Scan(&root); err != nil {
				if closeErr := rows.Close(); closeErr != nil {
					err = errors.Join(err, closeErr)
				}
				errw(w, 500, err.Error())
				return
			}
			rel, err := filepath.Rel(filepath.Clean(root), folder)
			if err != nil {
				if closeErr := rows.Close(); closeErr != nil {
					err = errors.Join(err, closeErr)
				}
				errw(w, 500, err.Error())
				return
			}
			if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				allowed = true
			}
		}
		rowsErr := rows.Err()
		if err := rows.Close(); err != nil {
			rowsErr = errors.Join(rowsErr, err)
		}
		if rowsErr != nil {
			errw(w, 500, rowsErr.Error())
			return
		}
		if !allowed {
			errw(w, 400, "folder is not allowed")
			return
		}
		in.Folder = &folder
	}
	id, created := uid(), now()
	c := chat{ID: id, ConnectorID: &in.ConnectorID, Title: in.Title, Model: in.Model, Folder: in.Folder, CreatedAt: created, UpdatedAt: created}
	if in.Folder == nil {
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
	if _, err := s.db.Exec(`INSERT INTO chat(id,connector_id,title,model,folder,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, c.ID, c.ConnectorID, c.Title, c.Model, c.Folder, c.CreatedAt, c.UpdatedAt); err != nil {
		if in.Folder == nil {
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
	rows, err = s.db.Query(`SELECT id,chat_id,tool_name,input_json,status,created_at,answered_at FROM chat_approval WHERE chat_id=? AND status='pending' ORDER BY created_at,rowid`, c.ID)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var a chatApproval
		if err := rows.Scan(&a.ID, &a.ChatID, &a.ToolName, &a.InputJSON, &a.Status, &a.CreatedAt, &a.AnsweredAt); err != nil {
			errw(w, 500, err.Error())
			return
		}
		approvals = append(approvals, a)
	}
	if err := rows.Err(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, map[string]any{"chat": c, "messages": messages, "pending_approvals": approvals})
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
	var kind string
	var configDir sql.NullString
	if err := s.db.QueryRow(`SELECT kind,config_dir FROM connector WHERE id=?`, *c.ConnectorID).Scan(&kind, &configDir); errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusBadRequest, errChatConnectorMissing.Error())
		return
	} else if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if kind != "claude" {
		errw(w, http.StatusBadRequest, errChatUnsupported.Error())
		return
	}
	dir := ""
	if configDir.Valid {
		dir = configDir.String
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
		Title *string `json:"title"`
		Model *string `json:"model"`
	}
	if decode(r, &in) != nil || in.Title == nil && in.Model == nil {
		errw(w, 400, "title or model required")
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
	title, model := c.Title, c.Model
	if in.Title != nil {
		title = *in.Title
	}
	if in.Model != nil {
		model = *in.Model
	}
	updated := now()
	update := func() error {
		res, err := s.db.Exec(`UPDATE chat SET title=?,model=?,updated_at=? WHERE id=?`, title, model, updated, id)
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
	if in.Model != nil && model != c.Model {
		err = s.chats.ChangeModel(id, update)
	} else {
		err = update()
	}
	if errors.Is(err, errChatModelChangeBusy) {
		errw(w, http.StatusConflict, err.Error())
	} else if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "chat not found")
	} else if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
	} else {
		c.Title, c.Model, c.UpdatedAt = title, model, updated
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

func CreateChatApproval(database *sql.DB, chatID, toolName string, input json.RawMessage) (string, error) {
	id := uid()
	_, err := database.Exec(`INSERT INTO chat_approval(id,chat_id,tool_name,input_json,created_at) VALUES(?,?,?,?,?)`, id, chatID, toolName, string(input), now())
	return id, err
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
