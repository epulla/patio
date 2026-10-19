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
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	connectorcli "patio/internal/connector"
	"patio/internal/proc"
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
	inputKey    string
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
	ancestors   map[string]string
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
	toolRows      map[string]string // v2 tool call ID -> tool chat message ID
	active        bool
	closed        bool
	release       sync.Once
	firstPrompt   bool
	personaPrompt bool
	mcpNames      []string
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
		roles: make(map[string]string), parts: make(map[string]string), firstPrompt: true, personaPrompt: c.SessionID == nil || *c.SessionID == ""}
	driver.mcpNames = []string{"patio"}
	for _, spec := range external {
		driver.mcpNames = append(driver.mcpNames, spec.Name)
	}
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
	input, err := json.Marshal(struct {
		ConnectorID string
		Path        string
		ConfigDir   string
		Ticket      map[string]any
		External    []mcpServerSpec
	}{connectorID, path, configDir, mcpSpec, external})
	if err != nil {
		return nil, err
	}
	inputDigest := sha256.Sum256(input)
	inputKey := fmt.Sprintf("%x", inputDigest[:])
	opencodeServers.Lock()
	defer opencodeServers.Unlock()
	for _, server := range opencodeServers.byConnector {
		if server.inputKey == inputKey {
			server.refs++
			return server, nil
		}
	}
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
	configValue := map[string]any{
		"permission": map[string]string{"*": "ask", "edit": "ask", "bash": "ask", "webfetch": "ask", "question": "deny"},
		"mcp":        opencodeMCPServers(exe, mcpArgs, external),
	}
	if major >= 2 {
		configValue = map[string]any{
			"permissions": []map[string]string{
				{"action": "edit", "resource": "*", "effect": "ask"},
				{"action": "shell", "resource": "*", "effect": "ask"},
				{"action": "webfetch", "resource": "*", "effect": "ask"},
				{"action": "question", "resource": "*", "effect": "deny"},
			},
			"mcp": opencodeV2MCPServers(exe, mcpArgs, external),
		}
	}
	config, err := json.Marshal(configValue)
	if err != nil {
		return nil, err
	}
	keyHash := sha256.Sum256(config)
	registryKey := connectorID + ":" + inputKey + ":" + fmt.Sprintf("%x", keyHash[:])
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
	server := &opencodeServer{connectorID: connectorID, registryKey: registryKey, inputKey: inputKey, major: major, baseURL: baseURL, password: password, client: &http.Client{Timeout: 0}, process: process,
		ctx: ctx, cancel: cancel, done: make(chan struct{}), chats: make(map[string]*opencodeRuntime), ancestors: make(map[string]string), refs: 1}
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
	proc.HideConsole(cmd)
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

func (d *opencodeRuntime) send(request ChatSendRequest) error {
	if d.firstPrompt {
		d.waitMCP()
	}
	files, err := loadChatAttachmentFiles(d.directory, request.Attachments)
	if err != nil {
		return err
	}
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
	prompt, err := ChatPrompt(d.manager.db, d.chatID)
	if err != nil {
		d.discardTurn()
		return err
	}
	if d.personaPrompt {
		request.Text = prompt + "\n\n" + request.Text
		d.personaPrompt = false
	}
	parts := opencodePromptParts(request.Text, files)
	body := map[string]any{"parts": parts}
	if d.server.major >= 2 {
		endpoint = "/api/session/" + url.PathEscape(d.session) + "/prompt"
		body = opencodeV2Prompt(request.Text, files)
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
			d.personaPrompt = true
			const note = "opencode was upgraded; earlier context was not carried over."
			if _, err := InsertChatMessage(d.manager.db, d.chatID, "error", note); err != nil {
				d.discardTurn()
				return err
			}
			d.manager.emit(d.chatID, map[string]any{"type": "error", "message": note})
			return d.send(request)
		}
		err := fmt.Errorf("opencode prompt returned %s", resp.Status)
		d.discardTurn()
		return err
	}
	return nil
}

