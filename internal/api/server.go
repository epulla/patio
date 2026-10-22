package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	db              *sql.DB
	userID          string
	showHidden      bool
	version         string
	dbDir           string
	dbPath          string
	logPath         string
	chats           *chatManager
	revealFile      func(string) error
	openOAuthURL    func(string) error
	notify          func(string, string, string) error
	oauthMu         sync.Mutex
	oauthLogins     map[string]*mcpOAuthLogin
	handler         http.Handler
	mcpCheckTimeout time.Duration
	mcpConnect      mcpConnector
	attachmentsMu   sync.Mutex
	attachments     map[string]map[string]ChatAttachment
	chatWindow      string
	openPath        string
	windowID        string
	startWindow     func([]string) error
	focusWindow     func()
}

type Options struct {
	DBPath  string
	LogPath string
	Emit    func(string, any)
	Notify  func(title, body, link string) error

	processFactory  chatProcessFactory
	commandBuilder  chatCommandBuilder
	findClaude      func() (string, error)
	findCodex       func() (string, error)
	codexStart      func(context.Context, string, []string, bool, string, string, ...string) (codexClient, error)
	version         string
	findCopilot     func() (string, error)
	mcpExecutable   string
	opencodeStarter opencodeProcessStarter
	findOpencode    func() (string, error)
	opencodeMajor   func(context.Context, string, []string) (int, error)
	mcpCheckTimeout time.Duration
	mcpConnect      mcpConnector
	openOAuthURL    func(string) error
	ChatWindow      string
	OpenPath        string
	WindowID        string
	StartWindow     func([]string) error
	FocusWindow     func()
}

