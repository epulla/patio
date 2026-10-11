package api

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"
	"my-tickets/internal/filetools"
)

const (
	localMaxToolCalls  = 20
	localMaxToolOutput = 32 << 10
	localSSELineLimit  = 4 << 20
	localToolNote      = "This model doesn't support tools; chatting without them."
	localToolLimitNote = "Tool call limit reached; stopping here."
)

type localRuntime struct {
	manager    *chatManager
	chatID     string
	model      string
	baseURL    string
	secret     string
	workdir    string
	ctx        context.Context
	cancel     context.CancelFunc
	session    *mcp.ClientSession
	cleanup    func()
	tools      []map[string]any
	mcpTools   map[string]string
	client     *http.Client
	mu         sync.Mutex
	turnCancel context.CancelFunc
	turnDone   chan struct{}
	closed     bool
	toolsOff   bool
}

func init() { runtimes["local"] = startLocal }

func startLocal(m *chatManager, c chat, _ string, _ *chatRuntime) (chatRuntimeDriver, error) {
	if c.ConnectorID == nil {
		return nil, errChatConnectorMissing
	}
	var kind, baseURL string
	if err := m.db.QueryRow(`SELECT kind,base_url FROM connector WHERE id=?`, *c.ConnectorID).Scan(&kind, &baseURL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errChatConnectorMissing
		}
		return nil, err
	}
	if kind != "local" {
		return nil, errChatUnsupported
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || !validConnectorURL(baseURL) {
		return nil, errors.New("invalid local model endpoint")
	}
	var secret string
	secret, err = keyring.Get(connectorService, connectorSecret(*c.ConnectorID))
	if errors.Is(err, keyring.ErrNotFound) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &localRuntime{
		manager: m, chatID: c.ID, model: c.Model, baseURL: strings.TrimRight(parsed.String(), "/"), secret: secret,
		workdir: c.Workdir, ctx: ctx, cancel: cancel, client: &http.Client{}, mcpTools: make(map[string]string),
	}
	for _, tool := range filetools.Tools() {
		d.tools = append(d.tools, map[string]any{"type": "function", "function": map[string]any{
			"name": "file__" + tool.Name, "description": tool.Description,
			"parameters": tool.Parameters,
		}})
	}
	spec, err := m.ticketMCPServerSpec()
	if err != nil {
		cancel()
		return nil, err
	}
	connectCtx, connectCancel := context.WithTimeout(ctx, 15*time.Second)
	defer connectCancel()
	connect := m.options.mcpConnect
	if connect == nil {
		connect = connectMCP
	}
	executable, ok := spec["command"].(string)
	if !ok {
		cancel()
		return nil, errors.New("ticket MCP spec has no command")
	}
	args, ok := spec["args"].([]string)
	if !ok {
		cancel()
		return nil, errors.New("ticket MCP spec has no args")
	}
	d.session, d.cleanup, err = connect(connectCtx, mcpServerSpec{Kind: "stdio", Command: executable, Args: args})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect ticket MCP: %w", err)
	}
	if d.cleanup == nil {
		d.cleanup = func() { _ = d.session.Close() }
	}
	if err := d.loadTools(ctx); err != nil {
		d.cleanupRuntime()
		cancel()
		return nil, fmt.Errorf("list ticket MCP tools: %w", err)
	}
	return d, nil
}

func (d *localRuntime) loadTools(ctx context.Context) error {
	cursor := ""
	seen := make(map[string]bool)
	for {
		result, err := d.session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return err
		}
		for _, tool := range result.Tools {
			if tool == nil || tool.Name == "" {
				continue
			}
			name := "ticket__" + tool.Name
			schema := tool.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			definition := map[string]any{"type": "function", "function": map[string]any{
				"name": name, "description": tool.Description, "parameters": schema,
			}}
			d.tools = append(d.tools, definition)
			d.mcpTools[name] = tool.Name
		}
		if result.NextCursor == "" {
			return nil
		}
		if seen[result.NextCursor] {
			return errors.New("ticket MCP repeated tools pagination cursor")
		}
		seen[result.NextCursor] = true
		cursor = result.NextCursor
	}
}

func (d *localRuntime) send(_ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return errors.New("local chat is closed")
	}
	if d.turnDone != nil {
		return errChatTurnRunning
	}
	ctx, cancel := context.WithCancel(d.ctx)
	d.turnCancel, d.turnDone = cancel, make(chan struct{})
	go d.runTurn(ctx)
	return nil
}

func (d *localRuntime) runTurn(ctx context.Context) {
	d.mu.Lock()
	done := d.turnDone
	d.mu.Unlock()
	defer close(done)
	text, err := d.completeTurn(ctx)
	d.mu.Lock()
	stopped := ctx.Err() != nil || d.closed
	if d.turnDone == done {
		d.turnDone, d.turnCancel = nil, nil
	}
	d.mu.Unlock()
	if stopped {
		return
	}
	if finishErr := d.manager.finishTurn(d.chatID, text, err); finishErr != nil {
		d.manager.emit(d.chatID, map[string]any{"type": "error", "message": finishErr.Error()})
	}
}

