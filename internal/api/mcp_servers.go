package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/zalando/go-keyring"
)

type mcpServer struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Command        string   `json:"command"`
	Args           []string `json:"args"`
	EnvKeys        []string `json:"env_keys"`
	URL            string   `json:"url"`
	Auth           string   `json:"auth"`
	Enabled        bool     `json:"enabled"`
	HasBearerToken bool     `json:"has_bearer_token"`
	HasOAuthToken  bool     `json:"has_oauth_token"`
	CreatedAt      string   `json:"created_at"`
	argsJSON       string
	envKeysJSON    string
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
}

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
	if err := row.Scan(&server.ID, &server.Name, &server.Kind, &server.Command, &server.argsJSON, &server.envKeysJSON, &server.URL, &server.Auth, &enabled, &server.CreatedAt); err != nil {
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
	return server, err
}

const mcpServerSelect = `SELECT id,name,kind,command,args_json,env_keys_json,url,auth,enabled,created_at FROM mcp_server`

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
	Name        *string         `json:"name"`
	Kind        *string         `json:"kind"`
	Command     *string         `json:"command"`
	Args        *[]string       `json:"args"`
	Env         json.RawMessage `json:"env"`
	URL         *string         `json:"url"`
	Auth        *string         `json:"auth"`
	BearerToken *string         `json:"bearer_token"`
	Enabled     *bool           `json:"enabled"`
}

type mcpServerConfig struct {
	Name    string
	Kind    string
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Auth    string
	Enabled bool
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
	if strings.EqualFold(sanitized, "my-tickets") {
		return errors.New("name produces reserved identifier my-tickets")
	}
	if !validMCPEnv(config.Env) {
		return errors.New("environment keys must match [A-Za-z_][A-Za-z0-9_]*")
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
	_, err = s.db.Exec(`INSERT INTO mcp_server(id,name,kind,command,args_json,env_keys_json,url,auth,enabled,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, config.Name, config.Kind, config.Command, string(argsJSON), keysJSON, config.URL, config.Auth, config.Enabled, now())
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
	config := mcpServerConfig{Name: old.Name, Kind: old.Kind, Command: old.Command, Args: old.Args, URL: old.URL, Auth: old.Auth, Enabled: old.Enabled}
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
	envSecret, hasEnv, err := snapshotMCPSecret(id, "env")
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	previous := map[string]string{"env": envSecret, "bearer": bearer, "oauth": oauth}
	present := map[string]bool{"env": hasEnv, "bearer": hasBearer, "oauth": hasOAuth}
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
	argsJSON, err := json.Marshal(config.Args)
	if err != nil {
		if restoreErr := restoreMCPSecrets(id, previous, present, changed); restoreErr != nil {
			err = fmt.Errorf("%w; restore secrets: %v", err, restoreErr)
		}
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = s.db.Exec(`UPDATE mcp_server SET name=?,kind=?,command=?,args_json=?,env_keys_json=?,url=?,auth=?,enabled=? WHERE id=?`, config.Name, config.Kind, config.Command, string(argsJSON), keysJSON, config.URL, config.Auth, config.Enabled, id)
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
	for _, suffix := range []string{"env", "bearer", "oauth"} {
		value, exists, err := snapshotMCPSecret(id, suffix)
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		previous[suffix], present[suffix] = value, exists
	}
	changed := []string{}
	for _, suffix := range []string{"env", "bearer", "oauth"} {
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
		specs = append(specs, spec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return specs, nil
}
