package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	connectorcli "my-tickets/internal/connector"
)

const chatApprovalTimeout = 10 * time.Minute

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
	send(text string) error
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
	approvalTimeout      time.Duration
	approvalPollInterval time.Duration
	mu                   sync.Mutex
	chats                map[string]*chatRuntime
}

func (m *chatManager) chatMCPServers(chatID string) ([]mcpServerSpec, error) {
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
	mu            sync.Mutex
	driver        chatRuntimeDriver
	busy          bool
	stopping      bool
	changingModel bool
	stopDone      chan struct{}
	connectorID   string
}

func newChatManager(database *sql.DB, options Options) *chatManager {
	emit := options.Emit
	if emit == nil {
		emit = func(string, any) {}
	}
	return &chatManager{
		db: database, dbPath: options.DBPath, mcpExecutable: options.mcpExecutable, emitEvent: emit, options: options,
		approvalTimeout: chatApprovalTimeout, approvalPollInterval: 300 * time.Millisecond,
		chats: make(map[string]*chatRuntime),
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

func (m *chatManager) Send(c chat, kind, configDir, text string) (ChatMessage, error) {
	rt := m.runtime(c.ID)
	rt.mu.Lock()
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
	workdir, err := m.workdir(c.ID)
	if err != nil {
		return ChatMessage{}, err
	}
	c.Workdir = workdir
	if c.ConnectorID == nil {
		return ChatMessage{}, errChatConnectorMissing
	}
	message, err := InsertChatMessage(m.db, c.ID, "user", text)
	if err != nil {
		return ChatMessage{}, err
	}
	rt.connectorID = *c.ConnectorID
	rt.busy = true
	if rt.driver == nil {
		rt.driver, err = start(m, c, configDir, rt)
		if err != nil {
			rt.busy = false
			return message, err
		}
	}
	if err := rt.driver.send(text); err != nil {
		rt.busy = false
		return message, fmt.Errorf("send chat message: %w", err)
	}
	return message, nil
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
	errChatTurnRunning      = errors.New("chat turn already running")
	errChatModelChangeBusy  = errors.New("Stop the reply before changing the model")
	errChatUnsupported      = errors.New("not supported yet")
	errChatConnectorMissing = errors.New("chat has no connector")
)

func (m *chatManager) emit(id string, data any) { m.emitEvent("chat:"+id, data) }

func (m *chatManager) emitDelta(id, text string) {
	m.emit(id, map[string]any{"type": "delta", "text": text})
}

func (m *chatManager) emitTool(id, summary string) error {
	message, err := InsertChatMessage(m.db, id, "tool", summary)
	if err != nil {
		return err
	}
	m.emit(id, map[string]any{"type": "tool", "message": message})
	return nil
}

func (m *chatManager) finishTurn(id, assistantText string, turnErr error) error {
	role, content := "assistant", assistantText
	if turnErr != nil {
		role, content = "error", turnErr.Error()
	}
	message, err := InsertChatMessage(m.db, id, role, content)
	if rt := m.runtime(id); rt != nil {
		rt.mu.Lock()
		rt.busy = false
		rt.mu.Unlock()
	}
	if err != nil {
		return err
	}
	m.emit(id, map[string]any{"type": "done", "message": message})
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

func (m *chatManager) ticketMCPServerSpec() (map[string]any, error) {
	exe := m.mcpExecutable
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"command": exe, "args": []string{"mcp", "-d", m.dbPath}}, nil
}

func (m *chatManager) requestApproval(ctx context.Context, chatID, toolName string, input json.RawMessage) (bool, error) {
	id, err := CreateChatApproval(m.db, chatID, toolName, input)
	if err != nil {
		return false, err
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
	rt.mu.Unlock()
	err := driver.stop()
	rt.mu.Lock()
	if rt.driver == driver {
		rt.driver = nil
		rt.busy = false
	}
	rt.stopping = false
	rt.stopDone = nil
	close(done)
	rt.mu.Unlock()
	m.emit(id, map[string]any{"type": "done"})
	return err
}

func (m *chatManager) ChangeModel(id string, update func() error) error {
	rt := m.runtime(id)
	rt.mu.Lock()
	if rt.busy || rt.stopping {
		rt.mu.Unlock()
		return errChatModelChangeBusy
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
	rt.mu.Unlock()
	err := driver.close()
	rt.mu.Lock()
	if rt.driver == driver {
		rt.driver = nil
		rt.busy = false
	}
	rt.stopping, rt.changingModel = false, false
	rt.mu.Unlock()
	return err
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
	driver := rt.driver
	if driver == nil {
		rt.busy = false
		rt.mu.Unlock()
		return nil
	}
	rt.stopping = true
	rt.mu.Unlock()
	err := driver.close()
	rt.mu.Lock()
	if rt.driver == driver {
		rt.driver = nil
		rt.busy = false
	}
	rt.stopping = false
	rt.mu.Unlock()
	return err
}

func (m *chatManager) Close() error {
	m.mu.Lock()
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
