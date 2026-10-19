package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	connectorcli "patio/internal/connector"
	"patio/internal/proc"
)

const chatApprovalTimeout = 10 * time.Minute
const defaultChatIdleTimeout = 10 * time.Minute

type chatProcess interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Interrupt() error
	Kill() error
	Wait() error
}

type chatProcessFactory func(path string, args, env []string, cwd string) (chatProcess, error)
type startFunc func(*chatManager, chat, string, *chatRuntime) (chatRuntimeDriver, error)

type chatRuntimeDriver interface {
	send(request ChatSendRequest) error
	stop() error
	close() error
}

var runtimes = map[string]startFunc{}

type chatManager struct {
	db                   *sql.DB
	dbPath               string
	dbDir                string
	mcpExecutable        string
	enabledMCPServers    func() ([]mcpServerSpec, error)
	emitEvent            func(string, any)
	options              Options
	notify               func(string, string, string, string, bool) error
	approvalTimeout      time.Duration
	approvalPollInterval time.Duration
	// burnedout: assumed 10-minute idle timeout for all chat kinds
	idleTimeout time.Duration
	mu          sync.Mutex
	chats       map[string]*chatRuntime
	closed      bool
	windowID    string
}

func (m *chatManager) chatMCPServers(chatID string) ([]mcpServerSpec, error) {
	return m.chatMCPServersWithNotes(chatID, true)
}

func (m *chatManager) chatMCPServersWithNotes(chatID string, notes bool) ([]mcpServerSpec, error) {
	if m.enabledMCPServers == nil {
		return nil, nil
	}
	specs, err := m.enabledMCPServers()
	if err != nil {
		return nil, err
	}
	servers := make([]mcpServerSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.AuthError != nil || spec.Auth == "oauth" && spec.OAuthHeader == nil {
			if !notes {
				continue
			}
			message := "MCP server " + spec.Name + " skipped: sign in again"
			if _, err := InsertChatMessage(m.db, chatID, "error", message); err != nil {
				return nil, err
			}
			m.emit(chatID, map[string]any{"type": "error", "message": message})
			continue
		}
		servers = append(servers, spec)
	}
	return servers, nil
}

func mcpSpecHeaders(spec mcpServerSpec) map[string]string {
	headers := make(map[string]string, len(spec.Headers)+1)
	for name, value := range spec.Headers {
		headers[name] = value
	}
	if spec.OAuthHeader != nil {
		headers["Authorization"] = *spec.OAuthHeader
	}
	return headers
}

type chatRuntime struct {
	mu              sync.Mutex
	driver          chatRuntimeDriver
	busy            bool
	stopping        bool
	changingModel   bool
	stopDone        chan struct{}
	connectorID     string
	startedThinking *bool
	// streamMu guards the fields below. Drivers emit deltas while holding their own lock,
	// and Send holds mu while calling the driver, so these must not need mu.
	streamMu         sync.Mutex
	showThinking     bool
	pendingThinking  strings.Builder
	assistantHasText bool
	toolAfterText    bool
	lastActivity     time.Time
	idleTimer        *time.Timer
	mcpSpecs         []mcpServerSpec
}

func newChatManager(database *sql.DB, options Options) *chatManager {
	emit := options.Emit
	if emit == nil {
		emit = func(string, any) {}
	}
	return &chatManager{
		db: database, dbPath: options.DBPath, mcpExecutable: options.mcpExecutable, emitEvent: emit, options: options, windowID: options.WindowID,
		approvalTimeout: chatApprovalTimeout, approvalPollInterval: 300 * time.Millisecond,
		idleTimeout: defaultChatIdleTimeout,
		chats:       make(map[string]*chatRuntime),
	}
}

func (m *chatManager) runtime(id string) *chatRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.chats[id]
	if rt == nil {
		rt = &chatRuntime{}
		m.chats[id] = rt
	}
	return rt
}