func New(db *sql.DB, userID string, showHidden bool, version, dbDir string, options ...Options) *Server {
	var opts Options
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.mcpCheckTimeout <= 0 {
		opts.mcpCheckTimeout = 15 * time.Second
	}
	if opts.mcpConnect == nil {
		opts.mcpConnect = connectMCP
	}
	dbPath := opts.DBPath
	actualDBPath := dbPath != ""
	if dbPath == "" {
		var seq int
		var name, file string
		if err := db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &file); err == nil && file != "" {
			dbPath = file
			actualDBPath = true
		}
	}
	if dbPath == "" {
		dbPath = filepath.Join(dbDir, "patio.db")
	}
	if actualDBPath {
		dbDir = filepath.Dir(dbPath)
	}
	s := &Server{db: db, userID: userID, showHidden: showHidden, version: version, dbDir: filepath.Clean(dbDir), dbPath: filepath.Clean(dbPath), logPath: opts.LogPath, revealFile: openInFileManager, mcpCheckTimeout: opts.mcpCheckTimeout, mcpConnect: opts.mcpConnect, openOAuthURL: opts.openOAuthURL, notify: opts.Notify, oauthLogins: make(map[string]*mcpOAuthLogin), attachments: make(map[string]map[string]ChatAttachment), chatWindow: opts.ChatWindow, openPath: opts.OpenPath, windowID: opts.WindowID, startWindow: opts.StartWindow, focusWindow: opts.FocusWindow}
	if s.openOAuthURL == nil {
		s.openOAuthURL = openLoginURL
	}
	opts.version = version
	s.chats = newChatManager(db, opts)
	s.chats.notify = s.Notify
	s.chats.notifyMCPSignIn = s.notifyMCPSignIn
	s.chats.dbDir = s.dbDir
	s.chats.enabledMCPServers = s.enabledMCPServers
	m := http.NewServeMux()
	m.HandleFunc("GET /api/config", s.config)
	m.HandleFunc("GET /api/notifications", s.listNotifications)
	m.HandleFunc("POST /api/notifications", s.createNotification)
	m.HandleFunc("GET /api/history", s.listItemHistory)
	m.HandleFunc("POST /api/history/{id}/restore", s.restoreItemHistory)
	m.HandleFunc("POST /api/notifications/{id}/read", s.readNotification)
	m.HandleFunc("POST /api/notifications/read-all", s.readAllNotifications)
	m.HandleFunc("DELETE /api/notifications/{id}", s.deleteNotification)
	m.HandleFunc("POST /api/notifications/{id}/native", s.nativeNotification)
	m.HandleFunc("GET /api/connectors", s.listConnectors)
	m.HandleFunc("GET /api/connectors/{id}/status", s.connectorStatus)
	m.HandleFunc("GET /api/connectors/{id}/quota", s.connectorQuota)
	m.HandleFunc("GET /api/connectors/{id}/models", s.connectorModels)
	m.HandleFunc("POST /api/connectors/local-models", s.localModels)
	m.HandleFunc("POST /api/connectors/{id}/login", s.connectorLogin)
	m.HandleFunc("POST /api/connectors", s.tracked("connector", s.createConnector))
	m.HandleFunc("PUT /api/connectors/{id}", s.tracked("connector", s.updateConnector))
	m.HandleFunc("DELETE /api/connectors/{id}", s.tracked("connector", s.deleteConnector))
	m.HandleFunc("GET /api/mcp-servers", s.listMCPServers)
	m.HandleFunc("GET /api/mcp-servers/{id}/check", s.checkMCPServer)
	m.HandleFunc("POST /api/mcp-servers/{id}/login", s.mcpServerLogin)
	m.HandleFunc("POST /api/mcp-servers", s.tracked("mcp_server", s.createMCPServer))
	m.HandleFunc("PUT /api/mcp-servers/{id}", s.tracked("mcp_server", s.updateMCPServer))
	m.HandleFunc("DELETE /api/mcp-servers/{id}", s.tracked("mcp_server", s.deleteMCPServer))
	m.HandleFunc("GET /api/skills", s.listSkills)
	m.HandleFunc("GET /api/skills/{id}", s.getSkill)
	m.HandleFunc("POST /api/skills", s.tracked("skill", s.createSkill))
	m.HandleFunc("PUT /api/skills/{id}", s.tracked("skill", s.updateSkill))
	m.HandleFunc("DELETE /api/skills/{id}", s.tracked("skill", s.deleteSkill))
	m.HandleFunc("POST /api/skills/import", s.tracked("skill", s.importSkill))
	m.HandleFunc("GET /api/skills/{id}/export", s.exportSkill)
	m.HandleFunc("GET /api/settings/folders", s.listAllowedFolders)
	m.HandleFunc("GET /api/settings/folders/browse", s.browseFolders)
	m.HandleFunc("POST /api/settings/folders", s.addAllowedFolder)
	m.HandleFunc("DELETE /api/settings/folders/{id}", s.deleteAllowedFolder)
	m.HandleFunc("GET /api/scheduled-tasks", s.listScheduledTasks)
	m.HandleFunc("GET /api/scheduled-tasks/{id}", s.getScheduledTask)
	m.HandleFunc("POST /api/scheduled-tasks", s.tracked("scheduled_task", s.createScheduledTask))
	m.HandleFunc("PUT /api/scheduled-tasks/{id}", s.tracked("scheduled_task", s.updateScheduledTask))
	m.HandleFunc("DELETE /api/scheduled-tasks/{id}", s.tracked("scheduled_task", s.deleteScheduledTask))
	m.HandleFunc("GET /api/monitors", s.listMonitors)
	m.HandleFunc("POST /api/monitors", s.createMonitor)
	m.HandleFunc("GET /api/monitors/{id}", s.getMonitor)
	m.HandleFunc("PUT /api/monitors/{id}", s.updateMonitor)
	m.HandleFunc("DELETE /api/monitors/{id}", s.deleteMonitor)
	m.HandleFunc("GET /api/monitors/{id}/runs", s.monitorRuns)
	m.HandleFunc("POST /api/monitors/{id}/run", s.runMonitorNow)
	m.HandleFunc("POST /api/monitors/test", s.testMonitor)
	m.HandleFunc("POST /api/scheduled-tasks/preview", s.previewScheduledTask)
	m.HandleFunc("GET /api/settings/defaults", s.getSettingsDefaults)
	m.HandleFunc("PUT /api/settings/defaults", s.putSettingsDefaults)
	m.HandleFunc("GET /api/settings/chat-cleanup", s.getChatCleanup)
	m.HandleFunc("PUT /api/settings/chat-cleanup", s.putChatCleanup)
	m.HandleFunc("POST /api/chats/cleanup", s.runChatCleanup)
	m.HandleFunc("GET /api/settings/about", s.settingsAbout)
	m.HandleFunc("POST /api/settings/reveal", s.revealSettingsTarget)
	m.HandleFunc("GET /api/chats", s.listChats)
	m.HandleFunc("POST /api/chats", s.createChat)
	m.HandleFunc("GET /api/chats/{id}", s.getChat)
	m.HandleFunc("PUT /api/chats/{id}", s.updateChat)
	m.HandleFunc("DELETE /api/chats/{id}", s.deleteChat)
	m.HandleFunc("POST /api/chats/{id}/approvals/{approval_id}", s.answerChatApproval)
	m.HandleFunc("POST /api/chats/{id}/questions/{messageID}/answer", s.answerChatQuestion)
	m.HandleFunc("POST /api/chats/{id}/reveal", s.revealChatFile)
	m.HandleFunc("POST /api/chats/{id}/attachments", s.uploadChatAttachments)
	m.HandleFunc("GET /api/chats/{id}/attachments/{name}", s.getChatAttachment)
	m.HandleFunc("DELETE /api/chats/{id}/attachments/{name}", s.deleteChatAttachment)
	m.HandleFunc("POST /api/chats/{id}/attachments/{name}/reveal", s.revealChatAttachment)
	m.HandleFunc("POST /api/chats/{id}/window", s.openChatWindow)
	m.HandleFunc("POST /api/windows/main", s.openMainWindow)
	m.HandleFunc("POST /api/chats/{id}/messages", s.sendChatMessage)
	m.HandleFunc("GET /api/chats/{id}/queue", s.listChatQueue)
	m.HandleFunc("DELETE /api/chats/{id}/queue/{qid}", s.deleteChatQueue)
	m.HandleFunc("POST /api/chats/{id}/queue/send", s.sendQueuedChatMessages)
	m.HandleFunc("GET /api/chats/{id}/messages/{messageID}/tool-result", s.getChatToolResult)
	m.HandleFunc("POST /api/chats/{id}/stop", s.stopChat)
	m.HandleFunc("GET /api/users", s.users)
	m.HandleFunc("GET /api/boards", s.boards)
	m.HandleFunc("PUT /api/board-order", s.boardOrder)
	m.HandleFunc("POST /api/boards", s.createBoard)
	m.HandleFunc("GET /api/boards/{id}", s.board)
	m.HandleFunc("PUT /api/boards/{id}", s.updateBoard)
	m.HandleFunc("DELETE /api/boards/{id}", s.deleteBoard)
	m.HandleFunc("GET /api/boards/{id}/data", s.boardData)
	m.HandleFunc("POST /api/boards/{id}/statuses", s.createStatus)
	m.HandleFunc("PUT /api/statuses/{id}", s.updateStatus)
	m.HandleFunc("DELETE /api/statuses/{id}", s.deleteStatus)
	m.HandleFunc("PUT /api/boards/{id}/status-order", s.statusOrder)
	m.HandleFunc("POST /api/boards/{id}/ticket-types", s.createType)
	m.HandleFunc("PUT /api/ticket-types/{id}", s.updateType)
	m.HandleFunc("DELETE /api/ticket-types/{id}", s.deleteType)
	m.HandleFunc("POST /api/boards/{id}/tickets", s.createTicket)
	m.HandleFunc("GET /api/tickets/search", s.searchTickets)
	m.HandleFunc("GET /api/tickets/{idOrCode}", s.getTicket)
	m.HandleFunc("GET /api/tickets/{id}/links", s.ticketLinks)
	m.HandleFunc("POST /api/tickets/{id}/links", s.addTicketLink)
	m.HandleFunc("DELETE /api/ticket-links/{id}", s.deleteTicketLink)
	m.HandleFunc("PUT /api/tickets/{id}", s.updateTicket)
	m.HandleFunc("GET /api/calendar", s.calendar)
	m.HandleFunc("POST /api/calendar-items", s.createCalendarItem)
	m.HandleFunc("PUT /api/calendar-items/{id}", s.updateCalendarItem)
	m.HandleFunc("DELETE /api/calendar-items/{id}", s.deleteCalendarItem)
	m.HandleFunc("DELETE /api/tickets/{id}", s.deleteTicket)
	m.HandleFunc("PUT /api/tickets/{id}/move", s.moveTicket)
	m.HandleFunc("GET /api/tickets/{id}/history", s.history)
	m.HandleFunc("GET /api/tickets/{id}/transitions", s.transitions)
	m.HandleFunc("GET /api/tickets/{id}/comments", s.comments)
	m.HandleFunc("POST /api/tickets/{id}/comments", s.addComment)
	m.HandleFunc("GET /api/boards/{id}/export", s.exportBoard)
	m.HandleFunc("GET /api/tickets/{id}/export", s.exportTicket)
	m.HandleFunc("POST /api/import", s.importBoard)
	s.handler = m
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

