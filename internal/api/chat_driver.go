package api

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	connectorcli "my-tickets/internal/connector"
)

const (
	chatLineLimit    = 10 << 20
	chatSystemPrompt = "You are chatting inside the my-tickets desktop app. Reply in Markdown. When you mention a ticket or board you read, created, or changed with the my-tickets tools, link it with an app path: tickets as [CODE: Title](/tickets/<ticket id>) and boards as [Board name](/boards/<board id>), using the ids returned by the tools. Never invent ids."
)

type chatProcess interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Interrupt() error
	Kill() error
	Wait() error
}

type chatProcessFactory func(path string, args, env []string, cwd string) (chatProcess, error)
type chatCommandBuilder func(exe, dbPath, chatID, model, sessionID string) ([]string, error)

type chatManager struct {
	db             *sql.DB
	dbPath         string
	emitEvent      func(string, any)
	processFactory chatProcessFactory
	commandBuilder chatCommandBuilder
	findClaude     func() (string, error)
	mu             sync.Mutex
	chats          map[string]*chatRuntime
}

type chatRuntime struct {
	mu            sync.Mutex
	process       chatProcess
	processDone   chan struct{}
	busy          bool
	stopping      bool
	changingModel bool
	connectorID   string
	assistant     strings.Builder
}

func newChatManager(database *sql.DB, options Options) *chatManager {
	emit := options.Emit
	if emit == nil {
		emit = func(string, any) {}
	}
	factory := options.processFactory
	if factory == nil {
		factory = startOSChatProcess
	}
	commandBuilder := options.commandBuilder
	if commandBuilder == nil {
		commandBuilder = buildClaudeArgs
	}
	finder := options.findClaude
	if finder == nil {
		finder = func() (string, error) { return connectorcli.Find("claude") }
	}
	return &chatManager{db: database, dbPath: options.DBPath, emitEvent: emit, processFactory: factory, commandBuilder: commandBuilder, findClaude: finder, chats: make(map[string]*chatRuntime)}
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
	if err := m.db.QueryRow(`SELECT model FROM chat WHERE id=?`, c.ID).Scan(&c.Model); err != nil {
		return ChatMessage{}, err
	}
	if kind != "claude" {
		return ChatMessage{}, errChatUnsupported
	}
	if c.ConnectorID == nil {
		return ChatMessage{}, errChatConnectorMissing
	}
	message, err := InsertChatMessage(m.db, c.ID, "user", text)
	if err != nil {
		return ChatMessage{}, err
	}
	rt.connectorID = *c.ConnectorID
	rt.assistant.Reset()
	rt.busy = true
	if rt.process == nil {
		if err := m.startLocked(c, configDir, rt); err != nil {
			rt.busy = false
			return message, err
		}
	}
	payload, err := json.Marshal(struct {
		Type            string `json:"type"`
		Message         any    `json:"message"`
		ParentToolUseID any    `json:"parent_tool_use_id"`
	}{"user", struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{"user", text}, nil})
	if err == nil {
		_, err = rt.process.Stdin().Write(append(payload, '\n'))
	}
	if err != nil {
		rt.busy = false
		return message, fmt.Errorf("send chat message: %w", err)
	}
	return message, nil
}

var (
	errChatTurnRunning      = errors.New("chat turn already running")
	errChatModelChangeBusy  = errors.New("Stop the reply before changing the model")
	errChatUnsupported      = errors.New("not supported yet")
	errChatConnectorMissing = errors.New("chat has no connector")
)

