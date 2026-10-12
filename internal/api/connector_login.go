package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
	conn "patio/internal/connector"
)

var codexLogins = struct {
	sync.Mutex
	pending map[string]bool
}{pending: make(map[string]bool)}

func (s *Server) connectorLogin(w http.ResponseWriter, r *http.Request) {
	var c connector
	if err := s.db.QueryRow(`SELECT id,kind,config_dir FROM connector WHERE id=?`, idParam(r, "id")).Scan(&c.ID, &c.Kind, &c.ConfigDir); errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "connector not found")
		return
	} else if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.Kind == "local" {
		errw(w, http.StatusBadRequest, "local connectors don't log in")
		return
	}
	if c.Kind == "copilot" {
		if c.ConfigDir != nil {
			errw(w, http.StatusBadRequest, "PAT connectors don't log in")
			return
		}
		_, err := keyring.Get(connectorService, connectorSecret(c.ID))
		if err == nil {
			errw(w, http.StatusBadRequest, "PAT connectors don't log in")
			return
		}
		if !errors.Is(err, keyring.ErrNotFound) {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	configDir := ""
	if c.ConfigDir != nil {
		configDir = *c.ConfigDir
	}
	path, err := conn.Find(c.Kind)
	if err != nil {
		hint := conn.InstallHint(c.Kind, runtime.GOOS)
		message := "CLI not found"
		if hint != nil {
			message = fmt.Sprintf("CLI not found; install with %s (%s)", hint.Command, hint.URL)
		}
		errw(w, http.StatusBadRequest, message)
		return
	}
	if c.Kind == "codex" {
		s.codexLogin(w, c, path, configDir)
		return
	}
	command, err := conn.BuildLoginCommand(runtime.GOOS, c.Kind, path, conn.Env(c.Kind, configDir, ""))
	if err != nil {
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := conn.LaunchTerminal(command); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusAccepted, map[string]string{"status": "login started"})
}

func (s *Server) codexLogin(w http.ResponseWriter, c connector, path, configDir string) {
	codexLogins.Lock()
	if codexLogins.pending[c.ID] {
		codexLogins.Unlock()
		errw(w, http.StatusConflict, "Codex login already in progress")
		return
	}
	codexLogins.pending[c.ID] = true
	codexLogins.Unlock()
	finish := func() {
		codexLogins.Lock()
		delete(codexLogins.pending, c.ID)
		codexLogins.Unlock()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	client, err := conn.StartCodex(ctx, path, conn.Env(c.Kind, configDir, ""), configDir != "", s.version)
	if err != nil {
		cancel()
		finish()
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	authURL, err := conn.BeginCodexLogin(ctx, client)
	if err != nil {
		_ = client.Close()
		cancel()
		finish()
		errw(w, http.StatusBadRequest, err.Error())
		return
	}
	parsed, err := url.Parse(authURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		_ = client.Close()
		cancel()
		finish()
		errw(w, http.StatusBadRequest, "Codex returned an invalid login URL")
		return
	}
	if err := openLoginURL(authURL); err != nil {
		_ = client.Close()
		cancel()
		finish()
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	go func() {
		defer cancel()
		defer finish()
		_ = conn.WaitCodexLogin(ctx, client)
	}()
	write(w, http.StatusAccepted, map[string]string{"auth_url": authURL})
}

func openLoginURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return errors.New("invalid login URL")
	}
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{raw}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", raw}
	case "linux":
		name, args = "xdg-open", []string{raw}
	default:
		return fmt.Errorf("unsupported operating system %q", runtime.GOOS)
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open login URL: %w", err)
	}
	return cmd.Process.Release()
}