func (s *Server) Close() error { return s.chats.Close() }

var _ http.Handler = (*Server)(nil)

type board struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Short    string `json:"short_name"`
	Template string `json:"template"`
	Created  string `json:"created_at"`
	Updated  string `json:"updated_at"`
}
type status struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	BoardID       string `json:"board_id"`
	HideAfterDays int    `json:"hide_after_days"`
}

func statusFields(raw string) (map[string]json.RawMessage, int, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		return nil, 0, errors.New("invalid status additional_fields")
	}
	days := 1
	if value, ok := fields["hide_after_days"]; ok && json.Unmarshal(value, &days) != nil {
		return nil, 0, errors.New("invalid hide_after_days")
	}
	if days < 1 {
		return nil, 0, errors.New("hide_after_days must be at least 1")
	}
	return fields, days, nil
}

func setStatusHideAfterDays(fields map[string]json.RawMessage, days int) (string, error) {
	value, err := json.Marshal(days)
	if err != nil {
		return "", err
	}
	fields["hide_after_days"] = value
	encoded, err := json.Marshal(fields)
	return string(encoded), err
}

func scanStatus(row interface{ Scan(...any) error }) (status, error) {
	var st status
	var raw string
	if err := row.Scan(&st.ID, &st.Name, &st.Type, &st.BoardID, &raw); err != nil {
		return st, err
	}
	_, days, err := statusFields(raw)
	st.HideAfterDays = days
	return st, err
}

