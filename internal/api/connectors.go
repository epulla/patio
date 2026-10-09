package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zalando/go-keyring"
)

const connectorService = "my-tickets"

type connector struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Name      string  `json:"name"`
	ConfigDir *string `json:"config_dir"`
	BaseURL   string  `json:"base_url"`
	Model     string  `json:"model"`
	QuotaJSON *string `json:"quota_json"`
	CreatedAt string  `json:"created_at"`
	HasSecret bool    `json:"has_secret"`
}

func connectorSecret(id string) string { return "connector:" + id }

func cleanupConnectorDir(path string, cause error) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("%w; cleanup config directory: %v", cause, err)
	}
	return cause
}

func (s *Server) readConnector(row interface{ Scan(...any) error }) (connector, error) {
	var c connector
	err := row.Scan(&c.ID, &c.Kind, &c.Name, &c.ConfigDir, &c.BaseURL, &c.Model, &c.QuotaJSON, &c.CreatedAt)
	if err == nil {
		_, err = keyring.Get(connectorService, connectorSecret(c.ID))
		if errors.Is(err, keyring.ErrNotFound) {
			err = nil
		} else if err == nil {
			c.HasSecret = true
		}
	}
	return c, err
}

func (s *Server) listConnectors(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(`SELECT id,kind,name,config_dir,base_url,model,quota_json,created_at FROM connector ORDER BY created_at,rowid`)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []connector{}
	for rows.Next() {
		c, err := s.readConnector(rows)
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, out)
}

type connectorInput struct {
	Kind             string  `json:"kind"`
	Name             string  `json:"name"`
	UseExistingLogin bool    `json:"use_existing_login"`
	BaseURL          string  `json:"base_url"`
	Model            string  `json:"model"`
	Secret           *string `json:"secret"`
}

func validConnectorKind(kind string) bool {
	switch kind {
	case "claude", "codex", "opencode", "copilot", "local":
		return true
	default:
		return false
	}
}

func validConnectorURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}

