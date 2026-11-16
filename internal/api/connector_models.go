package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
	connectorcli "patio/internal/connector"
	"patio/internal/proc"
)

type selectableModel struct {
	ID          string    `json:"id"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
	Efforts     *[]string `json:"efforts,omitempty"`
}

var defaultModel = selectableModel{ID: "", Label: "Default", Description: "Use connector default"}
var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}
var copilotEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

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
	if c.Kind == "local" {
		clearLocalModelCapabilities(c.ID)
	}
	defaultEntry := defaultModel
	if c.Kind == "claude" {
		defaultEntry.Efforts = &claudeEfforts
	}
	if c.Kind == "copilot" {
		defaultEntry.Efforts = &copilotEfforts
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	models, err := s.selectableModels(ctx, c.ID, c.Kind, c.BaseURL.String, c.ConfigDir.String, &defaultEntry)
	if err != nil {
		if c.Kind == "opencode" && len(models) > 0 {
			models = append([]selectableModel{defaultEntry}, models...)
		} else {
			models = []selectableModel{defaultModel}
		}
		write(w, http.StatusOK, map[string]any{"models": models, "note": err.Error()})
		return
	}
	if c.Kind == "codex" && len(models) > 0 {
		efforts := append([]string{}, (*models[0].Efforts)...)
		defaultEntry.Efforts = &efforts
	}
	write(w, http.StatusOK, map[string][]selectableModel{"models": append([]selectableModel{defaultEntry}, models...)})
}

func (s *Server) selectableModels(ctx context.Context, id, kind, baseURL, configDir string, defaultEntry *selectableModel) ([]selectableModel, error) {
	switch kind {
	case "claude":
		return []selectableModel{{ID: "opus", Label: "Opus", Description: "Most capable", Efforts: &claudeEfforts}, {ID: "sonnet", Label: "Sonnet", Description: "Fast and capable", Efforts: &claudeEfforts}, {ID: "haiku", Label: "Haiku", Description: "Fastest", Efforts: &claudeEfforts}}, nil
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
		args := []string{"models"}
		if kind == "copilot" {
			args = []string{"--help"}
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
		for i := range models {
			models[i].Efforts = &copilotEfforts
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
		for _, id := range ids {
			models = append(models, selectableModel{ID: id, Label: id})
		}
		return models, nil
	default:
		return nil, errors.New("unknown connector kind")
	}
}

func convertModels(models []connectorcli.Model) []selectableModel {
	out := make([]selectableModel, 0, len(models))
	for _, model := range models {
		efforts := append([]string{}, model.Efforts...)
		out = append(out, selectableModel{ID: model.ID, Label: model.Label, Description: model.Description, Efforts: &efforts})
	}
	return out
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
