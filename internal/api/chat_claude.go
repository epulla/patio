package api

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	connectorcli "patio/internal/connector"
)

const (
	chatLineLimit = 10 << 20
)

type chatCommandBuilder func(exe, dbPath, chatID, model, sessionID string) ([]string, error)

type claudeRuntime struct {
	manager        *chatManager
	chatID         string
	workdir        string
	runtime        *chatRuntime
	process        chatProcess
	processDone    chan struct{}
	configFile     string
	configOnce     sync.Once
	assistant      strings.Builder
	thinkingDeltas bool
	textDeltas     bool
	toolMessages   map[string]string // tool_use ID -> tool chat message ID
	steerMu        sync.Mutex
	steering       bool
	steerRequest   ChatSendRequest
}

func init() { runtimes["claude"] = startClaude }

func startClaude(m *chatManager, c chat, configDir string, rt *chatRuntime) (chatRuntimeDriver, error) {
	finder := m.options.findClaude
	if finder == nil {
		finder = func() (string, error) { return connectorcli.Find("claude") }
	}
	path, err := finder()
	if err != nil {
		return nil, fmt.Errorf("find claude: %w", err)
	}
	external, err := m.chatMCPServers(c.ID)
	if err != nil {
		return nil, err
	}
	exe := m.mcpExecutable
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			return nil, err
		}
	}
	var args []string
	prompt, err := ChatPrompt(m.db, c.ID)
	if err != nil {
		return nil, err
	}
	configPath := ""
	if m.options.commandBuilder == nil {
		ticket := map[string]any{"type": "stdio", "command": exe, "args": []string{"mcp", "-d", m.dbPath, "-approve-chat", c.ID, "-in-app"}}
		configPath, err = writeClaudeMCPConfig(m.dbDir, claudeMCPServers(ticket, external))
		if err != nil {
			return nil, err
		}
		args = buildClaudeArgsWithConfig(c.Model, stringValue(c.SessionID), configPath, prompt)
	} else {
		args, err = m.options.commandBuilder(exe, m.dbPath, c.ID, c.Model, stringValue(c.SessionID))
		if err != nil {
			return nil, err
		}
		args = append(args, "--append-system-prompt", prompt)
	}
	factory := m.options.processFactory
	if factory == nil {
		factory = startOSChatProcess
	}
	env := connectorEnv("claude", configDir)
	if !c.Thinking {
		env = setChatEnv(env, "MAX_THINKING_TOKENS", "0")
	}
	process, err := factory(path, args, env, c.Workdir)
	if err != nil {
		if configPath != "" {
			_ = os.Remove(configPath)
		}
		return nil, fmt.Errorf("start claude: %w", err)
	}
	driver := &claudeRuntime{manager: m, chatID: c.ID, workdir: c.Workdir, runtime: rt, process: process, processDone: make(chan struct{}), configFile: configPath}
	rt.driver = driver
	go driver.watch()
	return driver, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func buildClaudeArgsWithConfig(model, sessionID, configPath, prompt string) []string {
	args := []string{"-p", "--disallowedTools", "AskUserQuestion", "--append-system-prompt", prompt, "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "--mcp-config", configPath, "--strict-mcp-config", "--permission-prompt-tool", "mcp__patio__approve")
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	return args
}

func writeClaudeMCPConfig(dbDir string, servers map[string]any) (string, error) {
	config, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		return "", err
	}
	dir := filepath.Join(dbDir, "chats", ".tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "claude-mcp-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.Write(config); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func (d *claudeRuntime) removeConfig() {
	d.configOnce.Do(func() {
		if d.configFile != "" {
			_ = os.Remove(d.configFile)
		}
	})
}

func (d *claudeRuntime) send(request ChatSendRequest) error {
	return d.writeUserMessage(request)
}

func (d *claudeRuntime) writeUserMessage(request ChatSendRequest) error {
	files, err := loadChatAttachmentFiles(d.workdir, request.Attachments)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		Type            string `json:"type"`
		Message         any    `json:"message"`
		ParentToolUseID any    `json:"parent_tool_use_id"`
	}{"user", claudeUserMessage(request.Text, files), nil})
	if err != nil {
		return err
	}
	_, err = d.process.Stdin().Write(append(payload, '\n'))
	return err
}

