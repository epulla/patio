package api

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
	connectorcli "patio/internal/connector"
	"patio/internal/proc"
)

type selectableModel struct {
	ID            string    `json:"id"`
	Label         string    `json:"label"`
	Description   string    `json:"description"`
	Efforts       *[]string `json:"efforts,omitempty"`
	DefaultEffort string    `json:"default_effort,omitempty"`
	isDefault     bool
}

var defaultModel = selectableModel{ID: "", Label: "Default", Description: "Use connector default"}
var claudeModelCache sync.Map

func (s *Server) connectorModels(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID, Kind  string
		BaseURL   sql.NullString
		ConfigDir sql.NullString
	}
	err := s.db.QueryRow(`SELECT id,kind,base_url,config_dir FROM connector WHERE id=?`, idParam(r, "id")).Scan(&c.ID, &c.Kind, &c.BaseURL, &c.ConfigDir)
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "connector not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !validConnectorKind(c.Kind) {
		errw(w, http.StatusNotFound, "unknown connector")
		return
	}
	defaultEntry := defaultModel
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	models, err := s.selectableModels(ctx, c.ID, c.Kind, c.BaseURL.String, c.ConfigDir.String, &defaultEntry)
	if err != nil {
		if (c.Kind == "opencode" || c.Kind == "copilot" || c.Kind == "local") && len(models) > 0 {
			models = append([]selectableModel{defaultEntry}, models...)
		} else {
			models = []selectableModel{defaultModel}
		}
		write(w, http.StatusOK, map[string]any{"models": models, "note": err.Error()})
		return
	}
	if c.Kind == "codex" && len(models) > 0 {
		efforts := append([]string{}, derefModelEfforts(models[0].Efforts)...)
		defaultEntry.Efforts = &efforts
		defaultEntry.DefaultEffort = models[0].DefaultEffort
	}
	write(w, http.StatusOK, map[string][]selectableModel{"models": append([]selectableModel{defaultEntry}, models...)})
}

func (s *Server) selectableModels(ctx context.Context, id, kind, baseURL, configDir string, defaultEntry *selectableModel) ([]selectableModel, error) {
	switch kind {
	case "claude":
		finder := s.chats.options.findClaude
		if finder == nil {
			finder = func() (string, error) { return connectorcli.Find(kind) }
		}
		path, err := finder()
		if err != nil {
			return nil, err
		}
		env := connectorcli.Env(kind, configDir, "")
		claudeModels, err := loadClaudeModels(ctx, s.chats.options, id, path, env)
		if err != nil {
			return nil, err
		}
		filtered := make([]selectableModel, 0, len(claudeModels))
		for _, model := range claudeModels {
			if model.ID == "default" {
				*defaultEntry = model
				defaultEntry.ID = ""
				defaultEntry.Label = "Default"
				defaultEntry.Description = "Use connector default"
				continue
			}
			filtered = append(filtered, model)
		}
		return filtered, nil
	case "codex":
		path, err := connectorcli.Find(kind)
		if err != nil {
			return nil, err
		}
		client, err := connectorcli.StartCodex(ctx, path, connectorcli.Env(kind, configDir, ""), configDir != "", s.version)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		models, err := client.Models(ctx)
		return convertModels(models), err
	case "opencode", "copilot":
		path, err := connectorcli.Find(kind)
		if err != nil {
			return nil, err
		}
		secret := ""
		if kind == "copilot" {
			secret, err = keyring.Get(connectorService, connectorSecret(id))
			if errors.Is(err, keyring.ErrNotFound) {
				err = nil
			}
			if err != nil {
				return nil, err
			}
		}
		env := connectorcli.Env(kind, configDir, secret)
		args := []string{"models"}
		if kind == "copilot" {
			args = []string{"--help"}
		}
		var output []byte
		var runErr error
		if kind == "opencode" {
			output, runErr = connectorcli.OpencodeCommandCombinedOutput(ctx, path, args, env)
		} else {
			cmd := exec.CommandContext(ctx, path, args...)
			cmd.Env = env
			proc.HideConsole(cmd)
			output, runErr = cmd.CombinedOutput()
		}
		if runErr != nil {
			return nil, runErr
		}
		if kind == "opencode" {
			models := parseOpenCodeModels(string(output))
			if err := addOpenCodeEfforts(ctx, path, env, models, defaultEntry); err != nil {
				return models, err
			}
			return models, nil
		}
		models := parseCopilotModels(string(output))
		// Levels only come from models.list; a failed lookup leaves them unknown.
		lookup := s.chats.options.copilotModels
		if lookup == nil {
			lookup = copilotModelsList
		}
		catalog, err := lookup(ctx, path, env)
		if err != nil {
			return models, fmt.Errorf("read Copilot reasoning levels: %w", err)
		}
		for _, item := range catalog {
			if item.isDefault {
				defaultEntry.Efforts, defaultEntry.DefaultEffort = item.Efforts, item.DefaultEffort
			}
			for i := range models {
				if models[i].ID == item.ID {
					models[i].Efforts, models[i].DefaultEffort = item.Efforts, item.DefaultEffort
				}
			}
		}
		return models, nil
	case "local":
		secret, err := keyring.Get(connectorService, connectorSecret(id))
		if errors.Is(err, keyring.ErrNotFound) {
			err = nil
		}
		if err != nil {
			return nil, err
		}
		u, err := url.Parse(baseURL)
		if err != nil || !validConnectorURL(baseURL) {
			return nil, errors.New("invalid local model endpoint")
		}
		ids, err := fetchLocalModelIDs(ctx, u, secret)
		if err != nil {
			return nil, err
		}
		models := make([]selectableModel, 0, len(ids))
		for _, modelID := range ids {
			models = append(models, selectableModel{ID: modelID, Label: modelID})
		}
		// Only Ollama reports thinking levels; OpenAI-compatible servers stay unknown.
		clearLocalModelCapabilities(id)
		d := &localRuntime{connectorID: id, ollamaURL: ollamaOrigin(baseURL), secret: secret, ctx: ctx, client: &http.Client{}}
		d.detectOllama()
		if d.ollama {
			var connectorModel string
			_ = s.db.QueryRow(`SELECT model FROM connector WHERE id=?`, id).Scan(&connectorModel)
			var lookupErr error
			for i := range models {
				d.model = models[i].ID
				caps, ok := d.ollamaModelCapabilities(ctx)
				if !ok {
					lookupErr = fmt.Errorf("read Ollama thinking levels for %s", models[i].ID)
					continue
				}
				models[i].Efforts, models[i].DefaultEffort = ollamaEfforts(caps)
				if models[i].ID == connectorModel {
					defaultEntry.Efforts, defaultEntry.DefaultEffort = models[i].Efforts, models[i].DefaultEffort
				}
			}
			return models, lookupErr
		}
		return models, nil
	default:
		return nil, errors.New("unknown connector kind")
	}
}