func (s *Server) createConnector(w http.ResponseWriter, r *http.Request) {
	var in connectorInput
	if decode(r, &in) != nil || !validConnectorKind(in.Kind) || !required(in.Name) {
		errw(w, 400, "valid kind and name required")
		return
	}
	if in.Kind == "local" {
		if in.UseExistingLogin || !validConnectorURL(in.BaseURL) {
			errw(w, 400, "local connector requires a valid http or https base_url")
			return
		}
	}
	if in.Kind != "copilot" && in.Kind != "local" && in.Secret != nil {
		errw(w, 400, "secret is only supported for local and copilot connectors")
		return
	}
	if in.Kind == "copilot" && !in.UseExistingLogin && (in.Secret == nil || !required(*in.Secret)) {
		errw(w, 400, "copilot connector requires a PAT secret")
		return
	}
	if in.Kind == "opencode" && !in.UseExistingLogin && runtime.GOOS == "windows" {
		errw(w, 400, "isolated opencode connectors are not supported on Windows")
		return
	}
	if in.UseExistingLogin {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM connector WHERE kind=? AND config_dir IS NULL`, in.Kind).Scan(&count); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if count > 0 {
			errw(w, 400, "connector already uses existing login for this kind")
			return
		}
	}
	id, created := uid(), now()
	var configDir *string
	if in.Kind != "local" && !in.UseExistingLogin {
		dir := filepath.Join(s.dbDir, "connectors", id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			errw(w, 500, err.Error())
			return
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			errw(w, 500, cleanupConnectorDir(dir, err).Error())
			return
		}
		configDir = &dir
	}
	if in.Secret != nil && *in.Secret != "" {
		if err := keyring.Set(connectorService, connectorSecret(id), *in.Secret); err != nil {
			if configDir != nil {
				err = cleanupConnectorDir(*configDir, err)
			}
			errw(w, 500, err.Error())
			return
		}
	}
	_, err := s.db.Exec(`INSERT INTO connector(id,kind,name,config_dir,base_url,model,created_at) VALUES(?,?,?,?,?,?,?)`, id, in.Kind, in.Name, configDir, in.BaseURL, in.Model, created)
	if err != nil {
		_, secretErr := keyring.Get(connectorService, connectorSecret(id))
		if secretErr == nil {
			secretErr = keyring.Delete(connectorService, connectorSecret(id))
		}
		if configDir != nil {
			err = cleanupConnectorDir(*configDir, err)
		}
		if secretErr != nil && !errors.Is(secretErr, keyring.ErrNotFound) {
			err = fmt.Errorf("%w; cleanup secret: %v", err, secretErr)
		}
		if strings.Contains(err.Error(), "UNIQUE") {
			errw(w, 400, "connector already uses existing login for this kind")
		} else {
			errw(w, 500, err.Error())
		}
		return
	}
	c, err := s.readConnector(s.db.QueryRow(`SELECT id,kind,name,config_dir,base_url,model,quota_json,created_at FROM connector WHERE id=?`, id))
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 201, c)
}

type connectorUpdate struct {
	Kind             *string `json:"kind"`
	UseExistingLogin *bool   `json:"use_existing_login"`
	Name             *string `json:"name"`
	BaseURL          *string `json:"base_url"`
	Model            *string `json:"model"`
	Secret           *string `json:"secret"`
}

func restoreSecret(id, value string, exists bool) error {
	if exists {
		return keyring.Set(connectorService, connectorSecret(id), value)
	}
	err := keyring.Delete(connectorService, connectorSecret(id))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func (s *Server) updateConnector(w http.ResponseWriter, r *http.Request) {
	var in connectorUpdate
	if decode(r, &in) != nil || in.Kind != nil || in.UseExistingLogin != nil {
		errw(w, 400, "invalid JSON or immutable connector fields")
		return
	}
	id := idParam(r, "id")
	var old connector
	err := s.db.QueryRow(`SELECT id,kind,name,config_dir,base_url,model,quota_json,created_at FROM connector WHERE id=?`, id).Scan(&old.ID, &old.Kind, &old.Name, &old.ConfigDir, &old.BaseURL, &old.Model, &old.QuotaJSON, &old.CreatedAt)
	if err == sql.ErrNoRows {
		errw(w, 404, "connector not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	name, baseURL, model := old.Name, old.BaseURL, old.Model
	if in.Name != nil {
		name = *in.Name
	}
	if in.BaseURL != nil {
		baseURL = *in.BaseURL
	}
	if in.Model != nil {
		model = *in.Model
	}
	if !required(name) || old.Kind == "local" && !validConnectorURL(baseURL) {
		errw(w, 400, "name required; local connector requires a valid http or https base_url")
		return
	}
	if in.Secret != nil && old.Kind != "local" && old.Kind != "copilot" {
		errw(w, 400, "secret is only supported for local and copilot connectors")
		return
	}
	var previous string
	hadPrevious, changedSecret := false, false
	if in.Secret != nil {
		previous, err = keyring.Get(connectorService, connectorSecret(id))
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			errw(w, 500, err.Error())
			return
		}
		hadPrevious = err == nil
		if *in.Secret == "" {
			err = keyring.Delete(connectorService, connectorSecret(id))
			if errors.Is(err, keyring.ErrNotFound) {
				err = nil
			}
		} else {
			err = keyring.Set(connectorService, connectorSecret(id), *in.Secret)
		}
		if err != nil {
			errw(w, 500, err.Error())
			return
		}
		changedSecret = true
	}
	_, err = s.db.Exec(`UPDATE connector SET name=?,base_url=?,model=? WHERE id=?`, name, baseURL, model, id)
	if err != nil {
		if changedSecret {
			if restoreErr := restoreSecret(id, previous, hadPrevious); restoreErr != nil {
				err = fmt.Errorf("%w; restore secret: %v", err, restoreErr)
			}
		}
		errw(w, 500, err.Error())
		return
	}
	c, err := s.readConnector(s.db.QueryRow(`SELECT id,kind,name,config_dir,base_url,model,quota_json,created_at FROM connector WHERE id=?`, id))
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	write(w, 200, c)
}

func insideConnectorDir(root, path string) bool {
	root, path = filepath.Clean(root), filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (s *Server) deleteConnector(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	var c connector
	err := s.db.QueryRow(`SELECT id,kind,name,config_dir,base_url,model,quota_json,created_at FROM connector WHERE id=?`, id).Scan(&c.ID, &c.Kind, &c.Name, &c.ConfigDir, &c.BaseURL, &c.Model, &c.QuotaJSON, &c.CreatedAt)
	if err == sql.ErrNoRows {
		errw(w, 404, "connector not found")
		return
	}
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	secret, err := keyring.Get(connectorService, connectorSecret(id))
	hasSecret := err == nil
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		errw(w, 500, err.Error())
		return
	}
	if hasSecret {
		if err := keyring.Delete(connectorService, connectorSecret(id)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			errw(w, 500, err.Error())
			return
		}
	}
	res, err := s.db.Exec(`DELETE FROM connector WHERE id=?`, id)
	if err == nil {
		var n int64
		n, err = res.RowsAffected()
		if err == nil && n == 0 {
			err = sql.ErrNoRows
		}
	}
	if err != nil {
		if hasSecret {
			if restoreErr := keyring.Set(connectorService, connectorSecret(id), secret); restoreErr != nil {
				err = fmt.Errorf("%w; restore secret: %v", err, restoreErr)
			}
		}
		if errors.Is(err, sql.ErrNoRows) {
			errw(w, 404, "connector not found")
		} else {
			errw(w, 500, err.Error())
		}
		return
	}
	if err := s.chats.CloseConnector(id); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.ConfigDir != nil {
		root := filepath.Join(s.dbDir, "connectors")
		if insideConnectorDir(root, *c.ConfigDir) {
			if err := os.RemoveAll(*c.ConfigDir); err != nil {
				errw(w, 500, err.Error())
				return
			}
		}
	}
	noContent(w)
}
