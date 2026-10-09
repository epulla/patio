package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	connectorcli "my-tickets/internal/connector"
)

type codexClient interface {
	Call(context.Context, string, any, any) error
	Notifications() <-chan connectorcli.Notification
	Requests() <-chan connectorcli.Request
	Respond(json.RawMessage, any) error
	Close() error
}

type codexRuntime struct {
	manager   *chatManager
	chatID    string
	runtime   *chatRuntime
	client    codexClient
	threadID  string
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	turnID    string
	finished  bool
	closing   bool
	assistant strings.Builder
	items     map[string]json.RawMessage
}

func init() { runtimes["codex"] = startCodex }

func startCodex(m *chatManager, c chat, configDir string, rt *chatRuntime) (chatRuntimeDriver, error) {
	finder := m.options.findCodex
	if finder == nil {
		finder = func() (string, error) { return connectorcli.Find("codex") }
	}
	path, err := finder()
	if err != nil {
		return nil, fmt.Errorf("find codex: %w", err)
	}
	factory := m.options.codexStart
	if factory == nil {
		factory = func(ctx context.Context, path string, env []string, isolated bool, version, cwd string, overrides ...string) (codexClient, error) {
			return connectorcli.StartCodexInDir(ctx, path, env, isolated, version, cwd, overrides...)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	start := func(overrides ...string) (codexClient, error) {
		return factory(ctx, path, connectorEnv("codex", configDir), configDir != "", m.options.version, c.Workdir, overrides...)
	}
	client, err := start()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start codex: %w", err)
	}
	driver := &codexRuntime{manager: m, chatID: c.ID, runtime: rt, client: client, ctx: ctx, cancel: cancel, items: make(map[string]json.RawMessage)}
	if c.SessionID != nil && *c.SessionID != "" {
		driver.threadID = *c.SessionID
		var result json.RawMessage
		err = client.Call(ctx, "thread/resume", map[string]any{
			"threadId": driver.threadID, "approvalPolicy": "on-request", "sandbox": "read-only",
		}, &result)
	} else {
		var spec map[string]any
		spec, err = m.ticketMCPServerSpec()
		if err == nil {
			var result struct {
				Thread struct {
					ID string `json:"id"`
				} `json:"thread"`
			}
			params := map[string]any{
				"cwd": c.Workdir, "approvalPolicy": "on-request", "sandbox": "read-only",
				"config": map[string]any{"mcp_servers": map[string]any{"my-tickets": spec}},
			}
			if c.Model != "" {
				params["model"] = c.Model
			}
			err = client.Call(ctx, "thread/start", params, &result)
			if err != nil {
				_ = client.Close()
				overrides, overrideErr := codexMCPOverrides(spec)
				if overrideErr != nil {
					cancel()
					return nil, overrideErr
				}
				client, err = start(overrides...)
				if err == nil {
					driver.client = client
					params["config"] = map[string]any{}
					err = client.Call(ctx, "thread/start", params, &result)
				}
			}
			if err == nil {
				driver.threadID = result.Thread.ID
				if driver.threadID == "" {
					err = errors.New("Codex thread/start returned no thread id")
				}
			}
		}
	}
	if err == nil {
		err = m.saveSession(c.ID, driver.threadID)
	}
	if err != nil {
		cancel()
		_ = driver.client.Close()
		return nil, fmt.Errorf("start Codex thread: %w", err)
	}
	rt.driver = driver
	go driver.watch()
	return driver, nil
}

func codexMCPOverrides(spec map[string]any) ([]string, error) {
	command, ok := spec["command"].(string)
	if !ok {
		return nil, errors.New("Codex MCP server command is missing")
	}
	args, ok := spec["args"].([]string)
	if !ok {
		return nil, errors.New("Codex MCP server args are missing")
	}
	commandValue, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}
	argsValue, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	return []string{
		"mcp_servers.my-tickets.command=" + string(commandValue),
		"mcp_servers.my-tickets.args=" + string(argsValue),
	}, nil
}

func (d *codexRuntime) send(text string) error {
	d.mu.Lock()
	d.finished = false
	d.assistant.Reset()
	d.mu.Unlock()
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := d.client.Call(d.ctx, "turn/start", map[string]any{
		"threadId": d.threadID,
		"input":    []map[string]string{{"type": "text", "text": text}},
	}, &response); err != nil {
		return err
	}
	if response.Turn.ID == "" {
		return errors.New("Codex turn/start returned no turn id")
	}
	d.mu.Lock()
	d.turnID = response.Turn.ID
	d.mu.Unlock()
	return nil
}

func (d *codexRuntime) watch() {
	notifications, requests := d.client.Notifications(), d.client.Requests()
	for notifications != nil || requests != nil {
		select {
		case notification, ok := <-notifications:
			if !ok {
				notifications = nil
				continue
			}
			if err := d.handleNotification(notification); err != nil {
				d.finish(err)
			}
		case request, ok := <-requests:
			if !ok {
				requests = nil
				continue
			}
			go d.handleRequest(request)
		}
	}
	d.mu.Lock()
	closing := d.closing
	d.mu.Unlock()
	if !closing && d.manager.runtime(d.chatID).busy {
		d.finish(errors.New("Codex app-server exited"))
	}
}