func loadClaudeModels(ctx context.Context, options Options, connectorID, path string, env []string) ([]selectableModel, error) {
	if cached, ok := claudeModelCache.Load(connectorID); ok {
		return cached.([]selectableModel), nil
	}
	lookup := options.claudeModels
	if lookup == nil {
		lookup = claudeInitializeModels
	}
	models, err := lookup(ctx, path, env)
	if err != nil {
		return nil, err
	}
	cached, _ := claudeModelCache.LoadOrStore(connectorID, models)
	return cached.([]selectableModel), nil
}

func claudeEffortFor(ctx context.Context, options Options, connectorID, path, configDir, model, level string) string {
	models, err := loadClaudeModels(ctx, options, connectorID, path, connectorcli.Env("claude", configDir, ""))
	if err != nil {
		return ""
	}
	if model == "" {
		model = "default"
	}
	for _, item := range models {
		if item.ID == model && hasEffort(level, derefModelEfforts(item.Efforts)) {
			return level
		}
	}
	return ""
}

func convertModels(models []connectorcli.Model) []selectableModel {
	out := make([]selectableModel, 0, len(models))
	for _, model := range models {
		efforts := append([]string{}, model.Efforts...)
		out = append(out, selectableModel{ID: model.ID, Label: model.Label, Description: model.Description, Efforts: &efforts, DefaultEffort: model.DefaultEffort})
	}
	return out
}

func derefModelEfforts(efforts *[]string) []string {
	if efforts == nil {
		return nil
	}
	return *efforts
}

func ollamaEfforts(capabilities ollamaModelCapabilities) (*[]string, string) {
	if len(capabilities.thinkingValues) > 0 {
		efforts := append([]string{}, capabilities.thinkingValues...)
		return &efforts, capabilities.thinkingDefault
	}
	if capabilities.thinking {
		efforts := []string{"none", "thinking"}
		return &efforts, capabilities.thinkingDefault
	}
	efforts := []string{}
	return &efforts, ""
}

func claudeInitializeModels(ctx context.Context, path string, env []string) ([]selectableModel, error) {
	cmd := exec.CommandContext(ctx, path, "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose")
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
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if _, err := io.WriteString(stdin, "{\"type\":\"control_request\",\"request_id\":\"r1\",\"request\":{\"subtype\":\"initialize\"}}\n"); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var response struct {
			Type     string `json:"type"`
			Response struct {
				Response struct {
					Models []struct {
						Value                 string   `json:"value"`
						DisplayName           string   `json:"displayName"`
						SupportsEffort        *bool    `json:"supportsEffort"`
						SupportedEffortLevels []string `json:"supportedEffortLevels"`
					} `json:"models"`
				} `json:"response"`
				RequestID string `json:"request_id"`
			} `json:"response"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) != nil || response.Type != "control_response" || response.Response.RequestID != "r1" {
			continue
		}
		models := make([]selectableModel, 0, len(response.Response.Response.Models))
		for _, model := range response.Response.Response.Models {
			efforts := append([]string{}, model.SupportedEffortLevels...)
			if model.SupportsEffort == nil || !*model.SupportsEffort {
				efforts = []string{}
			}
			models = append(models, selectableModel{ID: model.Value, Label: model.DisplayName, Efforts: &efforts})
		}
		return models, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("Claude initialize response missing")
}

func copilotModelsList(ctx context.Context, path string, env []string) ([]selectableModel, error) {
	cmd := exec.CommandContext(ctx, path, "--headless", "--stdio")
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
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"models.list","params":{}}`)
	if _, err := fmt.Fprintf(stdin, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(stdout)
	length := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(strings.TrimSpace(line), "") {
			break
		}
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "Content-Length: %d", &length); err != nil {
			return nil, err
		}
	}
	if length < 1 || length > 4<<20 {
		return nil, errors.New("invalid Copilot models.list response length")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}
	return parseCopilotModelCatalog(data)
}

