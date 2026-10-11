package api

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
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
	localMaxToolCalls   = 20
	localMaxToolOutput  = 32 << 10
	localStoredToolMax  = 2 << 10
	localSSELineLimit   = 4 << 20
	localHistoryBudget  = 1500 * 4
	localHistoryLimit   = 40
	localToolsMaxBytes  = 8 << 10
	localDefaultContext = 4096
	localToolNote       = "This model doesn't support tools; chatting without them."
	localToolLimitNote  = "Tool call limit reached; stopping here."
	localContextNote    = "Conversation too long for this model's context; older messages were left out."
	localInvalidReply   = "I couldn't produce a reply. Please try again."
)

type localRuntime struct {
	manager        *chatManager
	chatID         string
	model          string
	baseURL        string
	ollamaURL      string
	ollama         bool
	secret         string
	workdir        string
	ctx            context.Context
	cancel         context.CancelFunc
	session        *mcp.ClientSession
	cleanup        func()
	tools          []map[string]any
	mcpTools       map[string]string
	client         *http.Client
	mu             sync.Mutex
	turnCancel     context.CancelFunc
	turnDone       chan struct{}
	turnRequest    ChatSendRequest
	closed         bool
	toolsOff       bool
	contextSize    int
	contextChecked bool
}

var localVisionCapabilities sync.Map

func init() { runtimes["local"] = startLocal }

