package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"
	"patio/internal/proc"
)

type mcpServer struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Kind                 string   `json:"kind"`
	Command              string   `json:"command"`
	Args                 []string `json:"args"`
	EnvKeys              []string `json:"env_keys"`
	URL                  string   `json:"url"`
	Auth                 string   `json:"auth"`
	Enabled              bool     `json:"enabled"`
	HasBearerToken       bool     `json:"has_bearer_token"`
	HasOAuthToken        bool     `json:"has_oauth_token"`
	OAuthClientID        string   `json:"oauth_client_id"`
	OAuthScopes          []string `json:"oauth_scopes"`
	HasOAuthClientSecret bool     `json:"has_oauth_client_secret"`
	CreatedAt            string   `json:"created_at"`
	argsJSON             string
	envKeysJSON          string
}

type mcpServerSpec struct {
	Name        string
	Kind        string
	Command     string
	Args        []string
	Env         map[string]string
	URL         string
	Headers     map[string]string
	Auth        string
	OAuthHeader *string
	AuthError   error
	stderr      *mcpStderr
}

type mcpConnector func(context.Context, mcpServerSpec) (*mcp.ClientSession, func(), error)

func mcpServerSecret(id, suffix string) string { return "mcp:" + id + ":" + suffix }

func sanitizedName(name string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			out.WriteRune(r)
		} else {
			out.WriteByte('-')
		}
	}
	return out.String()
}

func readMCPServer(row interface{ Scan(...any) error }) (mcpServer, error) {
	var server mcpServer
	var enabled int
	var scopesJSON string
	if err := row.Scan(&server.ID, &server.Name, &server.Kind, &server.Command, &server.argsJSON, &server.envKeysJSON, &server.URL, &server.Auth, &enabled, &server.CreatedAt, &server.OAuthClientID, &scopesJSON); err != nil {
		return server, err
	}
	if err := json.Unmarshal([]byte(server.argsJSON), &server.Args); err != nil {
		return server, fmt.Errorf("decode MCP server args: %w", err)
	}
	if err := json.Unmarshal([]byte(server.envKeysJSON), &server.EnvKeys); err != nil {
		return server, fmt.Errorf("decode MCP server environment keys: %w", err)
	}
	if server.Args == nil {
		server.Args = []string{}
	}
	if server.EnvKeys == nil {
		server.EnvKeys = []string{}
	}
	sort.Strings(server.EnvKeys)
	server.Enabled = enabled != 0
	if err := json.Unmarshal([]byte(scopesJSON), &server.OAuthScopes); err != nil {
		return server, fmt.Errorf("decode MCP OAuth scopes: %w", err)
	}
	if server.OAuthScopes == nil {
		server.OAuthScopes = []string{}
	}
	_, err := keyring.Get(connectorService, mcpServerSecret(server.ID, "bearer"))
	if errors.Is(err, keyring.ErrNotFound) {
		err = nil
	} else if err == nil {
		server.HasBearerToken = true
	}
	if err != nil {
		return server, err
	}
	_, err = keyring.Get(connectorService, mcpServerSecret(server.ID, "oauth"))
	if errors.Is(err, keyring.ErrNotFound) {
		err = nil
	} else if err == nil {
		server.HasOAuthToken = true
	}
	if err != nil {
		return server, err
	}
	_, err = keyring.Get(connectorService, mcpServerSecret(server.ID, "oauth-registered-secret"))
	if errors.Is(err, keyring.ErrNotFound) {
		err = nil
	} else if err == nil {
		server.HasOAuthClientSecret = true
	}
	return server, err
}

const mcpServerSelect = `SELECT id,name,kind,command,args_json,env_keys_json,url,auth,enabled,created_at,oauth_client_id,oauth_scopes FROM mcp_server`

func (s *Server) loadMCPServer(id string) (mcpServer, error) {
	return readMCPServer(s.db.QueryRow(mcpServerSelect+` WHERE id=?`, id))
}

func (s *Server) listMCPServers(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(mcpServerSelect + ` ORDER BY created_at,rowid`)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []mcpServer{}
	for rows.Next() {
		server, err := readMCPServer(rows)
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, server)
	}
	if err := rows.Err(); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, out)
}