func (d *localRuntime) completeTurn(ctx context.Context) (string, error) {
	messages, err := d.history()
	if err != nil {
		return "", err
	}
	toolCalls := 0
	var fullText strings.Builder
	for {
		response, err := d.stream(ctx, messages, !d.toolsOff)
		if err != nil {
			return "", err
		}
		fullText.WriteString(response.text)
		if len(response.calls) == 0 {
			return fullText.String(), nil
		}
		messages = append(messages, map[string]any{"role": "assistant", "content": nullableText(response.text), "tool_calls": openAIToolCalls(response.calls)})
		limitReached := false
		for _, call := range response.calls {
			if toolCalls >= localMaxToolCalls {
				limitReached = true
				break
			}
			toolCalls++
			result, summary, err := d.runTool(ctx, call)
			if err != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				result = err.Error()
			}
			result = capToolOutput(result)
			if err := d.manager.emitTool(d.chatID, summary); err != nil {
				return "", err
			}
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": call.ID, "content": result})
		}
		if limitReached || toolCalls >= localMaxToolCalls {
			note := localToolLimitNote
			d.manager.emitDelta(d.chatID, "\n\n"+note)
			fullText.WriteString("\n\n" + note)
			return strings.TrimSpace(fullText.String()), nil
		}
	}
}

