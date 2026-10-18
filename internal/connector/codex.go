package connector

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"sync"

	"patio/internal/proc"
)

type Notification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Model struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Efforts     []string `json:"efforts,omitempty"`
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

type Client struct {
	in            io.Reader
	out           io.Writer
	closeProcess  func() error
	writeMu       sync.Mutex
	mu            sync.Mutex
	nextID        uint64
	pending       map[string]chan rpcResult
	notifications chan Notification
	requests      chan Request
	readErr       error
}

func newClient(in io.Reader, out io.Writer, closeProcess func() error) *Client {
	c := &Client{in: in, out: out, closeProcess: closeProcess, pending: make(map[string]chan rpcResult), notifications: make(chan Notification, 16), requests: make(chan Request, 16)}
	go c.readLoop()
	return c
}

func (c *Client) Notifications() <-chan Notification { return c.notifications }

func (c *Client) Requests() <-chan Request { return c.requests }

func NewClientForIO(in io.Reader, out io.Writer, closeProcess func() error) *Client {
	return newClient(in, out, closeProcess)
}

func (c *Client) Initialize(ctx context.Context, version string) error {
	if version == "" {
		version = "dev"
	}
	var ignored json.RawMessage
	if err := c.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "patio", "title": "Patio", "version": version}}, &ignored); err != nil {
		return err
	}
	return c.Notify("initialized", map[string]any{})
}

func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	c.mu.Lock()
	if c.readErr != nil {
		err := c.readErr
		c.mu.Unlock()
		return err
	}
	c.nextID++
	idNumber := c.nextID - 1
	id := strconv.FormatUint(idNumber, 10)
	ch := make(chan rpcResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	request := struct {
		Method string `json:"method"`
		ID     uint64 `json:"id"`
		Params any    `json:"params"`
	}{method, idNumber, params}
	if err := c.write(request); err != nil {
		c.removePending(id)
		return err
	}
	select {
	case response := <-ch:
		if response.err != nil {
			return response.err
		}
		if out != nil {
			return json.Unmarshal(response.result, out)
		}
		return nil
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	}
}

func (c *Client) Notify(method string, params any) error {
	return c.write(struct {
		Method string `json:"method"`
		Params any    `json:"params"`
	}{method, params})
}

func (c *Client) Respond(id json.RawMessage, result any) error {
	if len(id) == 0 || !json.Valid(id) {
		return fmt.Errorf("invalid app-server request id")
	}
	return c.write(struct {
		ID     json.RawMessage `json:"id"`
		Result any             `json:"result"`
	}{id, result})
}

func (c *Client) RespondError(id json.RawMessage, code int, message string) error {
	if len(id) == 0 || !json.Valid(id) {
		return fmt.Errorf("invalid app-server request id")
	}
	return c.write(struct {
		ID    json.RawMessage `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{ID: id, Error: struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message}})
}

func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var response struct {
		Data []struct {
			ID                        string `json:"id"`
			DisplayName               string `json:"displayName"`
			Description               string `json:"description"`
			IsDefault                 bool   `json:"isDefault"`
			Hidden                    bool   `json:"hidden"`
			SupportedReasoningEfforts []struct {
				ReasoningEffort string `json:"reasoningEffort"`
			} `json:"supportedReasoningEfforts"`
		} `json:"data"`
	}
	if err := c.Call(ctx, "model/list", map[string]any{}, &response); err != nil {
		return nil, err
	}
	sort.SliceStable(response.Data, func(i, j int) bool { return response.Data[i].IsDefault && !response.Data[j].IsDefault })
	models := make([]Model, 0, len(response.Data))
	for _, model := range response.Data {
		if model.Hidden || model.ID == "" {
			continue
		}
		label := model.DisplayName
		if label == "" {
			label = model.ID
		}
		efforts := make([]string, 0, len(model.SupportedReasoningEfforts))
		for _, effort := range model.SupportedReasoningEfforts {
			if effort.ReasoningEffort != "" {
				efforts = append(efforts, effort.ReasoningEffort)
			}
		}
		models = append(models, Model{ID: model.ID, Label: label, Description: model.Description, Efforts: efforts})
	}
	return models, nil
}

func (c *Client) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.out.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write app-server request: %w", err)
	}
	return nil
}

func (c *Client) removePending(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) readLoop() {
	defer close(c.notifications)
	defer close(c.requests)
	scanner := bufio.NewScanner(c.in)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var message rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			c.fail(fmt.Errorf("decode app-server response: %w", err))
			return
		}
		if message.Method != "" {
			if len(message.ID) != 0 {
				c.requests <- Request{ID: message.ID, Method: message.Method, Params: message.Params}
			} else {
				select {
				case c.notifications <- Notification{Method: message.Method, Params: message.Params}:
				default:
					select {
					case <-c.notifications:
					default:
					}
					c.notifications <- Notification{Method: message.Method, Params: message.Params}
				}
			}
			continue
		}
		if len(message.ID) == 0 {
			continue
		}
		var id string
		if err := json.Unmarshal(message.ID, &id); err != nil {
			id = string(message.ID)
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			if message.Error != nil {
				ch <- rpcResult{err: fmt.Errorf("app-server %d: %s", message.Error.Code, message.Error.Message)}
			} else {
				ch <- rpcResult{result: message.Result}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		c.fail(err)
	} else {
		c.fail(io.EOF)
	}
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.readErr == nil {
		c.readErr = err
	}
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- rpcResult{err: err}
	}
	c.mu.Unlock()
}

func (c *Client) Close() error {
	if c.closeProcess != nil {
		return c.closeProcess()
	}
	return nil
}

func StartCodex(ctx context.Context, path string, env []string, isolated bool, version string) (*Client, error) {
	return StartCodexInDir(ctx, path, env, isolated, version, "")
}

func StartCodexInDir(ctx context.Context, path string, env []string, isolated bool, version, cwd string, overrides ...string) (*Client, error) {
	args := codexArgs(isolated)
	if len(overrides) != 0 {
		args = append(args[:len(args)-1], append(flattenOverrides(overrides), args[len(args)-1])...)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = cwd
	cmd.Env = env
	proc.HideConsole(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	client := newClient(stdout, stdin, func() error {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		return nil
	})
	if err := client.Initialize(ctx, version); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func flattenOverrides(overrides []string) []string {
	args := make([]string, 0, len(overrides)*2)
	for _, override := range overrides {
		args = append(args, "-c", override)
	}
	return args
}