func (d *claudeRuntime) canSteer() bool {
	d.steerMu.Lock()
	defer d.steerMu.Unlock()
	return !d.steering
}

func (d *claudeRuntime) steer(request ChatSendRequest) error {
	d.steerMu.Lock()
	defer d.steerMu.Unlock()
	if d.steering {
		return errors.New("Claude turn is already being steered")
	}
	d.steering = true
	d.steerRequest = request
	payload, err := json.Marshal(map[string]any{"type": "control_request", "request_id": fmt.Sprintf("steer-%d", time.Now().UnixNano()), "request": map[string]string{"subtype": "interrupt"}})
	if err != nil {
		d.steering = false
		return err
	}
	_, err = d.process.Stdin().Write(append(payload, '\n'))
	if err != nil {
		d.steering = false
	}
	return err
}

func claudeUserMessage(text string, files []chatAttachmentFile) map[string]any {
	if len(files) == 0 {
		return map[string]any{"role": "user", "content": text}
	}
	content := make([]any, 0, len(files)+1)
	prompt := promptWithAttachmentPaths(text, files, true)
	if prompt != "" {
		content = append(content, map[string]string{"type": "text", "text": prompt})
	}
	for _, file := range files {
		if file.Kind != "image" {
			continue
		}
		content = append(content, map[string]any{
			"type":   "image",
			"source": map[string]string{"type": "base64", "media_type": file.MIME, "data": base64.StdEncoding.EncodeToString(file.Data)},
		})
	}
	return map[string]any{"role": "user", "content": content}
}

func (d *claudeRuntime) watch() {
	var stderr cappedLines
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, d.process.Stderr())
		close(stderrDone)
	}()
	scanner := bufio.NewScanner(d.process.Stdout())
	scanner.Buffer(make([]byte, 64*1024), chatLineLimit)
	for scanner.Scan() {
		if err := d.parseLine(scanner.Bytes()); err != nil {
			d.manager.emit(d.chatID, map[string]any{"type": "error", "message": err.Error()})
		}
	}
	scanErr := scanner.Err()
	waitErr := d.process.Wait()
	<-stderrDone
	d.runtime.mu.Lock()
	expected := d.runtime.stopping
	unfinished := d.runtime.busy
	if d.runtime.driver == d {
		d.runtime.driver = nil
		d.runtime.busy = false
		if !d.runtime.changingModel && d.runtime.stopDone == nil {
			d.runtime.stopping = false
		}
	}
	d.runtime.mu.Unlock()
	close(d.processDone)
	d.removeConfig()
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
		d.manager.emit(d.chatID, map[string]any{"type": "error", "message": message})
	}
}

func (d *claudeRuntime) stop() error {
	_ = d.process.Interrupt()
	select {
	case <-d.processDone:
	case <-time.After(5 * time.Second):
		_ = d.process.Kill()
		<-d.processDone
	}
	d.removeConfig()
	return nil
}

