package api

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	connectorcli "my-tickets/internal/connector"
)

const opencodeSSELimit = 4 << 20

type opencodeProcess interface {
	Kill() error
	Wait() error
}

type opencodeProcessStarter func(path string, args, env []string) (opencodeProcess, error)

type opencodeCommand struct {
	cmd  *exec.Cmd
	once sync.Once
	err  error
}

func (p *opencodeCommand) Kill() error { return p.cmd.Process.Kill() }
func (p *opencodeCommand) Wait() error {
	p.once.Do(func() { p.err = p.cmd.Wait() })
	return p.err
}

type opencodeServer struct {
	connectorID string
	registryKey string
	major       int
	baseURL     string
	password    string
	client      *http.Client
	process     opencodeProcess
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	mu          sync.Mutex
	chats       map[string]*opencodeRuntime
	refs        int
}

type opencodeRuntime struct {
	manager       *chatManager
	chatID        string
	server        *opencodeServer
	session       string
	directory     string
	model         string
	storedSession bool
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	text          strings.Builder
	roles         map[string]string
	parts         map[string]string
	active        bool
	closed        bool
	release       sync.Once
}

var opencodeServers = struct {
	sync.Mutex
	byConnector map[string]*opencodeServer
}{byConnector: make(map[string]*opencodeServer)}

func init() { runtimes["opencode"] = startOpenCode }

func startOpenCode(m *chatManager, c chat, configDir string, rt *chatRuntime) (chatRuntimeDriver, error) {
	if c.ConnectorID == nil {
		return nil, errChatConnectorMissing
	}
	kind, actualConfigDir, err := m.connectorConfig(*c.ConnectorID)
	if err != nil {
		return nil, err
	}
	if kind != "opencode" {
		return nil, errChatUnsupported
	}
	external, err := m.chatMCPServers(c.ID)
	if err != nil {
		return nil, err
	}
	server, err := acquireOpenCodeServer(m, *c.ConnectorID, actualConfigDir, external)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(server.ctx)
	driver := &opencodeRuntime{manager: m, chatID: c.ID, server: server, directory: c.Workdir, model: c.Model, ctx: ctx, cancel: cancel,
		roles: make(map[string]string), parts: make(map[string]string)}
	if c.SessionID != nil && *c.SessionID != "" {
		driver.session = *c.SessionID
		driver.storedSession = true
	} else if err := driver.createSession(); err != nil {
		driver.releaseServer()
		return nil, err
	}
	server.mu.Lock()
	server.chats[driver.session] = driver
	server.mu.Unlock()
	return driver, nil
}