func startLocal(m *chatManager, c chat, _ string, _ *chatRuntime) (chatRuntimeDriver, error) {
	if c.ConnectorID == nil {
		return nil, errChatConnectorMissing
	}
	var kind, baseURL, connectorModel string
	if err := m.db.QueryRow(`SELECT kind,base_url,model FROM connector WHERE id=?`, *c.ConnectorID).Scan(&kind, &baseURL, &connectorModel); err != nil {
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
	model := c.Model
	if model == "" {
		model = connectorModel
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &localRuntime{
		manager: m, chatID: c.ID, model: model, baseURL: strings.TrimRight(parsed.String(), "/"), secret: secret,
		workdir: c.Workdir, ctx: ctx, cancel: cancel, client: &http.Client{}, mcpTools: make(map[string]string),
	}
	d.ollamaURL = ollamaOrigin(d.baseURL)
	d.detectOllama()
	for _, tool := range filetools.Tools() {
		definition, err := localToolDefinition("file__"+tool.Name, tool.Description, tool.Parameters)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("compact file tool %s: %w", tool.Name, err)
		}
		d.tools = append(d.tools, definition)
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
	d.tools, err = compactLocalTools(d.tools)
	if err != nil {
		d.cleanupRuntime()
		cancel()
		return nil, err
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
			definition, err := localToolDefinition(name, tool.Description, schema)
			if err != nil {
				return fmt.Errorf("compact ticket tool %s: %w", tool.Name, err)
			}
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

func ollamaOrigin(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(strings.ToLower(trimmed), "/v1") {
		trimmed = trimmed[:len(trimmed)-3]
	}
	return strings.TrimRight(trimmed, "/")
}

func (d *localRuntime) detectOllama() {
	ctx, cancel := context.WithTimeout(d.ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.ollamaURL+"/api/version", nil)
	if err != nil {
		return
	}
	if d.secret != "" {
		req.Header.Set("Authorization", "Bearer "+d.secret)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	d.ollama = resp.StatusCode >= 200 && resp.StatusCode < 300
}

func localToolDefinition(name, description string, parameters any) (map[string]any, error) {
	description = strings.TrimSpace(strings.SplitN(strings.SplitN(description, "\n", 2)[0], ". ", 2)[0])
	if len(description) > 100 {
		limit := 97
		for limit > 0 && !utf8.RuneStart(description[limit]) {
			limit--
		}
		description = description[:limit] + "..."
	}
	data, err := json.Marshal(parameters)
	if err != nil {
		return nil, err
	}
	var schemaValue any
	if err := json.Unmarshal(data, &schemaValue); err != nil {
		return nil, err
	}
	schemaValue = compactLocalSchema(schemaValue)
	return map[string]any{"type": "function", "function": map[string]any{
		"name": name, "description": description, "parameters": schemaValue,
	}}, nil
}

func compactLocalSchema(value any) any {
	switch item := value.(type) {
	case map[string]any:
		for key, nested := range item {
			if key == "description" {
				delete(item, key)
				continue
			}
			item[key] = compactLocalSchema(nested)
		}
	case []any:
		for i, nested := range item {
			item[i] = compactLocalSchema(nested)
		}
	}
	return value
}

func compactLocalTools(tools []map[string]any) ([]map[string]any, error) {
	compact := func(input []map[string]any) ([]map[string]any, error) {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		if len(data) > localToolsMaxBytes {
			return nil, fmt.Errorf("local tool definitions exceed %d bytes", localToolsMaxBytes)
		}
		return input, nil
	}
	result, err := compact(tools)
	if err == nil {
		return result, nil
	}
	filtered := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		function, _ := tool["function"].(map[string]any)
		name, _ := function["name"].(string)
		if strings.Contains(name, "calendar") || name == "file__duplicate" {
			continue
		}
		filtered = append(filtered, tool)
	}
	return compact(filtered)
}

func (d *localRuntime) send(request ChatSendRequest) error {
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
	d.turnRequest = request
	go d.runTurn(ctx)
	return nil
}

func (d *localRuntime) runTurn(ctx context.Context) {
	d.mu.Lock()
	done := d.turnDone
	request := d.turnRequest
	d.mu.Unlock()
	defer close(done)
	text, err := d.completeTurn(ctx, request)
	if err != nil {
		err = errors.New(d.redact(err.Error()))
	}
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

func (d *localRuntime) completeTurn(ctx context.Context, request ChatSendRequest) (string, error) {
	var thinking bool
	if err := d.manager.db.QueryRow(`SELECT thinking FROM chat WHERE id=?`, d.chatID).Scan(&thinking); err != nil {
		return "", err
	}
	files, err := loadChatAttachmentFiles(d.workdir, request.Attachments)
	if err != nil {
		return "", err
	}
	images := make([]chatAttachmentFile, 0, len(files))
	for _, file := range files {
		if file.Kind == "image" {
			images = append(images, file)
		}
	}
	includeImages := len(images) > 0
	if d.ollama && includeImages {
		includeImages = d.ollamaVision(ctx)
		if !includeImages {
			if err := saveChatImageNote(d.manager, d.chatID, localImageUnsupportedNote); err != nil {
				return "", err
			}
		}
	}
	messages, err := d.historyForToolsWithAttachments(includeImages)
	if err != nil {
		return "", err
	}
	if !includeImages {
		images = nil
	}
	toolCalls := 0
	contextNoted := false
	var fullText strings.Builder
	retriedWithoutTools := false
	retriedWithoutImages := false
	for {
		withTools := !d.toolsOff && !retriedWithoutTools
		response, err := d.streamWithImages(ctx, messages, withTools, thinking, images)
		if err != nil {
			if errors.Is(err, errLocalUnsupportedImage) && len(images) > 0 && !retriedWithoutImages {
				if err := appendCurrentImagePaths(messages, images); err != nil {
					return "", err
				}
				if err := saveChatImageNote(d.manager, d.chatID, localImageUnsupportedNote); err != nil {
					return "", err
				}
				images = nil
				retriedWithoutImages = true
				continue
			}
			return "", err
		}
		if d.ollama && !contextNoted && response.promptEvalCount > 0 {
			contextSize := d.ollamaContextSize(ctx)
			if response.promptEvalCount >= contextSize {
				if _, err := InsertChatMessage(d.manager.db, d.chatID, "error", localContextNote); err != nil {
					return "", err
				}
				d.manager.emit(d.chatID, map[string]any{"type": "error", "message": localContextNote})
				contextNoted = true
			}
		}
		if len(response.calls) == 0 {
			call, bareJSON, validCall := d.textToolCall(response.text)
			if validCall && withTools {
				response.calls = []localCall{call}
				response.text = ""
			} else if bareJSON || strings.TrimSpace(response.text) == "" {
				if !retriedWithoutTools {
					retriedWithoutTools = true
					continue
				}
				return localInvalidReply, nil
			}
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
				result = "Error: " + strings.TrimPrefix(err.Error(), "Error: ")
			}
			result = capToolOutput(d.redact(result))
			if err := d.manager.emitToolResult(d.chatID, summary, capStoredToolResult(result)); err != nil {
				return "", err
			}
			toolMessage := map[string]any{"role": "tool", "tool_call_id": call.ID, "content": result}
			if d.ollama {
				toolMessage["tool_name"] = call.Name
			}
			messages = append(messages, toolMessage)
		}
		if limitReached || toolCalls >= localMaxToolCalls {
			note := localToolLimitNote
			d.manager.emitDelta(d.chatID, "\n\n"+note)
			fullText.WriteString("\n\n" + note)
			return strings.TrimSpace(fullText.String()), nil
		}
		retriedWithoutTools = false
	}
}

func (d *localRuntime) textToolCall(content string) (localCall, bool, bool) {
	trimmed := strings.TrimSpace(content)
	jsonText := trimmed
	if strings.HasPrefix(jsonText, "```json") {
		jsonText = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(jsonText, "```json"), "```"))
	} else if strings.HasPrefix(jsonText, "```\n") {
		jsonText = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(jsonText, "```\n"), "```"))
	}
	if !json.Valid([]byte(jsonText)) {
		return localCall{}, false, false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonText), &object); err != nil || object == nil {
		return localCall{}, true, false
	}
	var name string
	if err := json.Unmarshal(object["name"], &name); err != nil || name == "" {
		return localCall{}, true, false
	}
	var argsRaw json.RawMessage
	if len(object["arguments"]) > 0 {
		argsRaw = object["arguments"]
	} else {
		argsRaw = object["parameters"]
	}
	var args map[string]any
	if len(argsRaw) == 0 || json.Unmarshal(argsRaw, &args) != nil || args == nil {
		return localCall{}, true, false
	}
	known := make(map[string]string)
	aliases := make(map[string]string)
	for _, definition := range d.tools {
		function, _ := definition["function"].(map[string]any)
		toolName, _ := function["name"].(string)
		if toolName == "" {
			continue
		}
		known[toolName] = toolName
		if strings.HasPrefix(toolName, "file__") || strings.HasPrefix(toolName, "ticket__") {
			alias := toolName[strings.Index(toolName, "__")+2:]
			if _, exists := aliases[alias]; exists {
				aliases[alias] = ""
			} else {
				aliases[alias] = toolName
			}
		}
	}
	if fullName, exists := known[name]; exists {
		name = fullName
	} else if fullName, exists := aliases[name]; exists && fullName != "" {
		name = fullName
	} else {
		return localCall{}, true, false
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return localCall{}, true, false
	}
	return localCall{ID: "text-tool-call", Name: name, Arguments: string(encoded)}, true, true
}

