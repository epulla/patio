package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type settingsDefaults struct {
	ConnectorID *string `json:"connector_id"`
	Folder      *string `json:"folder"`
	AutoAllow   bool    `json:"auto_allow"`
	Tools       bool    `json:"tools"`
	Thinking    bool    `json:"thinking"`
}

func (s *Server) chatDefaults() (settingsDefaults, error) {
	defaults := settingsDefaults{AutoAllow: true, Tools: true}
	rows, err := s.db.Query(`SELECT key,value FROM app_setting`)
	if err != nil {
		return defaults, err
	}
	values := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			_ = rows.Close()
			return defaults, err
		}
		values[key] = value
	}
	rowsErr := rows.Err()
	if err := rows.Close(); err != nil {
		rowsErr = errors.Join(rowsErr, err)
	}
	if rowsErr != nil {
		return defaults, rowsErr
	}
	if raw, ok := values["connector_id"]; ok {
		if err := json.Unmarshal([]byte(raw), &defaults.ConnectorID); err != nil {
			return defaults, err
		}
	}
	if defaults.ConnectorID != nil {
		var exists int
		err := s.db.QueryRow(`SELECT 1 FROM connector WHERE id=?`, *defaults.ConnectorID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			defaults.ConnectorID = nil
		} else if err != nil {
			return defaults, err
		}
	}
	if raw, ok := values["folder"]; ok {
		if err := json.Unmarshal([]byte(raw), &defaults.Folder); err != nil {
			return defaults, err
		}
	}
	if defaults.Folder != nil {
		folder, allowed, err := s.allowedFolderPath(*defaults.Folder)
		if err != nil {
			return defaults, err
		}
		if !allowed {
			defaults.Folder = nil
		} else {
			defaults.Folder = &folder
		}
	}
	if raw, ok := values["auto_allow"]; ok {
		if err := json.Unmarshal([]byte(raw), &defaults.AutoAllow); err != nil {
			return defaults, err
		}
	}
	if raw, ok := values["tools"]; ok {
		if err := json.Unmarshal([]byte(raw), &defaults.Tools); err != nil {
			return defaults, err
		}
	}
	if raw, ok := values["thinking"]; ok {
		if err := json.Unmarshal([]byte(raw), &defaults.Thinking); err != nil {
			return defaults, err
		}
	}
	return defaults, nil
}

func (s *Server) getSettingsDefaults(w http.ResponseWriter, _ *http.Request) {
	defaults, err := s.chatDefaults()
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, defaults)
}