func acquireOpenCodeServer(m *chatManager, connectorID, configDir string, external []mcpServerSpec) (*opencodeServer, error) {
	finder := m.options.findOpencode
	if finder == nil {
		finder = func() (string, error) { return connectorcli.Find("opencode") }
	}
	path, err := finder()
	if err != nil {
		return nil, fmt.Errorf("find opencode: %w", err)
	}
	env := connectorEnv("opencode", configDir)
	majorDetector := m.options.opencodeMajor
	if majorDetector == nil {
		majorDetector = connectorcli.OpencodeMajor
	}
	major, err := majorDetector(context.Background(), path, env)
	if err != nil {
		return nil, fmt.Errorf("detect opencode version: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	password := hex.EncodeToString(secret)
	mcpSpec, err := m.ticketMCPServerSpec()
	if err != nil {
		return nil, err
	}
	exe, ok := mcpSpec["command"].(string)
	if !ok {
		return nil, errors.New("ticket MCP spec has no command")
	}
	mcpArgs, ok := mcpSpec["args"].([]string)
	if !ok {
		return nil, errors.New("ticket MCP spec has no args")
	}
	configValue := map[string]any{
		"permission": map[string]string{"*": "ask", "edit": "ask", "bash": "ask", "webfetch": "ask"},
		"mcp":        opencodeMCPServers(exe, mcpArgs, external),
	}
	if major >= 2 {
		configValue = map[string]any{
			"permissions": []map[string]string{
				{"action": "edit", "resource": "*", "effect": "ask"},
				{"action": "shell", "resource": "*", "effect": "ask"},
				{"action": "webfetch", "resource": "*", "effect": "ask"},
			},
			"mcp": opencodeV2MCPServers(exe, mcpArgs, external),
		}
	}
	config, err := json.Marshal(configValue)
	if err != nil {
		return nil, err
	}
	keyHash := sha256.Sum256(config)
	registryKey := connectorID + ":" + fmt.Sprintf("%x", keyHash[:])
	opencodeServers.Lock()
	defer opencodeServers.Unlock()
	if server := opencodeServers.byConnector[registryKey]; server != nil {
		server.refs++
		return server, nil
	}
	starter := m.options.opencodeStarter
	if starter == nil {
		starter = startOpenCodeProcess
	}
	args := []string{"serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port)}
	env = opencodeEnv(env, "OPENCODE_SERVER_PASSWORD="+password, "OPENCODE_CONFIG_CONTENT="+string(config))
	process, err := starter(path, args, env)
	if err != nil {
		return nil, fmt.Errorf("start opencode serve: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
	server := &opencodeServer{connectorID: connectorID, registryKey: registryKey, major: major, baseURL: baseURL, password: password, client: &http.Client{Timeout: 0}, process: process,
		ctx: ctx, cancel: cancel, done: make(chan struct{}), chats: make(map[string]*opencodeRuntime), refs: 1}
	if err := server.ready(); err != nil {
		cancel()
		_ = process.Kill()
		_ = process.Wait()
		return nil, err
	}
	opencodeServers.byConnector[registryKey] = server
	go server.events()
	return server, nil
}

func opencodeEnv(env []string, values ...string) []string {
	for _, value := range values {
		key := strings.SplitN(value, "=", 2)[0] + "="
		found := false
		for i := range env {
			if strings.HasPrefix(env[i], key) {
				env[i], found = value, true
				break
			}
		}
		if !found {
			env = append(env, value)
		}
	}
	return env
}

func startOpenCodeProcess(path string, args, env []string) (opencodeProcess, error) {
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &opencodeCommand{cmd: cmd}, nil
}

func (s *opencodeServer) request(ctx context.Context, method, endpoint string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("opencode", s.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.client.Do(req)
}

func (s *opencodeServer) ready() error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		endpoint := "/doc"
		if s.major >= 2 {
			endpoint = "/api/info"
		}
		resp, err := s.request(ctx, http.MethodGet, endpoint, nil)
		cancel()
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
	return errors.New("opencode serve did not become ready within 15 seconds")
}

func (d *opencodeRuntime) endpoint(path string) string {
	if d.server.major >= 2 {
		return path
	}
	query := url.Values{"directory": []string{d.directory}}
	return path + "?" + query.Encode()
}

func (d *opencodeRuntime) createSession() error {
	endpoint := d.endpoint("/session")
	body := map[string]any{}
	if d.server.major >= 2 {
		endpoint = "/api/session"
		body["location"] = map[string]string{"directory": d.directory}
		if provider, modelID, ok := strings.Cut(d.model, "/"); ok && provider != "" && modelID != "" {
			body["model"] = map[string]string{"providerID": provider, "id": modelID}
		}
	}
	resp, err := d.server.request(context.Background(), http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("opencode create session returned %s", resp.Status)
	}
	var session struct {
		ID   string `json:"id"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&session); err != nil {
		return err
	}
	if d.server.major >= 2 {
		session.ID = session.Data.ID
	}
	if session.ID == "" {
		return errors.New("opencode create session returned no id")
	}
	d.session = session.ID
	return d.manager.saveSession(d.chatID, session.ID)
}

func (d *opencodeRuntime) send(text string) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return errors.New("opencode chat is closed")
	}
	d.text.Reset()
	d.active = true
	d.mu.Unlock()
	var model any
	if provider, modelID, ok := strings.Cut(d.model, "/"); ok && provider != "" && modelID != "" {
		model = map[string]string{"providerID": provider, "modelID": modelID}
	}
	endpoint := d.endpoint("/session/" + url.PathEscape(d.session) + "/prompt_async")
	body := map[string]any{"parts": []any{map[string]string{"type": "text", "text": text}}}
	if d.server.major >= 2 {
		endpoint = "/api/session/" + url.PathEscape(d.session) + "/prompt"
		body = map[string]any{"text": text}
	} else if model != nil {
		body["model"] = model
	}
	resp, err := d.server.request(context.Background(), http.MethodPost, endpoint, body)
	if err != nil {
		d.discardTurn()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if d.server.major >= 2 && d.storedSession && resp.StatusCode == http.StatusNotFound && opencodeSessionNotFound(resp) {
			_ = resp.Body.Close()
			oldSession := d.session
			if err := d.createSession(); err != nil {
				d.discardTurn()
				return err
			}
			d.server.mu.Lock()
			if d.server.chats[oldSession] == d {
				delete(d.server.chats, oldSession)
			}
			d.server.chats[d.session] = d
			d.server.mu.Unlock()
			d.storedSession = false
			const note = "opencode was upgraded; earlier context was not carried over."
			if _, err := InsertChatMessage(d.manager.db, d.chatID, "error", note); err != nil {
				d.discardTurn()
				return err
			}
			d.manager.emit(d.chatID, map[string]any{"type": "error", "message": note})
			return d.send(text)
		}
		err := fmt.Errorf("opencode prompt returned %s", resp.Status)
		d.discardTurn()
		return err
	}
	return nil
}

func (d *opencodeRuntime) stop() error {
	endpoint := d.endpoint("/session/" + url.PathEscape(d.session) + "/abort")
	var body any = map[string]any{}
	if d.server.major >= 2 {
		endpoint = "/api/session/" + url.PathEscape(d.session) + "/interrupt?resume=false"
		body = nil
	}
	resp, err := d.server.request(context.Background(), http.MethodPost, endpoint, body)
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			operation := "abort"
			if d.server.major >= 2 {
				operation = "interrupt"
			}
			err = fmt.Errorf("opencode %s returned %s", operation, resp.Status)
		}
	}
	d.releaseServer()
	return err
}

func (d *opencodeRuntime) close() error {
	d.releaseServer()
	return nil
}

func (d *opencodeRuntime) releaseServer() {
	d.release.Do(func() {
		d.cancel()
		d.mu.Lock()
		d.closed = true
		d.mu.Unlock()
		s := d.server
		s.mu.Lock()
		if s.chats[d.session] == d {
			delete(s.chats, d.session)
		}
		s.mu.Unlock()
		opencodeServers.Lock()
		s.refs--
		last := s.refs == 0
		if last && opencodeServers.byConnector[s.registryKey] == s {
			delete(opencodeServers.byConnector, s.registryKey)
		}
		opencodeServers.Unlock()
		if last {
			s.shutdown()
		}
	})
}

func (s *opencodeServer) shutdown() {
	s.cancel()
	_ = s.process.Kill()
	_ = s.process.Wait()
	<-s.done
}

func (s *opencodeServer) events() {
	defer close(s.done)
	wait := 100 * time.Millisecond
	for s.ctx.Err() == nil {
		endpoint := "/global/event"
		if s.major >= 2 {
			endpoint = "/api/event"
		}
		resp, err := s.request(s.ctx, http.MethodGet, endpoint, nil)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				if err := s.readEvents(resp.Body); err != nil && s.ctx.Err() == nil {
					wait = 100 * time.Millisecond
				}
			} else {
				_ = resp.Body.Close()
			}
		}
		if s.ctx.Err() != nil {
			return
		}
		time.Sleep(wait)
		if wait < time.Second {
			wait *= 2
		}
	}
}

func (s *opencodeServer) readEvents(body io.ReadCloser) error {
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), opencodeSSELimit)
	var data strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		} else if line == "" && data.Len() > 0 {
			var event map[string]json.RawMessage
			if err := json.Unmarshal([]byte(data.String()), &event); err != nil {
				return err
			}
			if payload := event["payload"]; len(payload) > 0 {
				if err := json.Unmarshal(payload, &event); err != nil {
					return err
				}
			}
			if err := s.dispatch(event); err != nil {
				s.failEvent(event, err)
			}
			data.Reset()
		}
		if s.ctx.Err() != nil {
			return s.ctx.Err()
		}
	}
	return scanner.Err()
}

func (s *opencodeServer) failEvent(event map[string]json.RawMessage, cause error) {
	var properties struct {
		SessionID string `json:"sessionID"`
	}
	field := event["properties"]
	if s.major >= 2 {
		field = event["data"]
	}
	if json.Unmarshal(field, &properties) != nil || properties.SessionID == "" {
		return
	}
	s.mu.Lock()
	driver := s.chats[properties.SessionID]
	s.mu.Unlock()
	if driver == nil {
		return
	}
	err := fmt.Errorf("process opencode event: %w", cause)
	if finishErr := driver.finish(err); finishErr != nil {
		driver.manager.emit(driver.chatID, map[string]any{"type": "error", "message": finishErr.Error()})
	}
}

func (s *opencodeServer) dispatch(event map[string]json.RawMessage) error {
	if s.major >= 2 {
		return s.dispatchV2(event)
	}
	var typ string
	if err := json.Unmarshal(event["type"], &typ); err != nil {
		return err
	}
	if typ == "sync" {
		return nil
	}
	var props map[string]json.RawMessage
	if err := json.Unmarshal(event["properties"], &props); err != nil {
		return err
	}
	var sid string
	for _, key := range []string{"sessionID", "sessionId"} {
		raw := props[key]
		if len(raw) == 0 {
			continue
		}
		if err := json.Unmarshal(raw, &sid); err != nil {
			return err
		}
		if sid != "" {
			break
		}
	}
	s.mu.Lock()
	driver := s.chats[sid]
	s.mu.Unlock()
	if driver != nil {
		switch typ {
		case "message.updated":
			var info struct {
				ID   string `json:"id"`
				Role string `json:"role"`
			}
			if err := json.Unmarshal(props["info"], &info); err != nil {
				return err
			}
			if info.ID == "" || info.Role == "" {
				return errors.New("opencode message update missing id or role")
			}
			driver.mu.Lock()
			driver.roles[info.ID] = info.Role
			driver.mu.Unlock()
		case "message.part.updated":
			var properties struct {
				Part json.RawMessage `json:"part"`
			}
			var part struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			}
			if err := json.Unmarshal(event["properties"], &properties); err != nil {
				return err
			}
			if err := json.Unmarshal(properties.Part, &part); err != nil {
				return err
			}
			if part.ID == "" || part.Type == "" {
				return errors.New("opencode part update missing id or type")
			}
			driver.mu.Lock()
			driver.parts[part.ID] = part.Type
			driver.mu.Unlock()
		}
	}
	if driver == nil {
		return nil
	}
	switch typ {
	case "message.part.delta":
		var delta struct {
			MessageID string `json:"messageID"`
			PartID    string `json:"partID"`
			Field     string `json:"field"`
			Delta     string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(event["properties"]), &delta); err != nil {
			return err
		}
		if delta.MessageID == "" || delta.PartID == "" || delta.Field == "" {
			return errors.New("opencode text delta missing message, part, or field")
		}
		driver.mu.Lock()
		role := driver.roles[delta.MessageID]
		partType := driver.parts[delta.PartID]
		if delta.Field == "text" && role == "assistant" && partType == "text" {
			if driver.active {
				driver.text.WriteString(delta.Delta)
				driver.manager.emitDelta(driver.chatID, delta.Delta)
			}
		}
		driver.mu.Unlock()
	case "message.part.updated":
		var part struct {
			MessageID string `json:"messageID"`
			Part      struct {
				MessageID string          `json:"messageID"`
				Type      string          `json:"type"`
				Tool      string          `json:"tool"`
				Input     json.RawMessage `json:"input"`
				State     struct {
					Status string          `json:"status"`
					Error  string          `json:"error"`
					Input  json.RawMessage `json:"input"`
				} `json:"state"`
			} `json:"part"`
		}
		if err := json.Unmarshal([]byte(event["properties"]), &part); err != nil {
			return err
		}
		if part.MessageID == "" {
			part.MessageID = part.Part.MessageID
		}
		driver.mu.Lock()
		role := driver.roles[part.MessageID]
		driver.mu.Unlock()
		if role != "user" && part.Part.Type == "tool" && (part.Part.State.Status == "completed" || part.Part.State.Status == "error") {
			toolInput := part.Part.Input
			if len(toolInput) == 0 {
				toolInput = part.Part.State.Input
			}
			input, err := compactJSON(toolInput)
			if err != nil {
				return err
			}
			if err := driver.manager.emitTool(driver.chatID, strings.TrimSpace(part.Part.Tool+" "+shortText(input, 180))); err != nil {
				return err
			}
		}
	case "permission.asked":
		var permission struct {
			ID         string          `json:"id"`
			Permission string          `json:"permission"`
			Properties json.RawMessage `json:"properties"`
			Patterns   []string        `json:"patterns"`
			Metadata   map[string]any  `json:"metadata"`
			Always     []string        `json:"always"`
			Tool       map[string]any  `json:"tool"`
		}
		if err := json.Unmarshal([]byte(event["properties"]), &permission); err != nil {
			return err
		}
		if permission.ID == "" || permission.Permission == "" {
			return errors.New("opencode permission event missing id or permission")
		}
		if len(permission.Properties) == 0 {
			properties, err := json.Marshal(map[string]any{"patterns": permission.Patterns, "metadata": permission.Metadata, "always": permission.Always, "tool": permission.Tool})
			if err != nil {
				return err
			}
			permission.Properties = properties
		}
		go func() {
			allowed, err := driver.manager.requestApproval(driver.ctx, driver.chatID, permission.Permission, permission.Properties)
			if err != nil {
				if driver.ctx.Err() == nil {
					driver.fail(fmt.Errorf("request opencode permission approval: %w", err))
				}
				return
			}
			response := "reject"
			if allowed {
				response = "once"
			}
			resp, err := s.request(driver.ctx, http.MethodPost, driver.endpoint("/session/"+url.PathEscape(sid)+"/permissions/"+url.PathEscape(permission.ID)), map[string]string{"response": response})
			if err != nil {
				if driver.ctx.Err() == nil {
					driver.fail(fmt.Errorf("answer opencode permission: %w", err))
				}
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				driver.fail(fmt.Errorf("answer opencode permission returned %s", resp.Status))
			}
		}()
	case "session.idle", "session.status":
		if typ == "session.idle" {
			return driver.finish(nil)
		}
		var status struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(props["status"], &status); err != nil {
			return err
		}
		if status.Type == "idle" {
			return driver.finish(nil)
		}
	case "session.error":
		var failure struct {
			Error any `json:"error"`
		}
		if err := json.Unmarshal([]byte(event["properties"]), &failure); err != nil {
			return err
		}
		return driver.finish(fmt.Errorf("opencode session error: %v", failure.Error))
	}
	return nil
}

func opencodeSessionNotFound(resp *http.Response) bool {
	var failure struct {
		Tag string `json:"_tag"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false
	}
	return json.Unmarshal(data, &failure) == nil && failure.Tag == "SessionNotFoundError"
}

func (s *opencodeServer) dispatchV2(event map[string]json.RawMessage) error {
	var typ string
	if err := json.Unmarshal(event["type"], &typ); err != nil {
		return err
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(event["data"], &data); err != nil {
		return err
	}
	var sid string
	if err := json.Unmarshal(data["sessionID"], &sid); err != nil {
		return err
	}
	if sid == "" {
		return nil
	}
	s.mu.Lock()
	driver := s.chats[sid]
	s.mu.Unlock()
	if driver == nil {
		return nil
	}
	switch typ {
	case "session.text.delta":
		var delta struct {
			Text string `json:"delta"`
		}
		if err := json.Unmarshal(event["data"], &delta); err != nil {
			return err
		}
		driver.mu.Lock()
		if driver.active {
			driver.text.WriteString(delta.Text)
			driver.manager.emitDelta(driver.chatID, delta.Text)
		}
		driver.mu.Unlock()
	case "session.tool.called":
		var tool struct {
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(event["data"], &tool); err != nil {
			return err
		}
		input, err := compactJSON(tool.Input)
		if err != nil {
			return err
		}
		if err := driver.manager.emitTool(driver.chatID, strings.TrimSpace(tool.Name+" "+shortText(input, 180))); err != nil {
			return err
		}
	case "permission.asked":
		var permission struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if err := json.Unmarshal(event["data"], &permission); err != nil {
			return err
		}
		if permission.ID == "" || permission.Action == "" {
			return errors.New("opencode permission event missing id or action")
		}
		go func() {
			allowed, err := driver.manager.requestApproval(driver.ctx, driver.chatID, permission.Action, json.RawMessage(event["data"]))
			if err != nil {
				if driver.ctx.Err() == nil {
					driver.fail(fmt.Errorf("request opencode permission approval: %w", err))
				}
				return
			}
			decision := "reject"
			if allowed {
				decision = "once"
			}
			endpoint := "/api/session/" + url.PathEscape(sid) + "/permission/" + url.PathEscape(permission.ID) + "/reply"
			resp, err := s.request(driver.ctx, http.MethodPost, endpoint, map[string]string{"decision": decision})
			if err != nil {
				if driver.ctx.Err() == nil {
					driver.fail(fmt.Errorf("answer opencode permission: %w", err))
				}
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				driver.fail(fmt.Errorf("answer opencode permission returned %s", resp.Status))
			}
		}()
	case "session.execution.succeeded", "session.execution.interrupted":
		return driver.finish(nil)
	case "session.execution.failed":
		var failure struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(event["data"], &failure); err != nil {
			return err
		}
		return driver.finish(fmt.Errorf("opencode session error: %s", failure.Error.Message))
	}
	return nil
}

func (d *opencodeRuntime) finish(err error) error {
	d.mu.Lock()
	if !d.active {
		d.mu.Unlock()
		return nil
	}
	d.active = false
	text := d.text.String()
	d.text.Reset()
	d.roles = make(map[string]string)
	d.parts = make(map[string]string)
	d.mu.Unlock()
	return d.manager.finishTurn(d.chatID, text, err)
}

func (d *opencodeRuntime) fail(err error) {
	if finishErr := d.finish(err); finishErr != nil {
		d.manager.emit(d.chatID, map[string]any{"type": "error", "message": finishErr.Error()})
	}
}

func (d *opencodeRuntime) discardTurn() {
	d.mu.Lock()
	d.active = false
	d.text.Reset()
	d.mu.Unlock()
}