func (d *claudeRuntime) close() error {
	_ = d.process.Kill()
	<-d.processDone
	d.removeConfig()
	return nil
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

// toolResultText flattens tool result content, which is a string or a list of MCP text blocks.
func toolResultText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct{ Type, Text string }
	_ = json.Unmarshal(raw, &blocks)
	parts := []string{}
	for _, block := range blocks {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (d *claudeRuntime) parseLine(raw []byte) error {
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
			if err := d.manager.db.QueryRow(`SELECT session_id FROM chat WHERE id=?`, d.chatID).Scan(&previous); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if !previous.Valid || previous.String != init.SessionID {
				return d.manager.saveSession(d.chatID, init.SessionID)
			}
		}
	case "stream_event":
		var frame struct {
			Event struct {
				Type  string `json:"type"`
				Delta struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					Thinking string `json:"thinking"`
				} `json:"delta"`
			} `json:"event"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			return err
		}
		if frame.Event.Type == "content_block_delta" && frame.Event.Delta.Type == "text_delta" {
			d.textDeltas = true
			d.assistant.WriteString(d.manager.emitDelta(d.chatID, frame.Event.Delta.Text))
		} else if frame.Event.Type == "content_block_delta" && frame.Event.Delta.Type == "thinking_delta" {
			d.thinkingDeltas = true
			d.manager.emitThinking(d.chatID, frame.Event.Delta.Thinking)
		}
	case "assistant":
		var assistant struct {
			Message struct {
				Content []struct {
					Type     string          `json:"type"`
					ID       string          `json:"id"`
					Text     string          `json:"text"`
					Thinking string          `json:"thinking"`
					Name     string          `json:"name"`
					Input    json.RawMessage `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(raw, &assistant); err != nil {
			return err
		}
		for _, block := range assistant.Message.Content {
			switch block.Type {
			case "text":
				if !d.textDeltas {
					if d.assistant.Len() > 0 {
						d.assistant.WriteString("\n\n")
					}
					d.assistant.WriteString(block.Text)
				}
			case "thinking":
				if !d.thinkingDeltas {
					d.manager.emitThinking(d.chatID, block.Thinking)
				}
			case "tool_use":
				args, err := compactJSON(block.Input)
				if err != nil {
					return err
				}
				content := strings.TrimSpace(block.Name + " " + shortText(args, toolInputLimit))
				messageID, err := d.manager.emitToolCall(d.chatID, content)
				if err != nil {
					return err
				}
				if block.ID != "" {
					if d.toolMessages == nil {
						d.toolMessages = map[string]string{}
					}
					d.toolMessages[block.ID] = messageID
				}
			}
		}
	case "user":
		var user struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(raw, &user); err != nil {
			return err
		}
		var blocks []struct {
			Type      string          `json:"type"`
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
			IsError   bool            `json:"is_error"`
		}
		// Plain-text user content is not a tool result; skip it.
		if json.Unmarshal(user.Message.Content, &blocks) != nil {
			return nil
		}
		for _, block := range blocks {
			messageID := d.toolMessages[block.ToolUseID]
			if block.Type != "tool_result" || messageID == "" {
				continue
			}
			delete(d.toolMessages, block.ToolUseID)
			if err := d.manager.attachToolResult(d.chatID, messageID, toolResultText(block.Content), block.IsError); err != nil {
				return err
			}
		}
	case "result":
		d.steerMu.Lock()
		if d.steering {
			request := d.steerRequest
			d.thinkingDeltas = false
			d.textDeltas = false
			if err := d.writeUserMessage(request); err != nil {
				content := d.assistant.String()
				d.assistant.Reset()
				finishErr := d.manager.finishTurn(d.chatID, content, err)
				d.steering = false
				d.steerMu.Unlock()
				return finishErr
			}
			d.steering = false
			d.steerMu.Unlock()
			return nil
		}
		d.steerMu.Unlock()
		d.thinkingDeltas = false
		d.textDeltas = false
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
		content := d.assistant.String()
		d.assistant.Reset()
		if content == "" {
			content = result.Result
		}
		var turnErr error
		if result.IsError {
			if content == "" && len(result.Errors) > 0 {
				content = result.Errors[0].Message
			}
			turnErr = errors.New(content)
		}
		if err := d.manager.finishTurn(d.chatID, content, turnErr); err != nil {
			return err
		}
	case "rate_limit_event":
		var limit struct {
			Info claudeRateLimitInfo `json:"rate_limit_info"`
		}
		if err := json.Unmarshal(raw, &limit); err != nil {
			return err
		}
		return d.storeQuota(limit.Info)
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

const toolInputLimit = 20000

func shortText(value string, limit int) string {
	runes := []rune(strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " "))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}

func (d *claudeRuntime) storeQuota(info claudeRateLimitInfo) error {
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
	updated := time.Now().UTC().Format(time.RFC3339)
	quota := connectorcli.Quota{Kind: "claude", Windows: windows, UpdatedAt: &updated}
	return d.manager.saveQuota(d.runtime.connectorID, quota)
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