type mcpServerInput struct {
	Name              *string         `json:"name"`
	Kind              *string         `json:"kind"`
	Command           *string         `json:"command"`
	Args              *[]string       `json:"args"`
	Env               json.RawMessage `json:"env"`
	URL               *string         `json:"url"`
	Auth              *string         `json:"auth"`
	BearerToken       *string         `json:"bearer_token"`
	OAuthClientID     *string         `json:"oauth_client_id"`
	OAuthClientSecret *string         `json:"oauth_client_secret"`
	OAuthScopes       *[]string       `json:"oauth_scopes"`
	Enabled           *bool           `json:"enabled"`
}

type mcpServerConfig struct {
	OAuthClientID string
	OAuthScopes   []string
	Name          string
	Kind          string
	Command       string
	Args          []string
	Env           map[string]string
	URL           string
	Auth          string
	Enabled       bool
}

func validMCPAuth(auth string) bool {
	switch auth {
	case "none", "bearer", "oauth":
		return true
	default:
		return false
	}
}

func validMCPEnv(env map[string]string) bool {
	for key := range env {
		if key == "" || !(key[0] == '_' || key[0] >= 'A' && key[0] <= 'Z' || key[0] >= 'a' && key[0] <= 'z') {
			return false
		}
		for _, char := range key[1:] {
			if !(char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9') {
				return false
			}
		}
	}
	return true
}

func validMCPCommand(command string) bool {
	if command == "" {
		return false
	}
	proc.RefreshPath()
	if filepath.IsAbs(command) {
		_, err := exec.LookPath(command)
		return err == nil
	}
	if strings.ContainsAny(command, `/\\`) {
		return false
	}
	_, err := exec.LookPath(command)
	return err == nil
}

func validMCPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return false
	}
	return u.Scheme != "http" || strings.EqualFold(u.Hostname(), "localhost") || u.Hostname() == "127.0.0.1"
}

func validateMCPServer(config mcpServerConfig) error {
	if utf8.RuneCountInString(config.Name) < 1 || utf8.RuneCountInString(config.Name) > 64 {
		return errors.New("name must be 1 to 64 characters")
	}
	sanitized := sanitizedName(config.Name)
	if sanitized == "" {
		return errors.New("name must produce a CLI identifier")
	}
	if strings.EqualFold(sanitized, "patio") {
		return errors.New("name produces reserved identifier patio")
	}
	if !validMCPEnv(config.Env) {
		return errors.New("environment keys must match [A-Za-z_][A-Za-z0-9_]*")
	}
	if (config.OAuthClientID != "" || len(config.OAuthScopes) > 0) && (config.Kind != "http" || config.Auth != "oauth") {
		return errors.New("OAuth client fields require an http server with oauth auth")
	}
	if !validMCPAuth(config.Auth) {
		return errors.New("auth must be none, bearer, or oauth")
	}
	switch config.Kind {
	case "stdio":
		if !validMCPCommand(config.Command) {
			return errors.New("stdio command must be an existing absolute path or an executable found on PATH")
		}
		if config.Auth != "none" {
			return errors.New("stdio servers require auth none")
		}
	case "http":
		if !validMCPURL(config.URL) {
			return errors.New("http server requires a valid https URL or localhost http URL")
		}
	default:
		return errors.New("kind must be stdio or http")
	}
	return nil
}

func (s *Server) validateMCPName(name, excludeID string) error {
	rows, err := s.db.Query(`SELECT id,name FROM mcp_server`)
	if err != nil {
		return err
	}
	defer rows.Close()
	target := sanitizedName(name)
	for rows.Next() {
		var id, other string
		if err := rows.Scan(&id, &other); err != nil {
			return err
		}
		if id != excludeID && sanitizedName(other) == target {
			return errors.New("name produces an identifier already used by another MCP server")
		}
	}
	return rows.Err()
}

func encodeMCPEnv(env map[string]string) (string, string, error) {
	if env == nil {
		env = map[string]string{}
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	envJSON, err := json.Marshal(env)
	if err != nil {
		return "", "", err
	}
	keysJSON, err := json.Marshal(keys)
	return string(envJSON), string(keysJSON), err
}

func decodeMCPEnv(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var env map[string]string
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.New("env must be an object of string values")
	}
	if env == nil {
		return nil, errors.New("env must be an object of string values")
	}
	return env, nil
}