func (d *localRuntime) history() ([]map[string]any, error) {
	return d.historyForTools()
}

func (d *localRuntime) historyForTools() ([]map[string]any, error) {
	return d.historyForToolsWithAttachments(false)
}

func (d *localRuntime) historyForToolsWithAttachments(currentImagesIncluded bool) ([]map[string]any, error) {
	rows, err := d.manager.db.Query(`SELECT role,content,attachments FROM chat_message WHERE chat_id=? AND role IN ('user','assistant') ORDER BY created_at DESC,rowid DESC LIMIT ?`, d.chatID, localHistoryLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type entry struct {
		role, content string
		attachments   []ChatAttachment
	}
	var saved []entry
	for rows.Next() {
		var item entry
		var raw string
		if err := rows.Scan(&item.role, &item.content, &raw); err != nil {
			return nil, err
		}
		item.attachments, err = decodeChatAttachments(raw)
		if err != nil {
			return nil, fmt.Errorf("decode local history attachments: %w", err)
		}
		if item.role == "user" && len(item.attachments) > 0 {
			lines := make([]string, 0, len(item.attachments))
			current := len(saved) == 0
			for _, attachment := range item.attachments {
				if current && currentImagesIncluded && attachment.Kind == "image" {
					continue
				}
				path, err := chatAttachmentAbsolutePath(d.workdir, attachment.Name)
				if err != nil {
					return nil, err
				}
				lines = append(lines, attachmentPathLine(path))
			}
			item.content = appendPromptLines(item.content, lines)
		}
		saved = append(saved, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	folder := "a private scratch folder"
	var folderPath sql.NullString
	if err := d.manager.db.QueryRow(`SELECT folder FROM chat WHERE id=?`, d.chatID).Scan(&folderPath); err != nil {
		return nil, err
	}
	if folderPath.Valid {
		folder = filepath.Base(filepath.Clean(folderPath.String))
	}
	systemPrompt := localSystemPrompt(folder, !d.toolsOff)
	messages := []map[string]any{{"role": "system", "content": systemPrompt}}
	if len(saved) == 0 {
		return messages, nil
	}
	selected := []entry{saved[0]}
	used := len(saved[0].content)
	for i := 1; i < len(saved); i++ {
		if used+len(saved[i].content) > localHistoryBudget {
			continue
		}
		selected = append(selected, saved[i])
		used += len(saved[i].content)
	}
	for i := len(selected) - 1; i >= 0; i-- {
		messages = append(messages, map[string]any{"role": selected[i].role, "content": selected[i].content})
	}
	return messages, nil
}

func appendCurrentImagePaths(messages []map[string]any, images []chatAttachmentFile) error {
	if len(messages) == 0 {
		return errors.New("local history has no user prompt")
	}
	last := messages[len(messages)-1]
	content, ok := last["content"].(string)
	if !ok {
		return fmt.Errorf("local history user content has type %T", last["content"])
	}
	lines := make([]string, 0, len(images))
	for _, image := range images {
		lines = append(lines, attachmentPathLine(image.Path))
	}
	last["content"] = appendPromptLines(content, lines)
	return nil
}

func localSystemPrompt(folder string, toolsEnabled bool) string {
	today := time.Now().In(time.Local).Format("2006-01-02")
	if !toolsEnabled {
		return fmt.Sprintf("Reply normally. Tools unavailable; do not claim file or ticket access. Today's date (local): %s.", today)
	}
	return fmt.Sprintf("Reply normally. Use tools only when user asks about files, this folder (%s), or tickets. Never describe tool calls in text. Examples: \"Hi\" → \"Hi!\" (no tool); \"list my files\" → call file__list_dir; \"what boards do I have?\" → call ticket__list_boards. Today's date (local): %s.", folder, today)
}

type localCall struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

type localStreamResult struct {
	text            string
	calls           []localCall
	promptEvalCount int
}

func (d *localRuntime) stream(ctx context.Context, messages []map[string]any, withTools, thinking bool) (localStreamResult, error) {
	return d.streamWithImages(ctx, messages, withTools, thinking, nil)
}

var errLocalUnsupportedImage = errors.New("local model rejected image input")

func (d *localRuntime) streamWithImages(ctx context.Context, messages []map[string]any, withTools, thinking bool, images []chatAttachmentFile) (localStreamResult, error) {
	if d.ollama {
		return d.streamOllamaWithImages(ctx, messages, withTools, thinking, images)
	}
	requestMessages := openAIMessagesWithImages(messages, thinking, images)
	body := map[string]any{"model": d.model, "stream": true, "messages": requestMessages}
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
		if len(images) > 0 && isUnsupportedLocalImageResponse(resp.StatusCode, message) {
			return localStreamResult{}, errLocalUnsupportedImage
		}
		if withTools && resp.StatusCode >= 400 && resp.StatusCode < 500 && strings.Contains(strings.ToLower(message), "tool") {
			d.toolsOff = true
			if _, err := InsertChatMessage(d.manager.db, d.chatID, "error", localToolNote); err != nil {
				return localStreamResult{}, err
			}
			d.manager.emit(d.chatID, map[string]any{"type": "error", "message": localToolNote})
			return d.streamWithImages(ctx, messages, false, thinking, images)
		}
		return localStreamResult{}, fmt.Errorf("local chat completion returned %s: %s", resp.Status, message)
	}
	defer resp.Body.Close()
	return d.readSSE(ctx, resp.Body)
}

func openAIMessagesWithImages(messages []map[string]any, thinking bool, images []chatAttachmentFile) []map[string]any {
	result := openAIMessages(messages, thinking)
	if len(images) == 0 {
		return result
	}
	for i := len(result) - 1; i >= 0; i-- {
		if result[i]["role"] != "user" {
			continue
		}
		text, _ := result[i]["content"].(string)
		content := make([]any, 0, len(images)+1)
		if text != "" {
			content = append(content, map[string]string{"type": "text", "text": text})
		}
		for _, image := range images {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": chatImageDataURL(image)}})
		}
		result[i]["content"] = content
		break
	}
	return result
}

func isUnsupportedLocalImageResponse(status int, body string) bool {
	if status < 400 || status >= 500 {
		return false
	}
	body = strings.ToLower(body)
	for _, marker := range []string{"image", "vision", "multimodal", "unsupported input", "content type"} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

func openAIMessages(messages []map[string]any, thinking bool) []map[string]any {
	copyMessages := make([]map[string]any, len(messages))
	for i, message := range messages {
		copyMessages[i] = make(map[string]any, len(message))
		for key, value := range message {
			copyMessages[i][key] = value
		}
	}
	if !thinking {
		for i := len(copyMessages) - 1; i >= 0; i-- {
			if copyMessages[i]["role"] == "user" {
				content, _ := copyMessages[i]["content"].(string)
				copyMessages[i]["content"] = content + " /no_think"
				break
			}
		}
	}
	return copyMessages
}

func (d *localRuntime) streamOllama(ctx context.Context, messages []map[string]any, withTools, thinking bool) (localStreamResult, error) {
	return d.streamOllamaWithImages(ctx, messages, withTools, thinking, nil)
}

func (d *localRuntime) streamOllamaWithImages(ctx context.Context, messages []map[string]any, withTools, thinking bool, images []chatAttachmentFile) (localStreamResult, error) {
	nativeMessages, err := ollamaMessages(messages)
	if err != nil {
		return localStreamResult{}, err
	}
	if len(images) > 0 {
		for i := len(nativeMessages) - 1; i >= 0; i-- {
			if nativeMessages[i]["role"] != "user" {
				continue
			}
			encoded := make([]string, 0, len(images))
			for _, image := range images {
				encoded = append(encoded, base64.StdEncoding.EncodeToString(image.Data))
			}
			nativeMessages[i]["images"] = encoded
			break
		}
	}
	body := map[string]any{"model": d.model, "stream": true, "think": thinking, "messages": nativeMessages}
	if withTools {
		body["tools"] = d.tools
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return localStreamResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.ollamaURL+"/api/chat", bytes.NewReader(encoded))
	if err != nil {
		return localStreamResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
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
			return d.streamOllamaWithImages(ctx, messages, false, thinking, images)
		}
		return localStreamResult{}, fmt.Errorf("local chat completion returned %s: %s", resp.Status, message)
	}
	defer resp.Body.Close()
	return d.readOllamaNDJSON(ctx, resp.Body)
}

func (d *localRuntime) ollamaVision(ctx context.Context) bool {
	key := d.ollamaURL + "\x00" + d.model
	if cached, ok := localVisionCapabilities.Load(key); ok {
		return cached.(bool)
	}
	vision := false
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	encoded, err := json.Marshal(map[string]string{"model": d.model})
	if err == nil {
		request, requestErr := http.NewRequestWithContext(queryCtx, http.MethodPost, d.ollamaURL+"/api/show", bytes.NewReader(encoded))
		if requestErr == nil {
			request.Header.Set("Content-Type", "application/json")
			if d.secret != "" {
				request.Header.Set("Authorization", "Bearer "+d.secret)
			}
			response, doErr := d.client.Do(request)
			if doErr == nil {
				if response.StatusCode >= 200 && response.StatusCode < 300 {
					var details struct {
						Capabilities []string `json:"capabilities"`
					}
					if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&details) == nil {
						for _, capability := range details.Capabilities {
							if capability == "vision" {
								vision = true
								break
							}
						}
					}
				}
				_ = response.Body.Close()
			}
		}
	}
	cached, _ := localVisionCapabilities.LoadOrStore(key, vision)
	return cached.(bool)
}

func ollamaMessages(messages []map[string]any) ([]map[string]any, error) {
	result := make([]map[string]any, len(messages))
	for i, message := range messages {
		converted := make(map[string]any, len(message))
		for key, value := range message {
			if key == "tool_call_id" {
				continue
			}
			if key == "tool_calls" {
				calls, ok := value.([]map[string]any)
				if !ok {
					return nil, errors.New("invalid local tool calls")
				}
				nativeCalls := make([]map[string]any, 0, len(calls))
				for _, call := range calls {
					function, ok := call["function"].(map[string]string)
					if !ok || function["name"] == "" {
						return nil, errors.New("invalid local tool function")
					}
					var arguments map[string]any
					if err := json.Unmarshal([]byte(function["arguments"]), &arguments); err != nil {
						return nil, fmt.Errorf("decode local tool arguments: %w", err)
					}
					if arguments == nil {
						return nil, errors.New("local tool arguments must be an object")
					}
					nativeCalls = append(nativeCalls, map[string]any{"function": map[string]any{"name": function["name"], "arguments": arguments}})
				}
				converted[key] = nativeCalls
				continue
			}
			converted[key] = value
		}
		result[i] = converted
	}
	return result, nil
}

type ollamaChunk struct {
	Error           any  `json:"error"`
	Done            bool `json:"done"`
	PromptEvalCount int  `json:"prompt_eval_count"`
	Message         struct {
		Content   string `json:"content"`
		Thinking  string `json:"thinking"`
		ToolCalls []struct {
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
}

func (d *localRuntime) readOllamaNDJSON(ctx context.Context, body io.Reader) (localStreamResult, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), localSSELineLimit)
	var result localStreamResult
	calls := make(map[int]localCall)
	filter := localSecretFilter{secret: d.secret}
	emitSafeText := func(text string) {
		if text != "" {
			result.text += text
			d.manager.emitDelta(d.chatID, text)
		}
	}
	emitText := func(text string) { emitSafeText(filter.feed(text)) }
	index := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return localStreamResult{}, err
		}
		var chunk ollamaChunk
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			return localStreamResult{}, fmt.Errorf("decode Ollama stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return localStreamResult{}, fmt.Errorf("Ollama stream error: %v", chunk.Error)
		}
		emitText(chunk.Message.Content)
		for _, tool := range chunk.Message.ToolCalls {
			var arguments map[string]any
			if len(tool.Function.Arguments) == 0 {
				return localStreamResult{}, fmt.Errorf("Ollama returned invalid arguments for %s", tool.Function.Name)
			}
			if err := json.Unmarshal(tool.Function.Arguments, &arguments); err != nil {
				return localStreamResult{}, fmt.Errorf("decode Ollama arguments for %s: %w", tool.Function.Name, err)
			}
			if arguments == nil {
				return localStreamResult{}, fmt.Errorf("Ollama arguments for %s must be an object", tool.Function.Name)
			}
			encoded, err := json.Marshal(arguments)
			if err != nil {
				return localStreamResult{}, err
			}
			calls[index] = localCall{Index: index, ID: fmt.Sprintf("ollama-call-%d", index), Name: tool.Function.Name, Arguments: string(encoded)}
			index++
		}
		if chunk.PromptEvalCount > 0 {
			result.promptEvalCount = chunk.PromptEvalCount
		}
		if chunk.Done {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return localStreamResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return localStreamResult{}, err
	}
	emitSafeText(filter.flush())
	for i := 0; i < len(calls); i++ {
		call, ok := calls[i]
		if !ok {
			return localStreamResult{}, errors.New("Ollama returned incomplete tool calls")
		}
		if call.Name == "" {
			return localStreamResult{}, errors.New("Ollama returned incomplete tool call")
		}
		result.calls = append(result.calls, call)
	}
	return result, nil
}