type ticketType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	BoardID string `json:"board_id"`
}
type ticket struct {
	ID, Title, Description                                                          string
	Number                                                                          int
	Code, Created, Updated                                                          string
	DueDate, StatusUpdated, StatusID, BoardID, TicketTypeID, ReporterID, AssigneeID *string
	BoardName, StatusName                                                           *string
}

func (t ticket) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID            string  `json:"id"`
		Title         string  `json:"title"`
		Description   string  `json:"description"`
		Number        int     `json:"number"`
		Code          string  `json:"code"`
		Created       string  `json:"created_at"`
		Updated       string  `json:"updated_at"`
		DueDate       *string `json:"due_date"`
		StatusUpdated *string `json:"status_updated_at"`
		StatusID      *string `json:"status_id"`
		BoardID       *string `json:"board_id"`
		TicketTypeID  *string `json:"ticket_type_id"`
		ReporterID    *string `json:"reporter_id"`
		AssigneeID    *string `json:"assignee_id"`
		BoardName     *string `json:"board_name,omitempty"`
		StatusName    *string `json:"status_name,omitempty"`
	}{t.ID, t.Title, t.Description, t.Number, t.Code, t.Created, t.Updated, t.DueDate, t.StatusUpdated, t.StatusID, t.BoardID, t.TicketTypeID, t.ReporterID, t.AssigneeID, t.BoardName, t.StatusName})
}

type history struct {
	ID, Content, Created, UserID, UserName string
	UserIsAI                               bool
}
type transition struct {
	ID                                               string
	FromID, FromName, ToID, ToName, UserID, UserName *string
	Moved                                            string
	UserIsAI                                         bool
}
type comment struct {
	ID, Content, Created, Updated, UserID, UserName string
	UserIsAI                                        bool
}
type user struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	LastName *string `json:"last_name"`
	Email    string  `json:"email"`
	IsAI     bool    `json:"is_ai"`
}

func (h history) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID       string `json:"id"`
		Content  string `json:"content"`
		Created  string `json:"created_at"`
		UserID   string `json:"user_id"`
		UserName string `json:"user_name"`
		UserIsAI bool   `json:"user_is_ai"`
	}{h.ID, h.Content, h.Created, h.UserID, h.UserName, h.UserIsAI})
}
func (c comment) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID       string `json:"id"`
		Content  string `json:"content"`
		Created  string `json:"created_at"`
		Updated  string `json:"updated_at"`
		UserID   string `json:"user_id"`
		UserName string `json:"user_name"`
		UserIsAI bool   `json:"user_is_ai"`
	}{c.ID, c.Content, c.Created, c.Updated, c.UserID, c.UserName, c.UserIsAI})
}
func (t transition) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID       string  `json:"id"`
		FromID   *string `json:"from_status_id"`
		FromName *string `json:"from_status_name"`
		ToID     *string `json:"to_status_id"`
		ToName   *string `json:"to_status_name"`
		UserID   *string `json:"user_id"`
		UserName *string `json:"user_name"`
		UserIsAI bool    `json:"user_is_ai"`
		Moved    string  `json:"moved_at"`
	}{t.ID, t.FromID, t.FromName, t.ToID, t.ToName, t.UserID, t.UserName, t.UserIsAI, t.Moved})
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }
func uid() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return hex.EncodeToString(b[:])
}
func write(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func errw(w http.ResponseWriter, code int, msg string) {
	write(w, code, map[string]string{"error": msg})
}
func noContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("invalid JSON")
	}
	return nil
}
func idParam(r *http.Request, n string) string { return r.PathValue(n) }
func txrun(db *sql.DB, f func(*sql.Tx) error) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	if e = f(tx); e != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("%w; rollback: %v", e, rollbackErr)
		}
		return e
	}
	return tx.Commit()
}
func required(s string) bool    { return strings.TrimSpace(s) != "" }
func shortName(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
func validShort(s string) bool {
	if len(s) < 1 || len(s) > 10 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func parseList(s string) ([]string, error) {
	var x []string
	if e := json.Unmarshal([]byte(s), &x); e != nil {
		return nil, e
	}
	return x, nil
}
func listJSON(x []string) string { b, _ := json.Marshal(x); return string(b) }
func contains(x []string, id string) bool {
	for _, v := range x {
		if v == id {
			return true
		}
	}
	return false
}
func remove(x []string, id string) []string {
	r := x[:0]
	for _, v := range x {
		if v != id {
			r = append(r, v)
		}
	}
	return r
}
