package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
	connectorcli "patio/internal/connector"
)

type selectableModel struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

var defaultModel = selectableModel{ID: "", Label: "Default", Description: "Use connector default"}

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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	models, err := s.selectableModels(ctx, c.ID, c.Kind, c.BaseURL.String, c.ConfigDir.String)
	if err != nil {
		write(w, http.StatusOK, map[string]any{"models": []selectableModel{defaultModel}, "note": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string][]selectableModel{"models": append([]selectableModel{defaultModel}, models...)})
}

func (s *Server) selectableModels(ctx context.Context, id, kind, baseURL, configDir string) ([]selectableModel, error) {
	switch kind {
	case "claude":
		return []selectableModel{{"opus", "Opus", "Most capable"}, {"sonnet", "Sonnet", "Fast and capable"}, {"haiku", "Haiku", "Fastest"}}, nil
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
			output, runErr = cmd.CombinedOutput()
		}
		if runErr != nil {
			return nil, runErr
		}
		if kind == "opencode" {
			return parseOpenCodeModels(string(output)), nil
		}
		return parseCopilotModels(string(output)), nil
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
		out = append(out, selectableModel{model.ID, model.Label, model.Description})
	}
	return out
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