func (m *chatManager) Running(id string) bool {
	m.mu.Lock()
	rt := m.chats[id]
	m.mu.Unlock()
	if rt == nil {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.busy
}

func (m *chatManager) Send(c chat, kind, configDir string, request ChatSendRequest) (ChatMessage, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ChatMessage{}, errChatManagerClosed
	}
	rt := m.chats[c.ID]
	if rt == nil {
		rt = &chatRuntime{}
		m.chats[c.ID] = rt
	}
	rt.mu.Lock()
	m.mu.Unlock()
	defer rt.mu.Unlock()
	if rt.busy || rt.stopping {
		return ChatMessage{}, errChatTurnRunning
	}
	start, ok := runtimes[kind]
	if !ok {
		return ChatMessage{}, errChatUnsupported
	}
	model, err := m.model(c.ID)
	if err != nil {
		return ChatMessage{}, err
	}
	c.Model = model
	if err := m.db.QueryRow(`SELECT thinking FROM chat WHERE id=?`, c.ID).Scan(&c.Thinking); err != nil {
		return ChatMessage{}, err
	}
	if err := m.db.QueryRow(`SELECT show_thinking FROM chat WHERE id=?`, c.ID).Scan(&c.ShowThinking); err != nil {
		return ChatMessage{}, err
	}
	workdir, err := m.workdir(c.ID)
	if err != nil {
		return ChatMessage{}, err
	}
	c.Workdir = workdir
	if c.ConnectorID == nil {
		return ChatMessage{}, errChatConnectorMissing
	}
	if err := m.restartThinkingLocked(kind, c, rt); err != nil {
		return ChatMessage{}, fmt.Errorf("restart chat for thinking change: %w", err)
	}
	if rt.driver != nil {
		specs, specErr := m.chatMCPServersWithNotes(c.ID, false)
		if specErr != nil {
			return ChatMessage{}, specErr
		}
		current, _ := json.Marshal(specs)
		started, _ := json.Marshal(rt.mcpSpecs)
		if string(current) != string(started) {
			driver := rt.driver
			rt.stopping = true
			rt.stopDone = make(chan struct{})
			done := rt.stopDone
			m.stopRuntimeTimerLocked(rt)
			rt.mu.Unlock()
			err := driver.close()
			rt.mu.Lock()
			if rt.driver == driver {
				rt.driver, rt.mcpSpecs = nil, nil
			}
			rt.stopping, rt.stopDone = false, nil
			close(done)
			if err != nil {
				return ChatMessage{}, err
			}
			if _, err := InsertChatMessage(m.db, c.ID, "error", "MCP servers changed — reconnected."); err != nil {
				return ChatMessage{}, err
			}
			m.emit(c.ID, map[string]any{"type": "error", "message": "MCP servers changed — reconnected."})
		}
	}
	if err := m.claimTurn(c.ID); err != nil {
		return ChatMessage{}, err
	}
	message, err := InsertChatMessageWithAttachments(m.db, c.ID, "user", request.Text, request.Attachments)
	if err != nil {
		return ChatMessage{}, errors.Join(err, m.releaseTurn(c.ID))
	}
	rt.connectorID = *c.ConnectorID
	rt.streamMu.Lock()
	rt.showThinking = c.ShowThinking
	rt.pendingThinking.Reset()
	rt.assistantHasText, rt.toolAfterText = false, false
	rt.streamMu.Unlock()
	rt.busy = true
	if rt.driver == nil {
		specs, specErr := m.chatMCPServersWithNotes(c.ID, false)
		if specErr != nil {
			rt.busy = false
			return message, errors.Join(specErr, m.releaseTurn(c.ID))
		}
		rt.driver, err = start(m, c, configDir, rt)
		if err != nil {
			rt.busy = false
			return message, errors.Join(err, m.releaseTurn(c.ID))
		}
		rt.mcpSpecs = specs
		if kind == "claude" || kind == "copilot" {
			thinking := c.Thinking
			rt.startedThinking = &thinking
		}
	}
	if err := rt.driver.send(request); err != nil {
		rt.busy = false
		if releaseErr := m.releaseTurn(c.ID); releaseErr != nil {
			return message, errors.Join(err, releaseErr)
		}
		m.markRuntimeActiveLocked(c.ID, rt)
		return message, fmt.Errorf("send chat message: %w", err)
	}
	m.markRuntimeActiveLocked(c.ID, rt)
	return message, nil
}