func (s *Server) putSettingsDefaults(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConnectorID json.RawMessage `json:"connector_id"`
		Folder      json.RawMessage `json:"folder"`
		AutoAllow   *bool           `json:"auto_allow"`
		Tools       *bool           `json:"tools"`
		Thinking    *bool           `json:"thinking"`
	}
	if decode(r, &in) != nil || len(in.ConnectorID) == 0 || len(in.Folder) == 0 || in.AutoAllow == nil || in.Tools == nil || in.Thinking == nil {
		errw(w, http.StatusBadRequest, "connector_id, folder, auto_allow, tools, and thinking required")
		return
	}
	var connectorID, folder *string
	if string(in.ConnectorID) != "null" {
		var value string
		if err := json.Unmarshal(in.ConnectorID, &value); err != nil || !required(value) {
			errw(w, http.StatusBadRequest, "connector must exist or be null")
			return
		}
		var exists int
		if err := s.db.QueryRow(`SELECT 1 FROM connector WHERE id=?`, value).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			errw(w, http.StatusBadRequest, "connector not found")
			return
		} else if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		connectorID = &value
	}
	if string(in.Folder) != "null" {
		var value string
		if err := json.Unmarshal(in.Folder, &value); err != nil || !required(value) {
			errw(w, http.StatusBadRequest, "folder must be an allowed folder or null")
			return
		}
		cleaned, allowed, err := s.allowedFolderPath(value)
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !allowed {
			errw(w, http.StatusBadRequest, "folder is not allowed")
			return
		}
		folder = &cleaned
	}
	values := map[string]any{
		"connector_id": connectorID,
		"folder":       folder,
		"auto_allow":   *in.AutoAllow,
		"tools":        *in.Tools,
		"thinking":     *in.Thinking,
	}
	encoded := make(map[string]string, len(values))
	for key, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
		encoded[key] = string(data)
	}
	if err := txrun(s.db, func(tx *sql.Tx) error {
		for key, value := range encoded {
			if _, err := tx.Exec(`INSERT INTO app_setting(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, settingsDefaults{ConnectorID: connectorID, Folder: folder, AutoAllow: *in.AutoAllow, Tools: *in.Tools, Thinking: *in.Thinking})
}

func (s *Server) allowedFolderPath(path string) (string, bool, error) {
	cleaned, err := cleanExistingDir(path)
	if err != nil {
		return "", false, nil
	}
	rows, err := s.db.Query(`SELECT path FROM allowed_folder`)
	if err != nil {
		return "", false, err
	}
	allowed := false
	for rows.Next() {
		var root string
		if err := rows.Scan(&root); err != nil {
			_ = rows.Close()
			return "", false, err
		}
		root, err := cleanExistingDir(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, cleaned)
		if err != nil {
			_ = rows.Close()
			return "", false, err
		}
		if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			allowed = true
		}
	}
	rowsErr := rows.Err()
	if err := rows.Close(); err != nil {
		rowsErr = errors.Join(rowsErr, err)
	}
	return cleaned, allowed, rowsErr
}

type settingsCounts struct {
	Boards     int `json:"boards"`
	Tickets    int `json:"tickets"`
	Chats      int `json:"chats"`
	Connectors int `json:"connectors"`
	MCPServers int `json:"mcp_servers"`
	Skills     int `json:"skills"`
}

type settingsAbout struct {
	Version       string         `json:"version"`
	OS            string         `json:"os"`
	Arch          string         `json:"arch"`
	DBPath        string         `json:"db_path"`
	DataDir       string         `json:"data_dir"`
	DataDirBytes  int64          `json:"data_dir_bytes"`
	Approximate   bool           `json:"approximate"`
	Counts        settingsCounts `json:"counts"`
	SchemaVersion int            `json:"schema_version"`
	ShowHidden    bool           `json:"show_hidden"`
	SecretsStore  string         `json:"secrets_store"`
	RepoURL       string         `json:"repo_url"`
}

func (s *Server) settingsAbout(w http.ResponseWriter, _ *http.Request) {
	about := settingsAbout{
		Version: s.version, OS: runtime.GOOS, Arch: runtime.GOARCH,
		DBPath: s.dbPath, DataDir: s.dbDir, ShowHidden: s.showHidden,
		SecretsStore: secretsStore(), RepoURL: "https://github.com/epulla/my-tickets",
	}
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&about.SchemaVersion); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	queries := []struct {
		query string
		count *int
	}{
		{`SELECT count(*) FROM board WHERE deleted_at IS NULL`, &about.Counts.Boards},
		{`SELECT count(*) FROM ticket`, &about.Counts.Tickets},
		{`SELECT count(*) FROM chat`, &about.Counts.Chats},
		{`SELECT count(*) FROM connector`, &about.Counts.Connectors},
		{`SELECT count(*) FROM mcp_server`, &about.Counts.MCPServers},
		{`SELECT count(*) FROM skill`, &about.Counts.Skills},
	}
	for _, item := range queries {
		if err := s.db.QueryRow(item.query).Scan(item.count); err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	const maxEntries = 200000
	entries := 0
	err := filepath.WalkDir(s.dbDir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entries++
		if entries > maxEntries {
			about.Approximate = true
			return fs.SkipAll
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			about.DataDirBytes += info.Size()
		}
		return nil
	})
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, about)
}

func secretsStore() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS Keychain"
	case "windows":
		return "Windows Credential Manager"
	default:
		return "Secret Service"
	}
}

func (s *Server) revealSettingsTarget(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Target string `json:"target"`
	}
	if decode(r, &in) != nil || in.Target != "database" && in.Target != "data_dir" {
		errw(w, http.StatusBadRequest, `target must be "database" or "data_dir"`)
		return
	}
	path := s.dbPath
	if in.Target == "data_dir" {
		path = s.dbDir
	}
	if err := s.revealFile(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			errw(w, http.StatusNotFound, err.Error())
		} else {
			errw(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	noContent(w)
}