func (d *codexRuntime) handleNotification(notification connectorcli.Notification) error {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(notification.Params, &params); err != nil {
		return fmt.Errorf("decode Codex %s notification: %w", notification.Method, err)
	}
	switch notification.Method {
	case "item/agentMessage/delta":
		var delta string
		if err := json.Unmarshal(params["delta"], &delta); err != nil {
			return err
		}
		d.mu.Lock()
		d.assistant.WriteString(delta)
		d.mu.Unlock()
		d.manager.emitDelta(d.chatID, delta)
	case "item/started":
		var item struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params["item"], &item); err != nil {
			return err
		}
		if item.ID != "" {
			d.mu.Lock()
			d.items[item.ID] = params["item"]
			d.mu.Unlock()
		}
	case "item/completed":
		var item struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(params["item"], &item); err != nil {
			return err
		}
		if item.ID != "" {
			d.mu.Lock()
			delete(d.items, item.ID)
			d.mu.Unlock()
		}
		if item.Type == "commandExecution" || item.Type == "fileChange" || item.Type == "mcpToolCall" {
			var data map[string]json.RawMessage
			if err := json.Unmarshal(params["item"], &data); err != nil {
				return err
			}
			if err := d.manager.emitTool(d.chatID, codexItemSummary(item.Type, data)); err != nil {
				return err
			}
		}
	case "turn/completed":
		var result struct {
			Turn struct {
				Status string `json:"status"`
				Error  any    `json:"error"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(notification.Params, &result); err != nil {
			return err
		}
		var turnErr error
		if result.Turn.Status == "failed" {
			turnErr = codexError(result.Turn.Error, "Codex turn failed")
		}
		return d.finish(turnErr)
	case "error":
		var result struct {
			WillRetry bool `json:"willRetry"`
			Error     any  `json:"error"`
		}
		if err := json.Unmarshal(notification.Params, &result); err != nil {
			return err
		}
		if !result.WillRetry {
			return d.finish(codexError(result.Error, "Codex app-server error"))
		}
	case "account/rateLimits/updated":
		var raw json.RawMessage
		if err := json.Unmarshal(notification.Params, &raw); err != nil {
			return err
		}
		windows, err := connectorcli.ParseCodexQuota(raw)
		if err != nil {
			return err
		}
		updated := time.Now().UTC().Format(time.RFC3339)
		return d.manager.saveQuota(d.runtime.connectorID, connectorcli.Quota{Kind: "codex", Windows: windows, UpdatedAt: &updated})
	}
	return nil
}

func (d *codexRuntime) handleRequest(request connectorcli.Request) {
	var result any
	switch request.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		toolName := "Run command"
		if request.Method == "item/fileChange/requestApproval" {
			toolName = "Edit files"
		}
		input, err := d.approvalInput(request.Params)
		if err != nil {
			d.finish(err)
			return
		}
		allowed, err := d.manager.requestApproval(d.ctx, d.chatID, toolName, input)
		if err != nil {
			d.finish(err)
			return
		}
		decision := "decline"
		if allowed {
			decision = "accept"
		}
		result = map[string]string{"decision": decision}
	default:
		result = map[string]string{"decision": "decline"}
	}
	if err := d.client.Respond(request.ID, result); err != nil {
		d.finish(fmt.Errorf("respond to Codex request: %w", err))
	}
}

func (d *codexRuntime) approvalInput(raw json.RawMessage) (json.RawMessage, error) {
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if input == nil {
		input = make(map[string]any)
	}
	var request struct {
		ItemID string `json:"itemId"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	itemID := request.ItemID
	if itemID != "" {
		d.mu.Lock()
		if item, ok := d.items[itemID]; ok {
			var enriched any
			if err := json.Unmarshal(item, &enriched); err != nil {
				d.mu.Unlock()
				return nil, err
			}
			input["item"] = enriched
		}
		d.mu.Unlock()
	}
	return json.Marshal(input)
}

func codexItemSummary(itemType string, data map[string]json.RawMessage) string {
	var details []string
	switch itemType {
	case "commandExecution":
		var command string
		_ = json.Unmarshal(data["command"], &command)
		details = append(details, command)
	case "fileChange":
		var changes []struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(data["changes"], &changes)
		for _, change := range changes {
			if change.Path != "" {
				details = append(details, change.Path)
			}
		}
	case "mcpToolCall":
		for _, key := range []string{"server", "toolName"} {
			var value string
			_ = json.Unmarshal(data[key], &value)
			details = append(details, value)
		}
	}
	return shortText(strings.TrimSpace(itemType+" "+strings.Join(details, " ")), 180)
}

func codexError(value any, fallback string) error {
	if value == nil {
		return errors.New(fallback)
	}
	if text, ok := value.(string); ok && text != "" {
		return errors.New(text)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New(fallback)
	}
	var detail struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &detail) == nil && detail.Message != "" {
		return errors.New(detail.Message)
	}
	return errors.New(string(data))
}

func (d *codexRuntime) finish(turnErr error) error {
	d.mu.Lock()
	if d.finished {
		d.mu.Unlock()
		return nil
	}
	d.finished = true
	text := d.assistant.String()
	d.assistant.Reset()
	d.mu.Unlock()
	return d.manager.finishTurn(d.chatID, text, turnErr)
}

func (d *codexRuntime) stop() error {
	d.mu.Lock()
	threadID, turnID := d.threadID, d.turnID
	d.mu.Unlock()
	var err error
	if turnID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = d.client.Call(ctx, "turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID}, nil)
		cancel()
	}
	return errors.Join(err, d.close())
}

func (d *codexRuntime) close() error {
	d.mu.Lock()
	if d.closing {
		d.mu.Unlock()
		return nil
	}
	d.closing = true
	d.mu.Unlock()
	d.cancel()
	err := d.client.Close()
	return err
}