func (d *opencodeRuntime) waitMCP() {
	started := time.Now()
	ctx, cancel := context.WithTimeout(d.ctx, 15*time.Second)
	defer cancel()
	notes := make(map[string]string)
	defer func() {
		for _, name := range d.mcpNames {
			if note, ok := notes[name]; ok {
				if _, err := InsertChatMessage(d.manager.db, d.chatID, "error", note); err == nil {
					d.manager.emit(d.chatID, map[string]any{"type": "error", "message": note})
				}
			}
		}
	}()
	var lastErr error
	firstPoll := true
	needsCatalogSettle := false
	for {
		endpoint := "/mcp?directory=" + url.QueryEscape(d.directory)
		if d.server.major >= 2 {
			endpoint = "/api/mcp?location%5Bdirectory%5D=" + url.QueryEscape(d.directory)
		}
		resp, err := d.server.request(ctx, http.MethodGet, endpoint, nil)
		if err == nil {
			var states map[string]struct {
				Status string `json:"status"`
				Error  string `json:"error"`
			}
			if d.server.major >= 2 {
				var result struct {
					Data []struct {
						Name   string `json:"name"`
						Status struct {
							Status string `json:"status"`
							Error  string `json:"error"`
						} `json:"status"`
					} `json:"data"`
				}
				err = json.NewDecoder(resp.Body).Decode(&result)
				_ = resp.Body.Close()
				states = make(map[string]struct {
					Status string `json:"status"`
					Error  string `json:"error"`
				})
				for _, item := range result.Data {
					states[item.Name] = struct {
						Status string `json:"status"`
						Error  string `json:"error"`
					}{item.Status.Status, item.Status.Error}
				}
			} else {
				err = json.NewDecoder(resp.Body).Decode(&states)
				_ = resp.Body.Close()
			}
			if err == nil {
				settled := true
				for _, name := range d.mcpNames {
					state, ok := states[name]
					if !ok || state.Status == "pending" {
						settled = false
						continue
					}
					if state.Status == "failed" || state.Status == "needs_auth" {
						notes[name] = fmt.Sprintf("MCP server %s unavailable: %s", name, state.Error)
					}
				}
				if firstPoll && !settled {
					needsCatalogSettle = true
				}
				firstPoll = false
				if settled {
					if needsCatalogSettle {
						// burnedout: assumed 1s; OpenCode adds tools to the catalog shortly after reporting connected
						timer := time.NewTimer(time.Second)
						select {
						case <-ctx.Done():
							timer.Stop()
							log.Printf("OpenCode MCP wait timed out after %s", time.Since(started))
							return
						case <-timer.C:
						}
					}
					log.Printf("OpenCode MCP ready after %s", time.Since(started))
					break
				}
			}
			lastErr = err
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				log.Printf("OpenCode MCP status failed after %s: %v", time.Since(started), lastErr)
			} else {
				log.Printf("OpenCode MCP wait timed out after %s", time.Since(started))
			}
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func opencodePromptParts(text string, files []chatAttachmentFile) []any {
	parts := make([]any, 0, len(files)+1)
	prompt := promptWithAttachmentPaths(text, files, true)
	if prompt != "" {
		parts = append(parts, map[string]string{"type": "text", "text": prompt})
	}
	for _, file := range files {
		if file.Kind == "image" {
			parts = append(parts, map[string]string{"type": "file", "mime": file.MIME, "url": chatImageDataURL(file)})
		}
	}
	return parts
}

func opencodeV2Prompt(text string, files []chatAttachmentFile) map[string]any {
	body := map[string]any{"text": promptWithAttachmentPaths(text, files, true)}
	attachments := make([]map[string]string, 0, len(files))
	for _, file := range files {
		if file.Kind == "image" {
			attachments = append(attachments, map[string]string{"uri": chatImageDataURL(file), "mime": file.MIME, "name": file.Name})
		}
	}
	if len(attachments) > 0 {
		body["files"] = attachments
	}
	return body
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
	if sid == "" && typ == "message.part.updated" {
		var part struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(props["part"], &part) == nil {
			sid = part.SessionID
		}
	}
	if typ == "session.created" || typ == "session.updated" {
		var info struct {
			ID       string `json:"id"`
			ParentID string `json:"parentID"`
		}
		if err := json.Unmarshal(props["info"], &info); err != nil {
			return err
		}
		if info.ID != "" && info.ParentID != "" {
			s.mu.Lock()
			if len(s.ancestors) >= 512 {
				s.ancestors = make(map[string]string)
			}
			s.ancestors[info.ID] = info.ParentID
			s.mu.Unlock()
		}
	}
	driver, child := s.driverForSession(sid)
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
		if !child && role == "assistant" && (partType == "text" || partType == "reasoning") {
			driver.startAutonomousTurnLocked()
		}
		if !child && delta.Field == "text" && role == "assistant" && partType == "text" {
			if driver.active {
				driver.text.WriteString(driver.manager.emitDelta(driver.chatID, delta.Delta))
			}
		} else if !child && delta.Field == "text" && role == "assistant" && partType == "reasoning" && driver.active {
			driver.manager.emitThinking(driver.chatID, delta.Delta)
		}
		driver.mu.Unlock()
	case "message.part.updated":
		var part struct {
			MessageID string `json:"messageID"`
			Delta     string `json:"delta"`
			Part      struct {
				MessageID string          `json:"messageID"`
				Type      string          `json:"type"`
				Tool      string          `json:"tool"`
				Input     json.RawMessage `json:"input"`
				State     struct {
					Status string          `json:"status"`
					Error  string          `json:"error"`
					Output string          `json:"output"`
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
		if !child && role == "assistant" && part.Delta != "" && (part.Part.Type == "text" || part.Part.Type == "reasoning") {
			driver.startAutonomousTurnLocked()
		}
		if !child && part.Delta != "" && role == "assistant" && driver.active {
			switch part.Part.Type {
			case "text":
				driver.text.WriteString(driver.manager.emitDelta(driver.chatID, part.Delta))
			case "reasoning":
				driver.manager.emitThinking(driver.chatID, part.Delta)
			}
		}
		driver.mu.Unlock()
		if !child && role == "assistant" && part.Part.Type == "tool" && (part.Part.State.Status == "completed" || part.Part.State.Status == "error") {
			driver.startAutonomousTurn()
			toolInput := part.Part.Input
			if len(toolInput) == 0 {
				toolInput = part.Part.State.Input
			}
			input, err := compactJSON(toolInput)
			if err != nil {
				return err
			}
			failed := part.Part.State.Status == "error"
			output := part.Part.State.Output
			if failed {
				output = part.Part.State.Error
			}
			if err := driver.manager.emitToolResult(driver.chatID, strings.TrimSpace(part.Part.Tool+" "+shortText(input, toolInputLimit)), output, failed); err != nil {
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
			if child {
				return nil
			}
			return driver.finish(nil)
		}
		var status struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(props["status"], &status); err != nil {
			return err
		}
		if status.Type == "idle" {
			if child {
				return nil
			}
			return driver.finish(nil)
		}
		if status.Type == "busy" && !child {
			driver.startAutonomousTurn()
		}
	case "session.error":
		if child {
			return nil
		}
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

// driverForSession routes subagent session events to their owning chat. API lookups
// are bounded and performed without holding s.mu because dispatch runs on SSE reader.
func (s *opencodeServer) driverForSession(sid string) (*opencodeRuntime, bool) {
	if sid == "" {
		return nil, false
	}
	s.mu.Lock()
	if d := s.chats[sid]; d != nil {
		s.mu.Unlock()
		return d, false
	}
	if root, ok := s.ancestors[sid]; ok {
		seen := map[string]bool{sid: true}
		for s.chats[root] == nil && s.ancestors[root] != "" && !seen[root] {
			seen[root] = true
			root = s.ancestors[root]
		}
		d := s.chats[root]
		s.mu.Unlock()
		if d != nil {
			return d, true
		}
		// A known parent chain can still be incomplete; API lookup remains fallback.
	} else {
		s.mu.Unlock()
	}
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()
	directories := []string{"."}
	if s.major < 2 {
		s.mu.Lock()
		seenDirs := map[string]bool{".": true}
		for _, d := range s.chats {
			if d.directory != "" && !seenDirs[d.directory] {
				seenDirs[d.directory] = true
				directories = append(directories, d.directory)
			}
		}
		s.mu.Unlock()
	}
	root, definitive := "", false
	for _, directory := range directories {
		current := sid
		seen := map[string]bool{sid: true}
		for range 8 {
			path := "/session/" + url.PathEscape(current)
			if s.major >= 2 {
				path = "/api" + path
			}
			if s.major < 2 {
				path += "?directory=" + url.QueryEscape(directory)
			}
			resp, err := s.request(ctx, http.MethodGet, path, nil)
			if err != nil {
				break
			}
			var body struct {
				Data struct {
					ParentID string `json:"parentID"`
				} `json:"data"`
				ParentID string `json:"parentID"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body)
			_ = resp.Body.Close()
			if decodeErr != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
				break
			}
			parent := body.ParentID
			if s.major >= 2 {
				parent = body.Data.ParentID
			}
			if parent == "" {
				definitive = true
				break
			}
			if seen[parent] {
				definitive = true
				break
			}
			seen[parent] = true
			s.mu.Lock()
			d := s.chats[parent]
			s.mu.Unlock()
			if d != nil {
				root, definitive = parent, true
				break
			}
			current = parent
		}
		if root != "" || definitive {
			break
		}
	}
	if !definitive {
		return nil, false
	}
	s.mu.Lock()
	if len(s.ancestors) >= 512 {
		s.ancestors = make(map[string]string)
	}
	s.ancestors[sid] = root
	d := s.chats[root]
	s.mu.Unlock()
	return d, d != nil
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
	driver, child := s.driverForSession(sid)
	if driver == nil {
		return nil
	}
	switch typ {
	case "session.reasoning.delta":
		var delta struct {
			Text string `json:"delta"`
		}
		if err := json.Unmarshal(event["data"], &delta); err != nil {
			return err
		}
		driver.mu.Lock()
		if !child {
			driver.startAutonomousTurnLocked()
			driver.manager.emitThinking(driver.chatID, delta.Text)
		}
		driver.mu.Unlock()
	case "session.text.delta":
		var delta struct {
			Text string `json:"delta"`
		}
		if err := json.Unmarshal(event["data"], &delta); err != nil {
			return err
		}
		driver.mu.Lock()
		if !child {
			driver.startAutonomousTurnLocked()
			driver.text.WriteString(driver.manager.emitDelta(driver.chatID, delta.Text))
		}
		driver.mu.Unlock()
	case "session.tool.called":
		var tool struct {
			ID    string          `json:"id"`
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
		if !child {
			driver.startAutonomousTurn()
		}
		messageID, err := driver.manager.emitToolCall(driver.chatID, strings.TrimSpace(tool.Name+" "+shortText(input, toolInputLimit)))
		if err != nil {
			return err
		}
		if tool.ID != "" {
			driver.mu.Lock()
			if driver.toolRows == nil {
				driver.toolRows = map[string]string{}
			}
			driver.toolRows[tool.ID] = messageID
			driver.mu.Unlock()
		}
	case "session.tool.success", "session.tool.failed":
		var tool struct {
			ID      string          `json:"id"`
			Content json.RawMessage `json:"content"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(event["data"], &tool); err != nil {
			return err
		}
		driver.mu.Lock()
		messageID := driver.toolRows[tool.ID]
		delete(driver.toolRows, tool.ID)
		driver.mu.Unlock()
		if messageID == "" {
			return nil
		}
		output := toolResultText(tool.Content)
		failed := typ == "session.tool.failed"
		if failed && tool.Error.Message != "" {
			output = strings.TrimSpace(tool.Error.Message + "\n" + output)
		}
		if err := driver.manager.attachToolResult(driver.chatID, messageID, output, failed); err != nil {
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
	case "session.execution.started":
		if !child {
			driver.startAutonomousTurn()
		}
	case "session.execution.succeeded", "session.execution.interrupted":
		if child {
			return nil
		}
		return driver.finish(nil)
	case "session.execution.failed":
		if child {
			return nil
		}
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

// startAutonomousTurnLocked is called with d.mu held for event-stream deltas.
func (d *opencodeRuntime) startAutonomousTurnLocked() {
	if d.active {
		return
	}
	if err := d.manager.claimTurn(d.chatID); err != nil {
		return
	}
	d.text.Reset()
	d.active = true
	if rt := d.manager.lookupRuntime(d.chatID); rt != nil {
		rt.mu.Lock()
		rt.busy = true
		d.manager.markRuntimeActiveLocked(d.chatID, rt)
		rt.mu.Unlock()
	}
}

func (d *opencodeRuntime) startAutonomousTurn() {
	d.mu.Lock()
	d.startAutonomousTurnLocked()
	d.mu.Unlock()
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
