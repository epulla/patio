package api

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/zalando/go-keyring"
	connectorcli "patio/internal/connector"
)

const copilotAuthError = "Sign in to Copilot: run `copilot login` (Connectors → Log in)"

func init() { runtimes["copilot"] = startCopilot }

type copilotRuntime struct {
	manager     *chatManager
	chatID      string
	workdir     string
	runtime     *chatRuntime
	process     chatProcess
	client      *acpClient
	processDone chan struct{}
	sessionID   string
	ctx         context.Context
	cancel      context.CancelFunc
	assistantMu sync.Mutex
	assistant   strings.Builder
	toolMu      sync.Mutex
	toolLabels  map[string]string
	replaying   atomic.Bool
	stopped     atomic.Bool
	closing     atomic.Bool
	authFailed  bool
	imagePrompt bool
	external    []mcpServerSpec
}

func startCopilot(m *chatManager, c chat, configDir string, rt *chatRuntime) (chatRuntimeDriver, error) {
	finder := m.options.findCopilot
	if finder == nil {
		finder = func() (string, error) { return connectorcli.Find("copilot") }
	}
	path, err := finder()
	if err != nil {
		return nil, fmt.Errorf("find copilot: %w", err)
	}
	external, err := m.chatMCPServers(c.ID)
	if err != nil {
		return nil, err
	}
	secret, err := keyring.Get(connectorService, connectorSecret(*c.ConnectorID))
	if errors.Is(err, keyring.ErrNotFound) {
		secret, err = "", nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Copilot credential: %w", err)
	}
	factory := m.options.processFactory
	if factory == nil {
		factory = startOSChatProcess
	}
	args := []string{"--acp", "--stdio"}
	if !c.Thinking {
		args = append(args, "--reasoning-effort", "low")
	}
	process, err := factory(path, args, connectorcli.Env("copilot", configDir, secret), c.Workdir)
	if err != nil {
		return nil, fmt.Errorf("start copilot: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &copilotRuntime{manager: m, chatID: c.ID, workdir: c.Workdir, runtime: rt, process: process, processDone: make(chan struct{}), ctx: ctx, cancel: cancel, external: external}
	d.client = newACPClient(process.Stdin(), process.Stdout(), d.handleMessage)
	go d.watch()
	if err := d.initialize(c); err != nil {
		_ = d.close()
		return nil, err
	}
	return d, nil
}

func (d *copilotRuntime) initialize(c chat) error {
	var initialized json.RawMessage
	if err := d.client.call("initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]bool{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
	}, &initialized); err != nil {
		return fmt.Errorf("initialize Copilot ACP: %w", err)
	}
	imagePrompt, err := copilotImageCapability(initialized)
	if err != nil {
		return fmt.Errorf("decode Copilot ACP capabilities: %w", err)
	}
	d.imagePrompt = imagePrompt
	mcp, err := d.manager.ticketMCPServerSpec()
	if err != nil {
		return err
	}
	d.replaying.Store(true)
	var session json.RawMessage
	method := "session/new"
	params := map[string]any{"cwd": c.Workdir, "mcpServers": copilotMCPServers(mcp, d.external)}
	if c.SessionID != nil && *c.SessionID != "" {
		method = "session/load"
		params["sessionId"] = *c.SessionID
		d.sessionID = *c.SessionID
	}
	err = d.client.call(method, params, &session)
	if err != nil {
		d.replaying.Store(false)
		var rpcErr *acpError
		if method == "session/new" && errors.As(err, &rpcErr) && rpcErr.Code == -32000 {
			d.authFailed = true
			go d.manager.finishTurn(d.chatID, "", errors.New(copilotAuthError))
			return nil
		}
		return fmt.Errorf("%s: %w", method, err)
	}
	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(session, &result); err != nil {
		d.replaying.Store(false)
		return fmt.Errorf("decode Copilot session: %w", err)
	}
	if result.SessionID == "" {
		d.replaying.Store(false)
		return errors.New("Copilot ACP returned empty sessionId")
	}
	d.sessionID = result.SessionID
	if err := d.manager.saveSession(d.chatID, result.SessionID); err != nil {
		d.replaying.Store(false)
		return err
	}
	d.replaying.Store(false)
	return nil
}

func copilotImageCapability(initialized json.RawMessage) (bool, error) {
	var capabilities struct {
		AgentCapabilities struct {
			PromptCapabilities struct {
				Image bool `json:"image"`
			} `json:"promptCapabilities"`
		} `json:"agentCapabilities"`
	}
	if err := json.Unmarshal(initialized, &capabilities); err != nil {
		return false, err
	}
	return capabilities.AgentCapabilities.PromptCapabilities.Image, nil
}

func (d *copilotRuntime) send(request ChatSendRequest) error {
	if d.authFailed {
		return nil
	}
	files, err := loadChatAttachmentFiles(d.workdir, request.Attachments)
	if err != nil {
		return err
	}
	if !d.imagePrompt && hasChatImage(files) {
		if err := saveChatImageNote(d.manager, d.chatID, "Copilot can't take images; sent the file path instead"); err != nil {
			return err
		}
	}
	d.assistantMu.Lock()
	d.assistant.Reset()
	d.assistantMu.Unlock()
	result, err := d.client.callAsync("session/prompt", map[string]any{
		"sessionId": d.sessionID,
		"prompt":    copilotPromptContent(request.Text, files, d.imagePrompt),
	})
	if err != nil {
		return err
	}
	go func() {
		got := <-result
		if got.err != nil {
			if !d.stopped.Load() {
				d.finish("", got.err)
			}
			return
		}
		d.finishPrompt(got.result)
	}()
	return nil
}

func hasChatImage(files []chatAttachmentFile) bool {
	for _, file := range files {
		if file.Kind == "image" {
			return true
		}
	}
	return false
}

func copilotPromptContent(text string, files []chatAttachmentFile, imageSupport bool) []any {
	content := make([]any, 0, len(files)+1)
	prompt := promptWithAttachmentPaths(text, files, imageSupport)
	if prompt != "" {
		content = append(content, map[string]string{"type": "text", "text": prompt})
	}
	if imageSupport {
		for _, file := range files {
			if file.Kind == "image" {
				content = append(content, map[string]string{"type": "image", "mimeType": file.MIME, "data": base64.StdEncoding.EncodeToString(file.Data)})
			}
		}
	}
	return content
}

func (d *copilotRuntime) finishPrompt(result json.RawMessage) {
	var response struct {
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		d.finish("", fmt.Errorf("decode Copilot prompt response: %w", err))
		return
	}
	d.assistantMu.Lock()
	text := d.assistant.String()
	d.assistant.Reset()
	d.assistantMu.Unlock()
	if response.StopReason == "refusal" {
		d.finish("", errors.New("Copilot refused this request"))
		return
	}
	d.finish(text, nil)
}

func (d *copilotRuntime) finish(text string, err error) {
	if !d.stopped.Load() {
		_ = d.manager.finishTurn(d.chatID, text, err)
	}
}

func (d *copilotRuntime) handleMessage(message acpMessage) {
	if message.Method == "session/update" {
		if d.replaying.Load() {
			return
		}
		d.handleUpdate(message.Params)
		return
	}
	if message.Method == "session/request_permission" {
		go d.handlePermission(message)
		return
	}
	if message.Method != "" && message.ID != nil {
		_ = d.client.respondError(message.ID, -32601, "Method not found")
	}
}

func (d *copilotRuntime) handleUpdate(raw json.RawMessage) {
	var params struct {
		Update struct {
			Type          string `json:"type"`
			SessionUpdate string `json:"sessionUpdate"`
			Content       struct {
				Text string `json:"text"`
			} `json:"content"`
			Title      string `json:"title"`
			Kind       string `json:"kind"`
			ToolCallID string `json:"toolCallId"`
			Status     string `json:"status"`
			ToolCall   struct {
				ToolCallID string `json:"toolCallId"`
				Title      string `json:"title"`
				Kind       string `json:"kind"`
			} `json:"toolCall"`
		} `json:"update"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return
	}
	switch params.Update.SessionUpdate {
	case "agent_message_chunk":
		text := params.Update.Content.Text
		if text != "" {
			d.assistantMu.Lock()
			d.assistant.WriteString(text)
			d.assistantMu.Unlock()
			d.manager.emitDelta(d.chatID, text)
		}
	case "tool_call":
		label := params.Update.Title
		if label == "" {
			label = params.Update.Kind
		}
		toolCallID := params.Update.ToolCallID
		if params.Update.ToolCall.ToolCallID != "" {
			toolCallID = params.Update.ToolCall.ToolCallID
		}
		if label == "" {
			label = params.Update.ToolCall.Title
		}
		if label == "" {
			label = params.Update.ToolCall.Kind
		}
		if label != "" {
			if toolCallID != "" {
				d.toolMu.Lock()
				if d.toolLabels == nil {
					d.toolLabels = make(map[string]string)
				}
				d.toolLabels[toolCallID] = label
				d.toolMu.Unlock()
			}
			_ = d.manager.emitTool(d.chatID, label)
		}
	case "tool_call_update":
		if params.Update.Status == "failed" {
			label := params.Update.Title
			if label == "" {
				label = params.Update.Kind
			}
			toolCallID := params.Update.ToolCallID
			if params.Update.ToolCall.ToolCallID != "" {
				toolCallID = params.Update.ToolCall.ToolCallID
			}
			if label == "" && toolCallID != "" {
				d.toolMu.Lock()
				label = d.toolLabels[toolCallID]
				d.toolMu.Unlock()
			}
			if label != "" {
				_ = d.manager.emitTool(d.chatID, label+" failed")
			}
		}
	}
}

func (d *copilotRuntime) handlePermission(message acpMessage) {
	var params struct {
		ToolCall struct {
			Title string `json:"title"`
			Kind  string `json:"kind"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Name     string `json:"name"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil {
		_ = d.client.respondError(message.ID, -32602, "Invalid params")
		return
	}
	toolName := params.ToolCall.Title
	if toolName == "" {
		toolName = params.ToolCall.Kind
	}
	allowed := isTicketMCPTool(toolName)
	var err error
	if !allowed {
		allowed, err = d.manager.requestApproval(d.ctx, d.chatID, toolName, message.Params)
	}
	if err != nil {
		_ = d.client.respondError(message.ID, -32603, err.Error())
		return
	}
	kind := "reject_once"
	if allowed {
		kind = "allow_once"
	}
	option := findACPOption(params.Options, kind)
	if option == "" {
		_ = d.client.respond(message.ID, map[string]any{"outcome": map[string]string{"outcome": "cancelled"}})
		return
	}
	_ = d.client.respond(message.ID, map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": option}})
}

func isTicketMCPTool(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "patio") || strings.HasPrefix(name, "mcp__patio__")
}

func findACPOption(options []struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}, wanted string) string {
	for _, option := range options {
		if option.OptionID == wanted {
			return option.OptionID
		}
	}
	for _, option := range options {
		if option.Kind == wanted {
			return option.OptionID
		}
	}
	fallback := "allow_always"
	if strings.HasPrefix(wanted, "reject") {
		fallback = "reject_always"
	}
	for _, option := range options {
		if option.OptionID == fallback || option.Kind == fallback {
			return option.OptionID
		}
	}
	return ""
}

func (d *copilotRuntime) stop() error {
	d.stopped.Store(true)
	var session sql.NullString
	if err := d.manager.db.QueryRow(`SELECT session_id FROM chat WHERE id=?`, d.chatID).Scan(&session); err != nil {
		return err
	}
	if session.Valid {
		err := d.client.notify("session/cancel", map[string]string{"sessionId": session.String})
		return errors.Join(err, d.terminate())
	}
	return d.terminate()
}

func (d *copilotRuntime) close() error { return d.terminate() }

func (d *copilotRuntime) terminate() error {
	d.closing.Store(true)
	d.cancel()
	select {
	case <-d.processDone:
		return nil
	default:
	}
	_ = d.process.Kill()
	<-d.processDone
	return nil
}

func (d *copilotRuntime) watch() {
	go func() { _, _ = io.Copy(io.Discard, d.process.Stderr()) }()
	<-d.client.done
	_ = d.process.Wait()
	close(d.processDone)
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
	if !expected && unfinished && !d.stopped.Load() && !d.closing.Load() {
		_ = d.manager.finishTurn(d.chatID, "", errors.New("Copilot ACP process exited unexpectedly"))
	}
}

type acpError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *acpError) Error() string { return fmt.Sprintf("ACP error %d: %s", e.Code, e.Message) }

type acpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *acpError       `json:"error,omitempty"`
}

type acpResponse struct {
	result json.RawMessage
	err    error
}

type acpClient struct {
	stdin   io.Writer
	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  uint64
	pending map[string]chan acpResponse
	handler func(acpMessage)
	done    chan struct{}
}

func newACPClient(stdin io.Writer, stdout io.Reader, handler func(acpMessage)) *acpClient {
	c := &acpClient{stdin: stdin, pending: make(map[string]chan acpResponse), handler: handler, done: make(chan struct{})}
	go c.read(stdout)
	return c
}

func (c *acpClient) call(method string, params any, result any) error {
	response, err := c.callAsync(method, params)
	if err != nil {
		return err
	}
	select {
	case got := <-response:
		if got.err != nil {
			return got.err
		}
		if result != nil {
			return json.Unmarshal(got.result, result)
		}
		return nil
	case <-c.done:
		return errors.New("Copilot ACP process closed")
	}
}

func (c *acpClient) callAsync(method string, params any) (<-chan acpResponse, error) {
	c.mu.Lock()
	c.nextID++
	id := json.RawMessage(fmt.Sprintf("%d", c.nextID))
	key := string(id)
	response := make(chan acpResponse, 1)
	c.pending[key] = response
	c.mu.Unlock()
	if err := c.write(acpMessage{JSONRPC: "2.0", ID: id, Method: method, Params: marshalACPParams(params)}); err != nil {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return nil, err
	}
	return response, nil
}

func marshalACPParams(params any) json.RawMessage {
	data, _ := json.Marshal(params)
	return data
}

func (c *acpClient) notify(method string, params any) error {
	return c.write(acpMessage{JSONRPC: "2.0", Method: method, Params: marshalACPParams(params)})
}

func (c *acpClient) respond(id json.RawMessage, result any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{"2.0", id, data})
}

func (c *acpClient) respondError(id json.RawMessage, code int, message string) error {
	return c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   *acpError       `json:"error"`
	}{"2.0", id, &acpError{Code: code, Message: message}})
}

func (c *acpClient) write(message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(append(data, '\n'))
	return err
}

func (c *acpClient) read(stdout io.Reader) {
	defer close(c.done)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), chatLineLimit)
	for scanner.Scan() {
		var message acpMessage
		if json.Unmarshal(scanner.Bytes(), &message) != nil || message.JSONRPC != "2.0" {
			continue
		}
		if message.Method != "" {
			if c.handler != nil {
				c.handler(message)
			}
			continue
		}
		key := string(message.ID)
		c.mu.Lock()
		response := c.pending[key]
		delete(c.pending, key)
		c.mu.Unlock()
		if response != nil {
			var responseErr error
			if message.Error != nil {
				responseErr = message.Error
			}
			response <- acpResponse{result: message.Result, err: responseErr}
		}
	}
	c.mu.Lock()
	for key, response := range c.pending {
		delete(c.pending, key)
		response <- acpResponse{err: errors.New("Copilot ACP process closed")}
	}
	c.mu.Unlock()
}