func (m *chatManager) startLocked(c chat, configDir string, rt *chatRuntime) error {
	path, err := m.findClaude()
	if err != nil {
		return fmt.Errorf("find claude: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args, err := m.commandBuilder(exe, m.dbPath, c.ID, c.Model, stringValue(c.SessionID))
	if err != nil {
		return err
	}
	env := connectorcli.Env("claude", configDir, "")
	p, err := m.processFactory(path, args, env, c.Workdir)
	if err != nil {
		return fmt.Errorf("start claude: %w", err)
	}
	rt.process = p
	rt.processDone = make(chan struct{})
	done := rt.processDone
	go m.watch(c.ID, rt, p, done)
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func buildClaudeArgs(exe, dbPath, chatID, model, sessionID string) ([]string, error) {
	config, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"my-tickets": map[string]any{
		"type": "stdio", "command": exe, "args": []string{"mcp", "-d", dbPath, "-approve-chat", chatID},
	}}})
	if err != nil {
		return nil, err
	}
	args := []string{"-p", "--append-system-prompt", chatSystemPrompt, "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "--mcp-config", string(config), "--strict-mcp-config", "--permission-prompt-tool", "mcp__my-tickets__approve")
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	return args, nil
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

func (m *chatManager) watch(id string, rt *chatRuntime, p chatProcess, done chan struct{}) {
	var stderr cappedLines
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, p.Stderr())
		close(stderrDone)
	}()
	scanner := bufio.NewScanner(p.Stdout())
	scanner.Buffer(make([]byte, 64*1024), chatLineLimit)
	for scanner.Scan() {
		if err := m.parseLine(id, rt, scanner.Bytes()); err != nil {
			m.emit(id, map[string]any{"type": "error", "message": err.Error()})
		}
	}
	scanErr := scanner.Err()
	waitErr := p.Wait()
	<-stderrDone
	rt.mu.Lock()
	expected := rt.stopping
	unfinished := rt.busy
	if rt.process == p {
		rt.process = nil
		rt.busy = false
		if !rt.changingModel {
			rt.stopping = false
		}
	}
	rt.mu.Unlock()
	close(done)
	if !expected && (unfinished || waitErr != nil || scanErr != nil || stderr.String() != "") {
		parts := []string{stderr.String()}
		if scanErr != nil {
			parts = append(parts, scanErr.Error())
		}
		if waitErr != nil {
			parts = append(parts, waitErr.Error())
		}
		message := strings.TrimSpace(strings.Join(parts, "\n"))
		if message == "" {
			message = "Claude process exited unexpectedly"
		}
		m.emit(id, map[string]any{"type": "error", "message": message})
	}
}

type cappedLines struct {
	mu   sync.Mutex
	data []byte
}

func (c *cappedLines) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = append(c.data, p...)
	if len(c.data) > 8<<10 {
		start := len(c.data) - (8 << 10)
		if newline := bytes.IndexByte(c.data[start:], '\n'); newline >= 0 {
			start += newline + 1
		}
		c.data = append([]byte(nil), c.data[start:]...)
	}
	return len(p), nil
}

func (c *cappedLines) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(string(c.data))
}

func (m *chatManager) emit(id string, data any) { m.emitEvent("chat:"+id, data) }