func applyMCPSecret(id, suffix, value string, present bool) error {
	if present {
		return keyring.Set(connectorService, mcpServerSecret(id, suffix), value)
	}
	err := keyring.Delete(connectorService, mcpServerSecret(id, suffix))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func snapshotMCPSecret(id, suffix string) (string, bool, error) {
	value, err := keyring.Get(connectorService, mcpServerSecret(id, suffix))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}
	return value, err == nil, err
}

func restoreMCPSecrets(id string, previous map[string]string, present map[string]bool, changed []string) error {
	var result error
	for _, suffix := range changed {
		if err := applyMCPSecret(id, suffix, previous[suffix], present[suffix]); err != nil {
			result = errors.Join(result, fmt.Errorf("restore %s secret: %w", suffix, err))
		}
	}
	return result
}

func (s *Server) createMCPServer(w http.ResponseWriter, r *http.Request) {
	var in mcpServerInput
	if decode(r, &in) != nil {
		errw(w, http.StatusBadRequest, "invalid MCP server JSON")
		return
	}
	config := mcpServerConfig{Auth: "none", Enabled: true, Args: []string{}, Env: map[string]string{}}
	if in.Name != nil {
		config.Name = *in.Name
	}
	if in.Kind != nil {
		config.Kind = *in.Kind
	}
	if in.Command != nil {
		config.Command = *in.Command
	}
	if in.Args != nil {
		config.Args = *in.Args
	}
	if len(in.Env) > 0 {
		env, err := decodeMCPEnv(in.Env)
		if err != nil {
			errw(w, http.StatusBadRequest, err.Error())
			return
		}
		config.Env = env
	}
	if in.URL != nil {
		config.URL = *in.URL
	}
	if in.Auth != nil {
		config.Auth = *in.Auth
	}
	if in.Enabled != nil {
		config.Enabled = *in.Enabled
	}
	if in.OAuthClientID != nil {
		config.OAuthClientID = *in.OAuthClientID
	}
	if in.OAuthScopes != nil {
		config.OAuthScopes = *in.OAuthScopes
	}
	if (in.OAuthClientID != nil || in.OAuthClientSecret != nil || in.OAuthScopes != nil) && (config.Kind != "http" || config.Auth != "oauth") {
		errw(w, http.StatusBadRequest, "OAuth client fields require an http server with oauth auth")
		return
	}
	if err := validateMCPServer(config); err != nil {
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	if config.Auth == "bearer" && (in.BearerToken == nil || *in.BearerToken == "") {
		errw(w, http.StatusBadRequest, "bearer auth requires bearer_token")
		return
	}
	if err := s.validateMCPName(config.Name, ""); err != nil {
		if strings.Contains(err.Error(), "identifier already used") {
			errw(w, http.StatusBadRequest, err.Error())
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	argsJSON, err := json.Marshal(config.Args)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	envJSON, keysJSON, err := encodeMCPEnv(config.Env)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := uid()
	scopesJSON, err := json.Marshal(config.OAuthScopes)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	changed := []string{}
	if len(config.Env) > 0 {
		if err := applyMCPSecret(id, "env", envJSON, true); err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, "env")
	}
	if config.Auth == "bearer" {
		if err := applyMCPSecret(id, "bearer", *in.BearerToken, true); err != nil {
			if restoreErr := restoreMCPSecrets(id, map[string]string{}, map[string]bool{}, changed); restoreErr != nil {
				err = fmt.Errorf("%w; cleanup secrets: %v", err, restoreErr)
			}
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, "bearer")
	}
	if in.OAuthClientSecret != nil && *in.OAuthClientSecret != "" {
		if err := applyMCPSecret(id, "oauth-registered-secret", *in.OAuthClientSecret, true); err != nil {
			cleanupErr := restoreMCPSecrets(id, map[string]string{}, map[string]bool{}, changed)
			if cleanupErr != nil {
				err = fmt.Errorf("%w; cleanup secrets: %v", err, cleanupErr)
			}
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, "oauth-registered-secret")
	}
	_, err = s.db.Exec(`INSERT INTO mcp_server(id,name,kind,command,args_json,env_keys_json,url,auth,enabled,created_at,oauth_client_id,oauth_scopes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, config.Name, config.Kind, config.Command, string(argsJSON), keysJSON, config.URL, config.Auth, config.Enabled, now(), config.OAuthClientID, string(scopesJSON))
	if err != nil {
		cleanupErr := restoreMCPSecrets(id, map[string]string{}, map[string]bool{}, changed)
		if cleanupErr != nil {
			err = fmt.Errorf("%w; cleanup secrets: %v", err, cleanupErr)
		}
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			errw(w, http.StatusBadRequest, "MCP server name or CLI identifier already exists")
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	server, err := s.loadMCPServer(id)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusCreated, server)
}

func (s *Server) updateMCPServer(w http.ResponseWriter, r *http.Request) {
	var in mcpServerInput
	if decode(r, &in) != nil {
		errw(w, http.StatusBadRequest, "invalid MCP server JSON")
		return
	}
	id := idParam(r, "id")
	old, err := s.loadMCPServer(id)
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "MCP server not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	config := mcpServerConfig{Name: old.Name, Kind: old.Kind, Command: old.Command, Args: old.Args, URL: old.URL, Auth: old.Auth, Enabled: old.Enabled, OAuthClientID: old.OAuthClientID, OAuthScopes: old.OAuthScopes}
	if in.Name != nil {
		config.Name = *in.Name
	}
	if in.Kind != nil {
		config.Kind = *in.Kind
	}
	if in.Command != nil {
		config.Command = *in.Command
	}
	if in.Args != nil {
		config.Args = *in.Args
	}
	if in.URL != nil {
		config.URL = *in.URL
	}
	if in.Auth != nil {
		config.Auth = *in.Auth
	}
	if in.Enabled != nil {
		config.Enabled = *in.Enabled
	}
	if in.OAuthClientID != nil {
		config.OAuthClientID = *in.OAuthClientID
	}
	if in.OAuthScopes != nil {
		config.OAuthScopes = *in.OAuthScopes
	}
	if (in.OAuthClientID != nil || in.OAuthClientSecret != nil || in.OAuthScopes != nil) && (config.Kind != "http" || config.Auth != "oauth") {
		errw(w, http.StatusBadRequest, "OAuth client fields require an http server with oauth auth")
		return
	}
	if config.Auth != "oauth" {
		config.OAuthClientID = ""
		config.OAuthScopes = []string{}
	}
	envJSON := ""
	keysJSON := old.envKeysJSON
	var env map[string]string
	if len(in.Env) > 0 {
		env, err = decodeMCPEnv(in.Env)
		if err != nil {
			errw(w, http.StatusBadRequest, err.Error())
			return
		}
		config.Env = env
		var err error
		envJSON, keysJSON, err = encodeMCPEnv(env)
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		config.Env = make(map[string]string, len(old.EnvKeys))
		if len(old.EnvKeys) > 0 {
			stored, err := keyring.Get(connectorService, mcpServerSecret(id, "env"))
			if err != nil {
				errw(w, http.StatusInternalServerError, err.Error())
				return
			}
			if err := json.Unmarshal([]byte(stored), &config.Env); err != nil {
				errw(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	if err := validateMCPServer(config); err != nil {
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.validateMCPName(config.Name, id); err != nil {
		if strings.Contains(err.Error(), "identifier already used") {
			errw(w, http.StatusBadRequest, err.Error())
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	bearer, hasBearer, err := snapshotMCPSecret(id, "bearer")
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	oauth, hasOAuth, err := snapshotMCPSecret(id, "oauth")
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	oauthClient, hasOAuthClient, err := snapshotMCPSecret(id, "oauth-client")
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	registeredSecret, hasRegisteredSecret, err := snapshotMCPSecret(id, "oauth-registered-secret")
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	envSecret, hasEnv, err := snapshotMCPSecret(id, "env")
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	previous := map[string]string{"env": envSecret, "bearer": bearer, "oauth": oauth, "oauth-client": oauthClient, "oauth-registered-secret": registeredSecret}
	present := map[string]bool{"env": hasEnv, "bearer": hasBearer, "oauth": hasOAuth, "oauth-client": hasOAuthClient, "oauth-registered-secret": hasRegisteredSecret}
	changed := []string{}
	if len(in.Env) > 0 {
		if err := applyMCPSecret(id, "env", envJSON, len(env) > 0); err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, "env")
	}
	if in.BearerToken != nil {
		if err := applyMCPSecret(id, "bearer", *in.BearerToken, *in.BearerToken != ""); err != nil {
			if restoreErr := restoreMCPSecrets(id, previous, present, changed); restoreErr != nil {
				err = fmt.Errorf("%w; restore secrets: %v", err, restoreErr)
			}
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, "bearer")
	}
	clientChanged := in.OAuthClientID != nil && *in.OAuthClientID != old.OAuthClientID
	if in.OAuthClientSecret != nil {
		if err := applyMCPSecret(id, "oauth-registered-secret", *in.OAuthClientSecret, *in.OAuthClientSecret != ""); err != nil {
			_ = restoreMCPSecrets(id, previous, present, changed)
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, "oauth-registered-secret")
	}
	if clientChanged || in.OAuthClientSecret != nil {
		if err := applyMCPSecret(id, "oauth", "", false); err != nil {
			_ = restoreMCPSecrets(id, previous, present, changed)
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, "oauth")
	}
	if config.Auth != "bearer" && present["bearer"] {
		if err := applyMCPSecret(id, "bearer", "", false); err != nil {
			if restoreErr := restoreMCPSecrets(id, previous, present, changed); restoreErr != nil {
				err = fmt.Errorf("%w; restore secrets: %v", err, restoreErr)
			}
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, "bearer")
	}
	if config.Auth != "oauth" && present["oauth"] {
		if err := applyMCPSecret(id, "oauth", "", false); err != nil {
			if restoreErr := restoreMCPSecrets(id, previous, present, changed); restoreErr != nil {
				err = fmt.Errorf("%w; restore secrets: %v", err, restoreErr)
			}
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, "oauth")
	}
	if config.Auth != "oauth" && present["oauth-client"] {
		if err := applyMCPSecret(id, "oauth-client", "", false); err != nil {
			if restoreErr := restoreMCPSecrets(id, previous, present, changed); restoreErr != nil {
				err = fmt.Errorf("%w; restore secrets: %v", err, restoreErr)
			}
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, "oauth-client")
	}
	if config.Auth != "oauth" && present["oauth-registered-secret"] {
		if err := applyMCPSecret(id, "oauth-registered-secret", "", false); err != nil {
			if restoreErr := restoreMCPSecrets(id, previous, present, changed); restoreErr != nil {
				err = fmt.Errorf("%w; restore secrets: %v", err, restoreErr)
			}
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, "oauth-registered-secret")
	}
	scopesJSON, err := json.Marshal(config.OAuthScopes)
	if err != nil {
		_ = restoreMCPSecrets(id, previous, present, changed)
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	argsJSON, err := json.Marshal(config.Args)
	if err != nil {
		if restoreErr := restoreMCPSecrets(id, previous, present, changed); restoreErr != nil {
			err = fmt.Errorf("%w; restore secrets: %v", err, restoreErr)
		}
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = s.db.Exec(`UPDATE mcp_server SET name=?,kind=?,command=?,args_json=?,env_keys_json=?,url=?,auth=?,enabled=?,oauth_client_id=?,oauth_scopes=? WHERE id=?`, config.Name, config.Kind, config.Command, string(argsJSON), keysJSON, config.URL, config.Auth, config.Enabled, config.OAuthClientID, string(scopesJSON), id)
	if err != nil {
		if restoreErr := restoreMCPSecrets(id, previous, present, changed); restoreErr != nil {
			err = fmt.Errorf("%w; restore secrets: %v", err, restoreErr)
		}
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			errw(w, http.StatusBadRequest, "MCP server name or CLI identifier already exists")
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	server, err := s.loadMCPServer(id)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, server)
}

func (s *Server) deleteMCPServer(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	if _, err := s.loadMCPServer(id); errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "MCP server not found")
		return
	} else if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	previous := map[string]string{}
	present := map[string]bool{}
	for _, suffix := range []string{"env", "bearer", "oauth", "oauth-client", "oauth-registered-secret"} {
		value, exists, err := snapshotMCPSecret(id, suffix)
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		previous[suffix], present[suffix] = value, exists
	}
	changed := []string{}
	for _, suffix := range []string{"env", "bearer", "oauth", "oauth-client", "oauth-registered-secret"} {
		if !present[suffix] {
			continue
		}
		if err := applyMCPSecret(id, suffix, "", false); err != nil {
			if restoreErr := restoreMCPSecrets(id, previous, present, changed); restoreErr != nil {
				err = fmt.Errorf("%w; restore secrets: %v", err, restoreErr)
			}
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, suffix)
	}
	res, err := s.db.Exec(`DELETE FROM mcp_server WHERE id=?`, id)
	if err == nil {
		var count int64
		count, err = res.RowsAffected()
		if err == nil && count == 0 {
			err = sql.ErrNoRows
		}
	}
	if err != nil {
		if restoreErr := restoreMCPSecrets(id, previous, present, changed); restoreErr != nil {
			err = fmt.Errorf("%w; restore secrets: %v", err, restoreErr)
		}
		if errors.Is(err, sql.ErrNoRows) {
			errw(w, http.StatusNotFound, "MCP server not found")
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	noContent(w)
}

func (s *Server) enabledMCPServers() ([]mcpServerSpec, error) {
	rows, err := s.db.Query(mcpServerSelect + ` WHERE enabled=1 ORDER BY created_at,rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	specs := []mcpServerSpec{}
	for rows.Next() {
		server, err := readMCPServer(rows)
		if err != nil {
			return nil, err
		}
		env := map[string]string{}
		if len(server.EnvKeys) > 0 {
			stored, err := keyring.Get(connectorService, mcpServerSecret(server.ID, "env"))
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal([]byte(stored), &env); err != nil {
				return nil, fmt.Errorf("decode MCP server environment: %w", err)
			}
		}
		spec := mcpServerSpec{Name: sanitizedName(server.Name), Kind: server.Kind, Command: server.Command, Args: server.Args, Env: env, URL: server.URL, Headers: map[string]string{}, Auth: server.Auth}
		if server.Auth == "bearer" {
			token, err := keyring.Get(connectorService, mcpServerSecret(server.ID, "bearer"))
			if err != nil && !errors.Is(err, keyring.ErrNotFound) {
				return nil, err
			}
			if err == nil {
				spec.Headers["Authorization"] = "Bearer " + token
			}
		}
		if server.Auth == "oauth" {
			if !server.HasOAuthToken {
				spec.AuthError = errors.New("Sign in again")
			} else {
				spec.OAuthHeader, spec.AuthError = s.oauthHeaderIfPresent(context.Background(), server)
			}
		}
		specs = append(specs, spec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return specs, nil
}

type mcpServerTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type mcpServerCheck struct {
	OK        bool            `json:"ok"`
	LatencyMS int64           `json:"latency_ms"`
	Tools     []mcpServerTool `json:"tools"`
	Error     string          `json:"error"`
}

func (s *Server) checkMCPServer(w http.ResponseWriter, r *http.Request) {
	server, err := s.loadMCPServer(idParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "MCP server not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	check, err := s.mcpCheck(r.Context(), server)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, check)
}

// mcpCheck connects to the server and lists its tools. A failed connection is reported in
// check.Error; err is only for Patio-side failures such as reading secrets.
func (s *Server) mcpCheck(ctx context.Context, server mcpServer) (check mcpServerCheck, err error) {
	started := time.Now()
	check = mcpServerCheck{Tools: []mcpServerTool{}}
	spec, err := s.mcpServerCheckSpec(ctx, server)
	if err != nil {
		return check, err
	}
	defer func() { check.LatencyMS = time.Since(started).Milliseconds() }()
	if spec.Auth == "oauth" && spec.OAuthHeader == nil {
		check.Error = "Sign in first"
		return check, nil
	}
	if spec.Kind == "stdio" {
		spec.stderr = &mcpStderr{}
	}
	ctx, cancel := context.WithTimeout(ctx, s.mcpCheckTimeout)
	defer cancel()
	session, cleanup, err := s.mcpConnect(ctx, spec)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		check.Error = withMCPStderr(err, spec.stderr).Error()
		return check, nil
	}
	tools, err := listMCPTools(ctx, session)
	if err != nil {
		check.Error = withMCPStderr(err, spec.stderr).Error()
	} else {
		check.OK = true
		check.Tools = tools
	}
	return check, nil
}

func (s *Server) mcpServerCheckSpec(ctx context.Context, server mcpServer) (mcpServerSpec, error) {
	spec := mcpServerSpec{
		Kind: server.Kind, Command: server.Command, Args: server.Args, URL: server.URL,
		Env: map[string]string{}, Headers: map[string]string{}, Auth: server.Auth,
	}
	if len(server.EnvKeys) > 0 {
		stored, err := keyring.Get(connectorService, mcpServerSecret(server.ID, "env"))
		if err != nil {
			return spec, err
		}
		if err := json.Unmarshal([]byte(stored), &spec.Env); err != nil {
			return spec, fmt.Errorf("decode MCP server environment: %w", err)
		}
	}
	switch server.Auth {
	case "bearer":
		token, err := keyring.Get(connectorService, mcpServerSecret(server.ID, "bearer"))
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return spec, err
		}
		if err == nil {
			spec.Headers["Authorization"] = "Bearer " + token
		}
	case "oauth":
		header, err := s.oauthHeaderIfPresent(ctx, server)
		if err != nil {
			return spec, err
		}
		spec.OAuthHeader = header
	}
	return spec, nil
}

func connectMCP(ctx context.Context, spec mcpServerSpec) (*mcp.ClientSession, func(), error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "patio-health-check", Version: "1"}, nil)
	var transport mcp.Transport
	var cmd *exec.Cmd
	var httpClient *http.Client
	switch spec.Kind {
	case "stdio":
		proc.RefreshPath()
		cmd = exec.Command(spec.Command, spec.Args...)
		cmd.Env = append(os.Environ(), mcpEnvironment(spec.Env)...)
		if spec.stderr == nil {
			spec.stderr = &mcpStderr{}
		}
		cmd.Stderr = spec.stderr
		proc.HideConsole(cmd)
		transport = &mcp.CommandTransport{Command: cmd, TerminateDuration: 250 * time.Millisecond}
	case "http":
		headers := make(http.Header)
		for name, value := range spec.Headers {
			headers.Set(name, value)
		}
		if spec.OAuthHeader != nil {
			headers.Set("Authorization", *spec.OAuthHeader)
		}
		base := http.DefaultTransport
		if defaultTransport, ok := base.(*http.Transport); ok {
			base = defaultTransport.Clone()
		}
		httpClient = &http.Client{Transport: mcpHeaderTransport{base: base, headers: headers}}
		transport = &mcp.StreamableClientTransport{Endpoint: spec.URL, HTTPClient: httpClient, DisableStandaloneSSE: true, MaxRetries: -1}
	default:
		return nil, nil, fmt.Errorf("unsupported MCP server kind %q", spec.Kind)
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		if httpClient != nil {
			httpClient.CloseIdleConnections()
		}
		return nil, nil, err
	}
	return session, func() {
		_ = session.Close()
		if httpClient != nil {
			httpClient.CloseIdleConnections()
		}
	}, nil
}

func listMCPTools(ctx context.Context, session *mcp.ClientSession) ([]mcpServerTool, error) {
	tools := make([]mcpServerTool, 0)
	cursor := ""
	seen := map[string]bool{}
	for len(tools) < 500 {
		result, err := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return tools, err
		}
		for _, tool := range result.Tools {
			if tool == nil {
				continue
			}
			tools = append(tools, mcpServerTool{Name: tool.Name, Description: tool.Description})
			if len(tools) == 500 {
				break
			}
		}
		if result.NextCursor == "" || len(tools) == 500 {
			break
		}
		if seen[result.NextCursor] {
			return tools, errors.New("MCP server repeated tools pagination cursor")
		}
		seen[result.NextCursor] = true
		cursor = result.NextCursor
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

func mcpEnvironment(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

type mcpHeaderTransport struct {
	base    http.RoundTripper
	headers http.Header
}

func (t mcpHeaderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	for name, values := range t.headers {
		for _, value := range values {
			clone.Header.Set(name, value)
		}
	}
	return t.base.RoundTrip(clone)
}

func (t mcpHeaderTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type mcpStderr struct {
	mu   sync.Mutex
	data []byte
}

func (s *mcpStderr) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	remaining := 4096 - len(s.data)
	if remaining > 0 {
		if len(data) < remaining {
			remaining = len(data)
		}
		s.data = append(s.data, data[:remaining]...)
	}
	return len(data), nil
}

func (s *mcpStderr) String() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(string(s.data))
}

func withMCPStderr(err error, stderr *mcpStderr) error {
	if message := stderr.String(); message != "" {
		return fmt.Errorf("%w: %s", err, message)
	}
	return err
}
