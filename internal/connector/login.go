package connector

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func Find(kind string) (string, error) { return find(kind) }

func InstallHint(kind, goos string) *Install { return installHint(kind, goos) }

func BuildLoginCommand(goos, kind, path string, env []string) (string, error) {
	if !filepath.IsAbs(path) && !(goos == "windows" && (len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') || strings.HasPrefix(path, `\\`))) {
		return "", errors.New("CLI path must be absolute")
	}
	args := "auth login"
	switch kind {
	case "claude", "opencode":
	case "copilot":
		args = "login --device-code"
	default:
		return "", errors.New("connector kind does not support terminal login")
	}
	allowed := map[string]bool{"CLAUDE_CONFIG_DIR": true, "XDG_DATA_HOME": true, "XDG_CONFIG_HOME": true, "COPILOT_HOME": true}
	values := make(map[string]string)
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && allowed[key] && ((key == "CLAUDE_CONFIG_DIR" && kind == "claude") || (strings.HasPrefix(key, "XDG_") && kind == "opencode") || (key == "COPILOT_HOME" && kind == "copilot")) {
			values[key] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if goos == "windows" {
		parts := make([]string, 0, len(keys)+1)
		for _, key := range keys {
			parts = append(parts, `set "`+key+`=`+strings.ReplaceAll(values[key], `"`, `^"`)+`"`)
		}
		parts = append(parts, `"`+path+`" `+args)
		return strings.Join(parts, " && "), nil
	}
	parts := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		parts = append(parts, "export "+key+"="+shellQuote(values[key]))
	}
	parts = append(parts, shellQuote(path)+" "+args)
	return strings.Join(parts, "; "), nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'" }

func TerminalInvocation(goos, command string) (string, []string, error) {
	switch goos {
	case "darwin":
		script := `tell application "Terminal" to do script "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(command) + `"`
		return "osascript", []string{"-e", script, "-e", `tell application "Terminal" to activate`}, nil
	case "windows":
		return "cmd", []string{"/c", "start", "", "cmd", "/k", command}, nil
	case "linux":
		return "x-terminal-emulator", []string{"-e", "sh", "-c", command}, nil
	default:
		return "", nil, fmt.Errorf("unsupported operating system %q", goos)
	}
}

func LaunchTerminal(command string) error {
	name, args, err := TerminalInvocation(runtime.GOOS, command)
	if err != nil {
		return err
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open terminal: %w", err)
	}
	return cmd.Process.Release()
}

func BeginCodexLogin(ctx context.Context, client *Client) (string, error) {
	var response struct {
		Type    string `json:"type"`
		AuthURL string `json:"authUrl"`
		LoginID string `json:"loginId"`
	}
	if err := client.Call(ctx, "account/login/start", map[string]string{"type": "chatgpt"}, &response); err != nil {
		return "", err
	}
	if response.Type != "chatgpt" || response.AuthURL == "" || response.LoginID == "" {
		return "", errors.New("invalid account/login/start response")
	}
	return response.AuthURL, nil
}

func WaitCodexLogin(ctx context.Context, client *Client) error {
	defer client.Close()
	for {
		select {
		case notification, ok := <-client.Notifications():
			if !ok {
				return errors.New("codex app-server exited before login completed")
			}
			if notification.Method == "account/login/completed" {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