func (m *chatManager) parseLine(id string, rt *chatRuntime, raw []byte) error {
	var event map[string]json.RawMessage
	if err := json.Unmarshal(raw, &event); err != nil {
		return fmt.Errorf("decode Claude stream event: %w", err)
	}
	var typ string
	if err := json.Unmarshal(event["type"], &typ); err != nil {
		return errors.New("Claude stream event missing type")
	}
	switch typ {
	case "system":
		var init struct {
			Subtype   string `json:"subtype"`
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(raw, &init); err != nil {
			return err
		}
		if init.Subtype == "init" && init.SessionID != "" {
			var previous sql.NullString
			if err := m.db.QueryRow(`SELECT session_id FROM chat WHERE id=?`, id).Scan(&previous); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if !previous.Valid || previous.String != init.SessionID {
				return SetChatSessionID(m.db, id, init.SessionID)
			}
		}
	case "stream_event":
		var frame struct {
			Event struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			} `json:"event"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			return err
		}
		if frame.Event.Type == "content_block_delta" && frame.Event.Delta.Type == "text_delta" {
			m.emit(id, map[string]any{"type": "delta", "text": frame.Event.Delta.Text})
		}
	case "assistant":
		var assistant struct {
			Message struct {
				Content []struct {
					Type  string          `json:"type"`
					Text  string          `json:"text"`
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(raw, &assistant); err != nil {
			return err
		}
		for _, block := range assistant.Message.Content {
			switch block.Type {
			case "text":
				rt.mu.Lock()
				rt.assistant.WriteString(block.Text)
				rt.mu.Unlock()
			case "tool_use":
				args, err := compactJSON(block.Input)
				if err != nil {
					return err
				}
				content := strings.TrimSpace(block.Name + " " + shortText(args, 180))
				message, err := InsertChatMessage(m.db, id, "tool", content)
				if err != nil {
					return err
				}
				m.emit(id, map[string]any{"type": "tool", "message": message})
			}
		}
	case "result":
		var result struct {
			IsError bool   `json:"is_error"`
			Result  string `json:"result"`
			Errors  []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return err
		}
		rt.mu.Lock()
		content := rt.assistant.String()
		rt.assistant.Reset()
		rt.mu.Unlock()
		if content == "" {
			content = result.Result
		}
		role := "assistant"
		if result.IsError {
			role = "error"
			if content == "" && len(result.Errors) > 0 {
				content = result.Errors[0].Message
			}
		}
		message, err := InsertChatMessage(m.db, id, role, content)
		if err != nil {
			rt.mu.Lock()
			rt.busy = false
			rt.mu.Unlock()
			return err
		}
		rt.mu.Lock()
		rt.busy = false
		rt.mu.Unlock()
		m.emit(id, map[string]any{"type": "done", "message": message})
	case "rate_limit_event":
		var limit struct {
			Info claudeRateLimitInfo `json:"rate_limit_info"`
		}
		if err := json.Unmarshal(raw, &limit); err != nil {
			return err
		}
		if err := m.storeClaudeQuota(id, limit.Info); err != nil {
			return err
		}
	}
	return nil
}

type claudeRateWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *float64 `json:"resetsAt"`
}

type claudeRateLimitInfo struct {
	Utilization    *float64                    `json:"utilization"`
	ResetsAt       *float64                    `json:"resetsAt"`
	RateLimitType  string                      `json:"rateLimitType"`
	UnifiedWindows map[string]claudeRateWindow `json:"unifiedWindows"`
}

func compactJSON(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "{}", nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", err
	}
	return compact.String(), nil
}

func shortText(value string, limit int) string {
	runes := []rune(strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " "))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}

func (m *chatManager) storeClaudeQuota(id string, info claudeRateLimitInfo) error {
	windows := make([]connectorcli.Window, 0, len(info.UnifiedWindows))
	if len(info.UnifiedWindows) > 0 {
		keys := make([]string, 0, len(info.UnifiedWindows))
		for key := range info.UnifiedWindows {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			rate := info.UnifiedWindows[key]
			window, err := claudeWindow(key, rate.Utilization, rate.ResetsAt)
			if err != nil {
				return err
			}
			windows = append(windows, window)
		}
	} else {
		window, err := claudeWindow(info.RateLimitType, info.Utilization, info.ResetsAt)
		if err != nil {
			return err
		}
		windows = append(windows, window)
	}
	quota := connectorcli.Quota{Kind: "claude", Windows: windows}
	updated := time.Now().UTC().Format(time.RFC3339)
	quota.UpdatedAt = &updated
	data, err := json.Marshal(quota)
	if err != nil {
		return err
	}
	_, err = m.db.Exec(`UPDATE connector SET quota_json=? WHERE id=(SELECT connector_id FROM chat WHERE id=?) AND kind='claude'`, string(data), id)
	return err
}

func claudeWindow(label string, utilization, resetsAt *float64) (connectorcli.Window, error) {
	if utilization == nil || math.IsNaN(*utilization) || math.IsInf(*utilization, 0) {
		return connectorcli.Window{}, errors.New("rate limit event missing utilization")
	}
	used := *utilization
	if used <= 1 {
		used *= 100
	}
	switch label {
	case "five_hour":
		label = "5h"
	case "seven_day":
		label = "weekly"
	}
	window := connectorcli.Window{Label: label, UsedPercent: &used}
	if resetsAt != nil {
		reset := time.Unix(int64(*resetsAt), 0).UTC().Format(time.RFC3339)
		window.ResetsAt = &reset
	}
	return window, nil
}

func (m *chatManager) Stop(id string) error {
	m.mu.Lock()
	rt := m.chats[id]
	m.mu.Unlock()
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	if rt.process == nil {
		rt.mu.Unlock()
		return nil
	}
	if rt.stopping {
		done := rt.processDone
		rt.mu.Unlock()
		<-done
		return nil
	}
	if !rt.busy {
		rt.mu.Unlock()
		return nil
	}
	p, done := rt.process, rt.processDone
	rt.stopping = true
	rt.mu.Unlock()
	_ = p.Interrupt()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = p.Kill()
		<-done
	}
	rt.mu.Lock()
	if rt.process == p {
		rt.process = nil
		rt.busy = false
		rt.stopping = false
	}
	rt.mu.Unlock()
	m.emit(id, map[string]any{"type": "done"})
	return nil
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
	p, done := rt.process, rt.processDone
	if p == nil {
		rt.mu.Unlock()
		return nil
	}
	rt.stopping, rt.changingModel = true, true
	rt.mu.Unlock()
	_ = p.Kill()
	<-done
	rt.mu.Lock()
	if rt.process == p {
		rt.process = nil
		rt.busy = false
	}
	rt.stopping, rt.changingModel = false, false
	rt.mu.Unlock()
	return nil
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
	m.mu.Lock()
	rt := m.chats[id]
	m.mu.Unlock()
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	p, done := rt.process, rt.processDone
	if p == nil {
		rt.busy = false
		rt.mu.Unlock()
		return nil
	}
	rt.stopping = true
	rt.mu.Unlock()
	_ = p.Kill()
	<-done
	rt.mu.Lock()
	if rt.process == p {
		rt.process = nil
		rt.busy = false
	}
	rt.stopping = false
	rt.mu.Unlock()
	return nil
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