func (d *localRuntime) history() ([]map[string]any, error) {
	rows, err := d.manager.db.Query(`SELECT role,content FROM chat_message WHERE chat_id=? AND role IN ('user','assistant') ORDER BY created_at DESC,rowid DESC LIMIT 40`, d.chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type entry struct{ role, content string }
	var saved []entry
	for rows.Next() {
		var item entry
		if err := rows.Scan(&item.role, &item.content); err != nil {
			return nil, err
		}
		saved = append(saved, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	systemPrompt := "You work inside this chat's folder. Use file__ tools for sandboxed file reads and approved writes; ticket__ tools access my-tickets. Reads and ticket actions need no approval; file changes require approval."
	if d.toolsOff {
		systemPrompt = "You work inside this chat's folder, but this model does not support tools. Answer without file or ticket access."
	}
	messages := []map[string]any{{"role": "system", "content": systemPrompt}}
	for i := len(saved) - 1; i >= 0; i-- {
		messages = append(messages, map[string]any{"role": saved[i].role, "content": saved[i].content})
	}
	return messages, nil
}

type localCall struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

type localStreamResult struct {
	text  string
	calls []localCall
}

func (d *localRuntime) stream(ctx context.Context, messages []map[string]any, withTools bool) (localStreamResult, error) {
	body := map[string]any{"model": d.model, "stream": true, "messages": messages}
	if withTools {
		body["tools"] = d.tools
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return localStreamResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return localStreamResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if d.secret != "" {
		req.Header.Set("Authorization", "Bearer "+d.secret)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return localStreamResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		if readErr != nil {
			return localStreamResult{}, readErr
		}
		message := strings.TrimSpace(string(data))
		if withTools && resp.StatusCode >= 400 && resp.StatusCode < 500 && strings.Contains(strings.ToLower(message), "tool") {
			d.toolsOff = true
			if _, err := InsertChatMessage(d.manager.db, d.chatID, "error", localToolNote); err != nil {
				return localStreamResult{}, err
			}
			d.manager.emit(d.chatID, map[string]any{"type": "error", "message": localToolNote})
			return d.stream(ctx, messages, false)
		}
		return localStreamResult{}, fmt.Errorf("local chat completion returned %s: %s", resp.Status, message)
	}
	defer resp.Body.Close()
	return d.readSSE(ctx, resp.Body)
}

type localToolDelta struct {
	ID       string `json:"id"`
	Index    int    `json:"index"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type localChunk struct {
	Error   any `json:"error"`
	Choices []struct {
		Delta struct {
			Content   string           `json:"content"`
			ToolCalls []localToolDelta `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

func (d *localRuntime) readSSE(ctx context.Context, body io.Reader) (localStreamResult, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), localSSELineLimit)
	var result localStreamResult
	calls := make(map[int]*localCall)
	var data strings.Builder
	filter := thinkFilter{}
	finished := false
	consume := func(raw string) error {
		if raw == "[DONE]" {
			finished = true
			return nil
		}
		var chunk localChunk
		if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
			return fmt.Errorf("decode local SSE chunk: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("local model stream error: %v", chunk.Error)
		}
		for _, choice := range chunk.Choices {
			if text := filter.feed(choice.Delta.Content); text != "" {
				result.text += text
				d.manager.emitDelta(d.chatID, text)
			}
			for _, delta := range choice.Delta.ToolCalls {
				call := calls[delta.Index]
				if call == nil {
					call = &localCall{Index: delta.Index}
					calls[delta.Index] = call
				}
				if delta.ID != "" {
					call.ID = delta.ID
				}
				if delta.Function.Name != "" {
					call.Name += delta.Function.Name
				}
				call.Arguments += delta.Function.Arguments
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
		if line == "" && data.Len() > 0 {
			if err := consume(data.String()); err != nil {
				return localStreamResult{}, err
			}
			data.Reset()
			if finished {
				break
			}
		}
		if err := ctx.Err(); err != nil {
			return localStreamResult{}, err
		}
	}
	if !finished && data.Len() > 0 {
		if err := consume(data.String()); err != nil {
			return localStreamResult{}, err
		}
	}
	if err := scanner.Err(); err != nil {
		return localStreamResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return localStreamResult{}, err
	}
	if rest := filter.flush(); rest != "" {
		result.text += rest
		d.manager.emitDelta(d.chatID, rest)
	}
	indices := make([]int, 0, len(calls))
	for index := range calls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		call := *calls[index]
		if call.ID == "" || call.Name == "" {
			return localStreamResult{}, errors.New("local model returned incomplete tool call")
		}
		if call.Arguments == "" {
			call.Arguments = "{}"
		}
		result.calls = append(result.calls, call)
	}
	return result, nil
}

func openAIToolCalls(calls []localCall) []map[string]any {
	result := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		result = append(result, map[string]any{"id": call.ID, "type": "function", "function": map[string]string{"name": call.Name, "arguments": call.Arguments}})
	}
	return result
}

func nullableText(text string) any {
	if text == "" {
		return nil
	}
	return text
}

func (d *localRuntime) runTool(ctx context.Context, call localCall) (string, string, error) {
	input := json.RawMessage(call.Arguments)
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil || args == nil {
		return "", call.Name, fmt.Errorf("invalid arguments for %s", call.Name)
	}
	if strings.HasPrefix(call.Name, "file__") {
		name := strings.TrimPrefix(call.Name, "file__")
		var definition *filetools.Tool
		for _, candidate := range filetools.Tools() {
			if candidate.Name == name {
				copy := candidate
				definition = &copy
				break
			}
		}
		if definition == nil {
			return "", call.Name, fmt.Errorf("unknown file tool %q", name)
		}
		if definition.Write {
			allowed, err := d.manager.requestApproval(ctx, d.chatID, call.Name, input)
			if err != nil {
				return "", call.Name, err
			}
			if !allowed {
				return "User denied this action.", call.Name + " — " + filetools.Summary(name, input), nil
			}
		}
		result, err := filetools.Run(ctx, d.workdir, name, input)
		return result, call.Name + " — " + filetools.Summary(name, input), err
	}
	name, ok := d.mcpTools[call.Name]
	if !ok {
		return "", call.Name, fmt.Errorf("unknown ticket tool %q", call.Name)
	}
	result, err := d.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", call.Name, err
	}
	output, err := mcpToolOutput(result)
	if result.IsError && err == nil {
		err = errors.New(output)
	}
	return output, call.Name, err
}

func mcpToolOutput(result *mcp.CallToolResult) (string, error) {
	if result == nil {
		return "", errors.New("ticket tool returned no result")
	}
	var parts []string
	for _, item := range result.Content {
		if text, ok := item.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n"), nil
	}
	if result.StructuredContent != nil {
		data, err := json.Marshal(result.StructuredContent)
		return string(data), err
	}
	return "", nil
}

func capToolOutput(output string) string {
	if len(output) <= localMaxToolOutput {
		return output
	}
	const suffix = "\n[tool output truncated]"
	end := localMaxToolOutput - len(suffix)
	for end > 0 && !utf8.RuneStart(output[end]) {
		end--
	}
	return output[:end] + suffix
}

type thinkFilter struct {
	pending string
	inThink bool
}

func (f *thinkFilter) feed(input string) string {
	f.pending += input
	var out strings.Builder
	for f.pending != "" {
		marker := "<think>"
		if f.inThink {
			marker = "</think>"
		}
		if index := strings.Index(strings.ToLower(f.pending), marker); index >= 0 {
			if !f.inThink {
				out.WriteString(f.pending[:index])
			}
			f.pending = f.pending[index+len(marker):]
			f.inThink = !f.inThink
			continue
		}
		keep := len(marker) - 1
		if len(f.pending) <= keep {
			break
		}
		flush := len(f.pending) - keep
		if !f.inThink {
			out.WriteString(f.pending[:flush])
		}
		f.pending = f.pending[flush:]
	}
	return out.String()
}

func (f *thinkFilter) flush() string {
	defer func() { f.pending = "" }()
	if f.inThink {
		return ""
	}
	return f.pending
}

func (d *localRuntime) stop() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	if d.turnCancel != nil {
		d.turnCancel()
	}
	done := d.turnDone
	d.cancel()
	d.mu.Unlock()
	if done != nil {
		<-done
	}
	d.cleanupRuntime()
	return nil
}

func (d *localRuntime) close() error { return d.stop() }

func (d *localRuntime) cleanupRuntime() {
	d.mu.Lock()
	cleanup := d.cleanup
	d.cleanup = nil
	d.mu.Unlock()
	if cleanup != nil {
		cleanup()
	}
}