func (d *localRuntime) ollamaContextSize(ctx context.Context) int {
	if d.contextChecked {
		if d.contextSize == 0 {
			return localDefaultContext
		}
		return d.contextSize
	}
	d.contextChecked = true
	d.contextSize = localDefaultContext
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request := func(method, path string, body any) (*http.Response, error) {
		var reader io.Reader
		if body != nil {
			encoded, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			reader = bytes.NewReader(encoded)
		}
		req, err := http.NewRequestWithContext(queryCtx, method, d.ollamaURL+path, reader)
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if d.secret != "" {
			req.Header.Set("Authorization", "Bearer "+d.secret)
		}
		return d.client.Do(req)
	}
	resp, err := request(http.MethodGet, "/api/ps", nil)
	if err == nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var running struct {
				Models []struct {
					Name          string `json:"name"`
					Model         string `json:"model"`
					ContextLength int    `json:"context_length"`
				} `json:"models"`
			}
			if decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&running); decodeErr != nil {
				d.contextSize = localDefaultContext
			} else {
				for _, model := range running.Models {
					if (model.Name == d.model || model.Model == d.model) && model.ContextLength > 0 {
						d.contextSize = model.ContextLength
						break
					}
				}
			}
		}
		_ = resp.Body.Close()
	}
	// /api/show reports model maximum context, not active context; read it once but
	// use the running context from /api/ps or the server's default instead.
	selectedContext := d.contextSize
	resp, err = request(http.MethodPost, "/api/show", map[string]string{"model": d.model})
	if err == nil {
		if _, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20)); readErr != nil {
			d.contextSize = selectedContext
		}
		_ = resp.Body.Close()
	}
	return d.contextSize
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
	secretFilter := localSecretFilter{secret: d.secret}
	finished := false
	emitSafeText := func(text string) {
		if text != "" {
			result.text += text
			d.manager.emitDelta(d.chatID, text)
		}
	}
	emitText := func(text string) { emitSafeText(secretFilter.feed(text)) }
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
			emitText(filter.feed(choice.Delta.Content))
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
		emitText(rest)
	}
	emitSafeText(secretFilter.flush())
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