func parseCopilotModelCatalog(data []byte) ([]selectableModel, error) {
	var response struct {
		ID     *int            `json:"id"`
		Error  json.RawMessage `json:"error"`
		Result *struct {
			Models []struct {
				ID                        string   `json:"id"`
				Name                      string   `json:"name"`
				DisplayName               string   `json:"displayName"`
				IsDefault                 bool     `json:"isDefault"`
				SupportedReasoningEfforts []string `json:"supportedReasoningEfforts"`
				DefaultReasoningEffort    string   `json:"defaultReasoningEffort"`
				Capabilities              struct {
					Supports struct {
						ReasoningEffort bool `json:"reasoningEffort"`
					} `json:"supports"`
				} `json:"capabilities"`
			} `json:"models"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	if response.ID == nil || *response.ID != 1 || (len(response.Error) > 0 && string(response.Error) != "null") || response.Result == nil {
		return nil, fmt.Errorf("Copilot models.list failed: %s", data)
	}
	models := make([]selectableModel, 0, len(response.Result.Models))
	for _, item := range response.Result.Models {
		label := item.DisplayName
		if label == "" {
			label = item.Name
		}
		if label == "" {
			label = item.ID
		}
		model := selectableModel{ID: item.ID, Label: label, isDefault: item.IsDefault}
		if item.Capabilities.Supports.ReasoningEffort {
			efforts := append([]string{}, item.SupportedReasoningEfforts...)
			model.Efforts, model.DefaultEffort = &efforts, item.DefaultReasoningEffort
		}
		models = append(models, model)
	}
	return models, nil
}

func addOpenCodeEfforts(ctx context.Context, path string, env []string, models []selectableModel, defaultEntry *selectableModel) error {
	output, err := connectorcli.OpencodeCommandCombinedOutput(ctx, path, []string{"api", "GET", "/api/model"}, env)
	if err != nil {
		return err
	}
	var catalog struct {
		Data []struct {
			ID         string `json:"id"`
			ProviderID string `json:"providerID"`
			Variants   *[]struct {
				ID string `json:"id"`
			} `json:"variants"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &catalog); err != nil || catalog.Data == nil {
		return errors.New("invalid OpenCode model catalog")
	}
	for _, model := range catalog.Data {
		if model.Variants == nil {
			continue
		}
		id := model.ProviderID + "/" + model.ID
		for i := range models {
			if models[i].ID == id {
				efforts := make([]string, 0, len(*model.Variants))
				for _, variant := range *model.Variants {
					efforts = append(efforts, variant.ID)
				}
				models[i].Efforts = &efforts
			}
		}
	}
	output, err = connectorcli.OpencodeCommandCombinedOutput(ctx, path, []string{"api", "GET", "/api/model/default"}, env)
	if err != nil {
		return err
	}
	var response struct {
		Data *struct {
			Variants *[]struct {
				ID string `json:"id"`
			} `json:"variants"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &response); err != nil || response.Data == nil {
		return errors.New("invalid OpenCode default model catalog")
	}
	if response.Data.Variants != nil {
		efforts := make([]string, 0, len(*response.Data.Variants))
		for _, variant := range *response.Data.Variants {
			efforts = append(efforts, variant.ID)
		}
		defaultEntry.Efforts = &efforts
	}
	return nil
}

func parseOpenCodeModels(output string) []selectableModel {
	var models []selectableModel
	for _, line := range strings.Split(output, "\n") {
		id := strings.TrimSpace(line)
		if id != "" {
			models = append(models, selectableModel{ID: id, Label: id})
		}
	}
	return models
}

var modelHelpChoices = regexp.MustCompile(`(?i)(?:choices|possible values):\s*([^\n]+)`)

func parseCopilotModels(output string) []selectableModel {
	lines := strings.Split(output, "\n")
	var modelOption string
	for i, line := range lines {
		if !strings.Contains(line, "--model") {
			continue
		}
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "--") {
			end++
		}
		modelOption = strings.Join(lines[i:end], " ")
		break
	}
	match := modelHelpChoices.FindStringSubmatch(modelOption)
	if len(match) != 2 {
		return nil
	}
	var models []selectableModel
	for _, choice := range strings.Split(match[1], ",") {
		id := strings.TrimSpace(choice)
		if id != "" {
			models = append(models, selectableModel{ID: id, Label: id})
		}
	}
	return models
}