func (m *chatManager) claimTurn(id string) error {
	if m.windowID == "" {
		return nil
	}
	return txrun(m.db, func(tx *sql.Tx) error {
		var owner string
		var heartbeat sql.NullString
		err := tx.QueryRow(`SELECT t.window_id,w.heartbeat_at FROM chat_turn t LEFT JOIN app_window w ON w.id=t.window_id WHERE t.chat_id=?`, id).Scan(&owner, &heartbeat)
		if err == nil {
			if owner != m.windowID && heartbeat.Valid && heartbeat.String >= nowMinusWindowStale() {
				return errChatTurnOtherWindow
			}
			if _, err := tx.Exec(`DELETE FROM chat_turn WHERE chat_id=?`, id); err != nil {
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(`INSERT INTO chat_turn(chat_id,window_id,started_at) VALUES(?,?,?)`, id, m.windowID, now())
		return err
	})
}

func (m *chatManager) releaseTurn(id string) error {
	if m.windowID == "" {
		return nil
	}
	_, err := m.db.Exec(`DELETE FROM chat_turn WHERE chat_id=? AND window_id=?`, id, m.windowID)
	return err
}

var errChatTurnOtherWindow = errors.New("A reply is already running in another window")

func (m *chatManager) restartThinkingLocked(kind string, c chat, rt *chatRuntime) error {
	if (kind != "claude" && kind != "copilot") || rt.driver == nil || rt.startedThinking == nil || *rt.startedThinking == c.Thinking {
		return nil
	}
	driver := rt.driver
	rt.stopping = true
	rt.stopDone = make(chan struct{})
	done := rt.stopDone
	m.stopRuntimeTimerLocked(rt)
	rt.mu.Unlock()
	err := driver.close()
	rt.mu.Lock()
	if rt.driver == driver {
		rt.driver = nil
		rt.busy = false
		rt.startedThinking = nil
	}
	rt.stopping = false
	rt.stopDone = nil
	close(done)
	return err
}

func (m *chatManager) model(chatID string) (string, error) {
	var model string
	err := m.db.QueryRow(`SELECT model FROM chat WHERE id=?`, chatID).Scan(&model)
	return model, err
}

func (m *chatManager) workdir(chatID string) (string, error) {
	var folder sql.NullString
	if err := m.db.QueryRow(`SELECT folder FROM chat WHERE id=?`, chatID).Scan(&folder); err != nil {
		return "", err
	}
	if folder.Valid {
		return folder.String, nil
	}
	return filepath.Join(m.dbDir, "chats", chatID), nil
}

var (
	errChatTurnRunning        = errors.New("chat turn already running")
	errChatModelChangeBusy    = errors.New("Stop the reply before changing the model")
	errChatThinkingChangeBusy = errors.New("Stop the reply before changing thinking")
	errChatFolderChangeBusy   = errors.New("Stop the reply before changing the folder")
	errChatFolderLocked       = errors.New("Start a new chat to use another folder")
	errChatUnsupported        = errors.New("not supported yet")
	errChatConnectorMissing   = errors.New("chat has no connector")
	errChatManagerClosed      = errors.New("chat manager is closed")
)

func (m *chatManager) emit(id string, data any) { m.emitEvent("chat:"+id, data) }

func (m *chatManager) emitDelta(id, text string) string {
	rt := m.lookupRuntime(id)
	if rt != nil {
		rt.streamMu.Lock()
		if text != "" && rt.toolAfterText {
			text = "\n\n" + text
			rt.toolAfterText = false
		}
		if text != "" {
			rt.assistantHasText = true
		}
		rt.streamMu.Unlock()
	}
	if err := m.flushThinking(id); err != nil {
		m.emit(id, map[string]any{"type": "error", "message": err.Error()})
	}
	m.emit(id, map[string]any{"type": "delta", "text": text})
	return text
}

func (m *chatManager) emitThinking(id, text string) {
	rt := m.lookupRuntime(id)
	if rt == nil {
		return
	}
	rt.streamMu.Lock()
	show := rt.showThinking
	if show {
		rt.pendingThinking.WriteString(text)
	}
	rt.streamMu.Unlock()
	if !show {
		return
	}
	m.emit(id, map[string]any{"type": "thinking_delta", "text": text})
}

func (m *chatManager) flushThinking(id string) error {
	rt := m.lookupRuntime(id)
	if rt == nil {
		return nil
	}
	rt.streamMu.Lock()
	text := rt.pendingThinking.String()
	rt.pendingThinking.Reset()
	rt.streamMu.Unlock()
	if text == "" {
		return nil
	}
	message, err := InsertChatMessage(m.db, id, "thinking", text)
	if err != nil {
		return err
	}
	m.emit(id, map[string]any{"type": "thinking", "message": message})
	return nil
}

func (m *chatManager) emitTool(id, summary string) error {
	m.markToolAfterText(id)
	if err := m.flushThinking(id); err != nil {
		return err
	}
	message, err := InsertChatMessage(m.db, id, "tool", summary)
	if err != nil {
		return err
	}
	m.emit(id, map[string]any{"type": "tool", "message": message})
	return nil
}

func (m *chatManager) emitToolCall(id, name, input string) error {
	if !isPatioAskUser(name) {
		return m.emitTool(id, strings.TrimSpace(name+" "+shortText(input, toolInputLimit)))
	}
	var questions any
	if err := json.Unmarshal([]byte(input), &questions); err != nil {
		return fmt.Errorf("invalid ask_user input: %w", err)
	}
	m.markToolAfterText(id)
	if err := m.flushThinking(id); err != nil {
		return err
	}
	message, err := InsertChatMessage(m.db, id, "question", input)
	if err != nil {
		return err
	}
	m.emit(id, map[string]any{"type": "question", "message": message})
	if m.notify != nil {
		var taskTitle string
		err := m.db.QueryRow(`SELECT t.title FROM scheduled_task_run r JOIN scheduled_task t ON t.id=r.task_id WHERE r.chat_id=? AND r.status='running'`, id).Scan(&taskTitle)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if err := m.notify("scheduled", taskTitle+" has a question", "", "/ai/chats/"+id, true); err != nil {
				log.Printf("notify scheduled question for chat %s: %v", id, err)
			}
		}
	}
	return nil
}

func isPatioAskUser(name string) bool {
	return name == "mcp__patio__ask_user" || name == "patio.ask_user" || name == "patio_ask_user"
}

func (m *chatManager) emitToolResult(id, summary, result string) error {
	m.markToolAfterText(id)
	if err := m.flushThinking(id); err != nil {
		return err
	}
	message, err := InsertChatToolMessage(m.db, id, summary, result)
	if err != nil {
		return err
	}
	m.emit(id, map[string]any{"type": "tool", "message": message})
	return nil
}

func (m *chatManager) markToolAfterText(id string) {
	if rt := m.lookupRuntime(id); rt != nil {
		rt.streamMu.Lock()
		rt.toolAfterText = rt.toolAfterText || rt.assistantHasText
		rt.streamMu.Unlock()
	}
}

func (m *chatManager) finishTurn(id, assistantText string, turnErr error) error {
	if err := m.flushThinking(id); err != nil {
		return err
	}
	role, content := "assistant", assistantText
	if turnErr != nil {
		role, content = "error", turnErr.Error()
	}
	message, err := InsertChatMessage(m.db, id, role, content)
	if rt := m.lookupRuntime(id); rt != nil {
		rt.mu.Lock()
		rt.busy = false
		rt.streamMu.Lock()
		rt.assistantHasText, rt.toolAfterText = false, false
		rt.streamMu.Unlock()
		m.markRuntimeActiveLocked(id, rt)
		rt.mu.Unlock()
	}
	if err != nil {
		if releaseErr := m.releaseTurn(id); releaseErr != nil {
			log.Printf("release chat turn %s: %v", id, releaseErr)
		}
		return err
	}
	if err := m.releaseTurn(id); err != nil {
		return err
	}
	m.emit(id, map[string]any{"type": "done", "message": message})
	var runID, taskID, title string
	lookupErr := m.db.QueryRow(`SELECT r.id,t.id,t.title FROM scheduled_task_run r JOIN scheduled_task t ON t.id=r.task_id WHERE r.chat_id=? AND r.status IN ('running','waiting')`, id).Scan(&runID, &taskID, &title)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return lookupErr
	}
	if lookupErr == nil {
		var openQuestion int
		if err := m.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM chat_message WHERE chat_id=? AND role='question' AND answered=0)`, id).Scan(&openQuestion); err != nil {
			return err
		}
		status, message := "done", "Scheduled task finished: "+title
		failure := ""
		if turnErr != nil {
			status, message, failure = "failed", "Scheduled task failed: "+title, turnErr.Error()
		} else if openQuestion != 0 {
			status, message = "waiting", ""
		}
		if _, err := m.db.Exec(`UPDATE scheduled_task_run SET status=?,error=? WHERE id=? AND status IN ('running','waiting')`, status, failure, runID); err != nil {
			return err
		}
		if status != "waiting" && m.notify != nil {
			if err := m.notify("scheduled", message, "", "/ai/scheduled/"+taskID, true); err != nil {
				log.Printf("notify scheduled task run %s: %v", runID, err)
			}
		}
	} else if turnErr == nil && m.notify != nil {
		var title string
		if err := m.db.QueryRow(`SELECT title FROM chat WHERE id=?`, id).Scan(&title); err != nil {
			return err
		}
		if err := m.notify("chat", "Reply finished: "+title, "", "/ai/chats/"+id, false); err != nil {
			log.Printf("notify finished chat %s: %v", id, err)
		}
	} else if turnErr != nil && m.notify != nil {
		var title string
		if err := m.db.QueryRow(`SELECT title FROM chat WHERE id=?`, id).Scan(&title); err != nil {
			return err
		}
		if err := m.notify("chat", "Chat failed: "+title, "", "/ai/chats/"+id, false); err != nil {
			log.Printf("notify failed chat %s: %v", id, err)
		}
	}
	return nil
}

func (m *chatManager) saveSession(id, sessionID string) error {
	return SetChatSessionID(m.db, id, sessionID)
}

func (m *chatManager) saveQuota(connectorID string, quota connectorcli.Quota) error {
	data, err := json.Marshal(quota)
	if err != nil {
		return err
	}
	_, err = m.db.Exec(`UPDATE connector SET quota_json=? WHERE id=?`, string(data), connectorID)
	return err
}

func (m *chatManager) connectorConfig(connectorID string) (string, string, error) {
	var kind string
	var configDir sql.NullString
	err := m.db.QueryRow(`SELECT kind,config_dir FROM connector WHERE id=?`, connectorID).Scan(&kind, &configDir)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errChatConnectorMissing
	}
	if err != nil {
		return "", "", err
	}
	if !configDir.Valid {
		return kind, "", nil
	}
	return kind, configDir.String, nil
}

func connectorEnv(kind, configDir string) []string {
	return connectorcli.Env(kind, configDir, "")
}

func setChatEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i := range env {
		if strings.HasPrefix(env[i], prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func (m *chatManager) ticketMCPServerSpec() (map[string]any, error) {
	exe := m.mcpExecutable
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"command": exe, "args": []string{"mcp", "-d", m.dbPath, "-in-app"}}, nil
}

func (m *chatManager) requestApproval(ctx context.Context, chatID, toolName string, input json.RawMessage) (bool, error) {
	id, auto, err := CreateChatApproval(m.db, chatID, toolName, input)
	if err != nil {
		return false, err
	}
	if auto {
		return true, nil
	}
	var taskTitle, taskID string
	if err := m.db.QueryRow(`SELECT t.title,t.id FROM chat c JOIN scheduled_task t ON t.id=c.scheduled_task_id WHERE c.id=?`, chatID).Scan(&taskTitle, &taskID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if m.notify != nil {
		var title string
		if err := m.db.QueryRow(`SELECT title FROM chat WHERE id=?`, chatID).Scan(&title); err != nil {
			return false, err
		}
		link, kind, noticeTitle := "/ai/chats/"+chatID, "approval", "Approval needed: "+title
		if taskTitle != "" {
			link, kind, noticeTitle = "/ai/scheduled/"+taskID, "scheduled", "Scheduled task needs approval: "+taskTitle
		}
		if err := m.notify(kind, noticeTitle, "", link, true); err != nil {
			log.Printf("notify chat approval %s: %v", chatID, err)
		}
	}
	m.emit(chatID, map[string]any{"type": "approval"})
	timer := time.NewTimer(m.approvalTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(m.approvalPollInterval)
	defer ticker.Stop()
	for {
		status, err := ChatApprovalStatus(m.db, chatID, id)
		if err != nil {
			return false, err
		}
		switch status {
		case "allowed":
			return true, nil
		case "denied", "expired":
			return false, nil
		}
		select {
		case <-ctx.Done():
			status, err := ExpireChatApproval(m.db, chatID, id)
			if err != nil {
				return false, err
			}
			if status == "allowed" {
				return true, nil
			}
			if status == "denied" {
				return false, nil
			}
			if status == "expired" {
				return false, nil
			}
			return false, ctx.Err()
		case <-timer.C:
			status, err := ExpireChatApproval(m.db, chatID, id)
			if err != nil {
				return false, err
			}
			return status == "allowed", nil
		case <-ticker.C:
		}
	}
}

func (m *chatManager) Stop(id string) error {
	rt := m.lookupRuntime(id)
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	if rt.stopping {
		done := rt.stopDone
		rt.mu.Unlock()
		if done != nil {
			<-done
		}
		return nil
	}
	if rt.driver == nil || !rt.busy {
		rt.mu.Unlock()
		return nil
	}
	driver := rt.driver
	rt.stopping = true
	rt.stopDone = make(chan struct{})
	done := rt.stopDone
	m.stopRuntimeTimerLocked(rt)
	rt.mu.Unlock()
	err := driver.stop()
	rt.mu.Lock()
	if rt.driver == driver {
		rt.driver = nil
		rt.busy = false
		rt.startedThinking = nil
	}
	rt.stopping = false
	rt.stopDone = nil
	close(done)
	rt.mu.Unlock()
	if releaseErr := m.releaseTurn(id); releaseErr != nil {
		return errors.Join(err, releaseErr)
	}
	m.emit(id, map[string]any{"type": "done"})
	return err
}

func (m *chatManager) ChangeModel(id string, update func() error) error {
	return m.restart(id, errChatModelChangeBusy, update)
}

// ChangeFolder closes the runtime so the next turn starts in the new folder.
func (m *chatManager) ChangeFolder(id string, update func() error) error {
	return m.restart(id, errChatFolderChangeBusy, update)
}

func (m *chatManager) restart(id string, busy error, update func() error) error {
	rt := m.runtime(id)
	rt.mu.Lock()
	if rt.busy || rt.stopping {
		rt.mu.Unlock()
		return busy
	}
	if err := update(); err != nil {
		rt.mu.Unlock()
		return err
	}
	driver := rt.driver
	if driver == nil {
		rt.mu.Unlock()
		return nil
	}
	rt.stopping, rt.changingModel = true, true
	rt.stopDone = make(chan struct{})
	done := rt.stopDone
	m.stopRuntimeTimerLocked(rt)
	rt.mu.Unlock()
	err := driver.close()
	rt.mu.Lock()
	if rt.driver == driver {
		rt.driver = nil
		rt.busy = false
	}
	rt.stopping, rt.changingModel = false, false
	rt.stopDone = nil
	close(done)
	rt.mu.Unlock()
	return err
}

func (m *chatManager) UpdateChat(id string, update func() error) error {
	rt := m.runtime(id)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.busy || rt.stopping {
		return errChatThinkingChangeBusy
	}
	return update()
}

func (m *chatManager) lookupRuntime(id string) *chatRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chats[id]
}

func (m *chatManager) CloseChat(id string) error { return m.closeRuntime(id) }

func (m *chatManager) CloseConnector(id string) error {
	m.mu.Lock()
	ids := make([]string, 0)
	for chatID, rt := range m.chats {
		rt.mu.Lock()
		match := rt.connectorID == id
		rt.mu.Unlock()
		if match {
			ids = append(ids, chatID)
		}
	}
	m.mu.Unlock()
	var joined error
	for _, chatID := range ids {
		joined = errors.Join(joined, m.closeRuntime(chatID))
	}
	return joined
}

func (m *chatManager) closeRuntime(id string) error {
	rt := m.lookupRuntime(id)
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	if rt.stopping {
		done := rt.stopDone
		rt.mu.Unlock()
		if done != nil {
			<-done
		}
		return nil
	}
	driver := rt.driver
	m.stopRuntimeTimerLocked(rt)
	if driver == nil {
		rt.busy = false
		rt.mu.Unlock()
		return nil
	}
	rt.stopping = true
	rt.stopDone = make(chan struct{})
	done := rt.stopDone
	rt.mu.Unlock()
	err := driver.close()
	rt.mu.Lock()
	if rt.driver == driver {
		rt.driver = nil
		rt.busy = false
	}
	rt.stopping = false
	rt.stopDone = nil
	close(done)
	rt.mu.Unlock()
	return err
}

func (m *chatManager) closeIdleRuntime(id string, rt *chatRuntime) error {
	rt.mu.Lock()
	if rt.driver == nil || rt.busy || rt.stopping {
		rt.mu.Unlock()
		return nil
	}
	remaining := m.idleTimeout - time.Since(rt.lastActivity)
	if remaining > 0 {
		m.resetRuntimeTimerLocked(id, rt, remaining)
		rt.mu.Unlock()
		return nil
	}
	var pending bool
	err := m.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM chat_approval WHERE chat_id=? AND status='pending')`, id).Scan(&pending)
	if err != nil || pending {
		m.resetRuntimeTimerLocked(id, rt, m.idleTimeout)
		rt.mu.Unlock()
		if err != nil {
			m.emit(id, map[string]any{"type": "error", "message": "check pending chat approvals: " + err.Error()})
		}
		return nil
	}
	driver := rt.driver
	rt.stopping = true
	rt.stopDone = make(chan struct{})
	done := rt.stopDone
	m.stopRuntimeTimerLocked(rt)
	rt.mu.Unlock()
	err = driver.close()
	rt.mu.Lock()
	if rt.driver == driver {
		rt.driver = nil
		rt.busy = false
	}
	rt.stopping = false
	rt.stopDone = nil
	close(done)
	rt.mu.Unlock()
	return err
}