func (d *localRuntime) redact(text string) string {
	if d.secret == "" {
		return text
	}
	return strings.ReplaceAll(text, d.secret, localRedactionMarker(d.secret))
}

type localSecretFilter struct {
	secret  string
	pending string
}

func (f *localSecretFilter) feed(text string) string {
	if f.secret == "" {
		return text
	}
	f.pending += text
	safeLimit := len(f.pending) - len(f.secret) + 1
	if safeLimit <= 0 {
		return ""
	}
	var output strings.Builder
	consumed := 0
	for consumed < safeLimit {
		if strings.HasPrefix(f.pending[consumed:], f.secret) {
			output.WriteString(localRedactionMarker(f.secret))
			consumed += len(f.secret)
			continue
		}
		_, size := utf8.DecodeRuneInString(f.pending[consumed:])
		if consumed+size > safeLimit {
			break
		}
		output.WriteString(f.pending[consumed : consumed+size])
		consumed += size
	}
	f.pending = f.pending[consumed:]
	return output.String()
}

func (f *localSecretFilter) flush() string {
	defer func() { f.pending = "" }()
	if f.secret == "" {
		return f.pending
	}
	return strings.ReplaceAll(f.pending, f.secret, localRedactionMarker(f.secret))
}

func localRedactionMarker(secret string) string {
	for _, marker := range []string{"[redacted]", "[hidden]", "***", "█", "■", "◆"} {
		if !strings.Contains(marker, secret) {
			return marker
		}
	}
	return "\u0000"
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

func capStoredToolResult(output string) string {
	const suffix = "\n[tool result truncated]"
	if len(output) <= localStoredToolMax {
		return output
	}
	end := localStoredToolMax - len(suffix)
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
