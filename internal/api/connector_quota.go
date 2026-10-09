package api

import (
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
	"time"

	"github.com/zalando/go-keyring"
	connectorquota "my-tickets/internal/connector"
)

func (s *Server) connectorQuota(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID        string
		Kind      string
		ConfigDir *string
		QuotaJSON sql.NullString
	}
	err := s.db.QueryRow(`SELECT id,kind,config_dir,quota_json FROM connector WHERE id=?`, idParam(r, "id")).Scan(&c.ID, &c.Kind, &c.ConfigDir, &c.QuotaJSON)
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "connector not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.Kind == "claude" {
		quota := connectorquota.Quota{Kind: "claude", Windows: []connectorquota.Window{}, Note: "Unknown until the first chat"}
		if c.QuotaJSON.Valid {
			if err := json.Unmarshal([]byte(c.QuotaJSON.String), &quota); err != nil {
				quota = connectorquota.Quota{Kind: "claude", Windows: []connectorquota.Window{}, Note: "Invalid stored quota"}
			} else {
				quota.Kind = "claude"
				if quota.Windows == nil {
					quota.Windows = []connectorquota.Window{}
				}
			}
		}
		write(w, http.StatusOK, quota)
		return
	}
	if c.Kind == "local" {
		write(w, http.StatusOK, connectorquota.Quota{Kind: "local", Windows: []connectorquota.Window{}, Note: "No quota for local models"})
		return
	}
	secret := ""
	if c.Kind == "copilot" {
		secret, err = keyring.Get(connectorService, connectorSecret(c.ID))
		if errors.Is(err, keyring.ErrNotFound) {
			err = nil
		}
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	configDir := ""
	if c.ConfigDir != nil {
		configDir = *c.ConfigDir
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	quota := connectorquota.ReadQuota(ctx, connectorquota.Options{Kind: c.Kind, ConfigDir: configDir, Secret: secret, Version: s.version})
	write(w, http.StatusOK, quota)
}

type localModelsRequest struct {
	BaseURL string  `json:"base_url"`
	Secret  *string `json:"secret"`
	ID      string  `json:"id"`
}

func (s *Server) localModels(w http.ResponseWriter, r *http.Request) {
	var in localModelsRequest
	if decode(r, &in) != nil {
		errw(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	u, err := url.Parse(in.BaseURL)
	if err != nil || !validConnectorURL(in.BaseURL) || u.RawQuery != "" || u.Fragment != "" {
		errw(w, http.StatusBadRequest, "valid http or https base_url required")
		return
	}
	secret := ""
	if in.ID != "" {
		var kind string
		if err := s.db.QueryRow(`SELECT kind FROM connector WHERE id=?`, in.ID).Scan(&kind); errors.Is(err, sql.ErrNoRows) {
			errw(w, http.StatusNotFound, "connector not found")
			return
		} else if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		} else if kind != "local" {
			errw(w, http.StatusBadRequest, "id must identify a local connector")
			return
		}
		if in.Secret == nil {
			secret, err = keyring.Get(connectorService, connectorSecret(in.ID))
			if errors.Is(err, keyring.ErrNotFound) {
				err = nil
			}
			if err != nil {
				errw(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	if in.Secret != nil {
		secret = *in.Secret
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	u.RawPath = ""
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		errw(w, http.StatusBadRequest, "invalid base_url")
		return
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	client := *http.DefaultClient
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		errw(w, http.StatusBadGateway, safeUpstreamMessage(err.Error(), secret))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		detail := strings.TrimSpace(string(message))
		if detail == "" {
			detail = resp.Status
		}
		errw(w, http.StatusBadGateway, safeUpstreamMessage(fmt.Sprintf("model endpoint returned %s: %s", resp.Status, detail), secret))
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		errw(w, http.StatusBadGateway, "model response too large or unreadable")
		return
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		errw(w, http.StatusBadGateway, "invalid model response")
		return
	}
	models := make([]string, 0, len(result.Data))
	for _, model := range result.Data {
		models = append(models, safeUpstreamMessage(model.ID, secret))
	}
	sort.Strings(models)
	write(w, http.StatusOK, map[string][]string{"models": models})
}

func safeUpstreamMessage(message, secret string) string {
	if secret != "" {
		message = strings.ReplaceAll(message, secret, "[redacted]")
	}
	return message
}