func (m *chatManager) markRuntimeActiveLocked(id string, rt *chatRuntime) {
	if rt.driver == nil || rt.stopping {
		return
	}
	rt.lastActivity = time.Now()
	m.resetRuntimeTimerLocked(id, rt, m.idleTimeout)
}

func (m *chatManager) resetRuntimeTimerLocked(id string, rt *chatRuntime, delay time.Duration) {
	if rt.idleTimer != nil {
		rt.idleTimer.Stop()
	}
	rt.idleTimer = time.AfterFunc(delay, func() {
		if err := m.closeIdleRuntime(id, rt); err != nil {
			m.emit(id, map[string]any{"type": "error", "message": "close idle chat runtime: " + err.Error()})
		}
	})
}

func (m *chatManager) stopRuntimeTimerLocked(rt *chatRuntime) {
	if rt.idleTimer != nil {
		rt.idleTimer.Stop()
		rt.idleTimer = nil
	}
}

func (m *chatManager) Close() error {
	m.mu.Lock()
	m.closed = true
	ids := make([]string, 0, len(m.chats))
	for id := range m.chats {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var joined error
	for _, id := range ids {
		joined = errors.Join(joined, m.closeRuntime(id))
	}
	return joined
}

type osChatProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	wait   sync.Once
	err    error
}

func startOSChatProcess(path string, args, env []string, cwd string) (chatProcess, error) {
	cmd := exec.Command(path, args...)
	cmd.Env, cmd.Dir = env, cwd
	proc.HideConsole(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &osChatProcess{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

func (p *osChatProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *osChatProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *osChatProcess) Stderr() io.ReadCloser { return p.stderr }
func (p *osChatProcess) Interrupt() error {
	if runtime.GOOS == "windows" {
		return p.Kill()
	}
	return p.cmd.Process.Signal(os.Interrupt)
}
func (p *osChatProcess) Kill() error { return p.cmd.Process.Kill() }
func (p *osChatProcess) Wait() error {
	p.wait.Do(func() { p.err = p.cmd.Wait() })
	return p.err
}
