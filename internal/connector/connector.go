package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Install struct {
	Command string `json:"command"`
	URL     string `json:"url"`
}

type Status struct {
	Kind      string   `json:"kind"`
	Installed *bool    `json:"installed"`
	Path      string   `json:"path"`
	Version   string   `json:"version"`
	LoggedIn  *bool    `json:"logged_in"`
	Account   string   `json:"account"`
	Plan      string   `json:"plan"`
	Note      string   `json:"note"`
	Install   *Install `json:"install"`
}

type Options struct {
	Kind          string
	Path          string
	ConfigDir     string
	Secret        string
	Version       string
	GitHubBaseURL string
	HTTPClient    *http.Client
}

var opencodeCommandMu sync.Mutex

func lockOpencodeCommand(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if opencodeCommandMu.TryLock() {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func OpencodeCommandOutput(ctx context.Context, path string, args []string, env []string) (string, error) {
	if err := lockOpencodeCommand(ctx); err != nil {
		return "", err
	}
	defer opencodeCommandMu.Unlock()
	return commandOutput(ctx, path, args, env)
}

func OpencodeCommandCombinedOutput(ctx context.Context, path string, args []string, env []string) ([]byte, error) {
	if err := lockOpencodeCommand(ctx); err != nil {
		return nil, err
	}
	defer opencodeCommandMu.Unlock()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	configureCommandCancel(cmd)
	return cmd.CombinedOutput()
}

func cliName(kind string) string {
	switch kind {
	case "claude":
		return "claude"
	case "codex":
		return "codex"
	case "opencode":
		return "opencode"
	case "copilot":
		return "copilot"
	default:
		return ""
	}
}

func Env(kind, configDir, secret string) []string {
	env := os.Environ()
	var extra []string
	if configDir != "" {
		switch kind {
		case "claude":
			extra = append(extra, "CLAUDE_CONFIG_DIR="+configDir)
		case "codex":
			extra = append(extra, "CODEX_HOME="+configDir)
		case "opencode":
			extra = append(extra, "XDG_DATA_HOME="+filepath.Join(configDir, "data"), "XDG_CONFIG_HOME="+filepath.Join(configDir, "config"))
		case "copilot":
			extra = append(extra, "COPILOT_HOME="+configDir)
		}
	}
	if kind == "copilot" && secret != "" {
		extra = append(extra, "COPILOT_GITHUB_TOKEN="+secret)
	}
	return replaceEnv(env, extra)
}

func replaceEnv(env, values []string) []string {
	for _, value := range values {
		key := strings.SplitN(value, "=", 2)[0] + "="
		for i, old := range env {
			if strings.HasPrefix(old, key) {
				env[i] = value
				goto next
			}
		}
		env = append(env, value)
	next:
	}
	return env
}

func codexArgs(isolated bool) []string {
	if isolated {
		return []string{"-c", `cli_auth_credentials_store="file"`, "app-server"}
	}
	return []string{"app-server"}
}

func find(kind string) (string, error) {
	cli := cliName(kind)
	if cli == "" {
		return "", errors.New("unknown CLI")
	}
	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath(cli); err == nil {
			if found, err := absoluteFile(path); err == nil {
				return found, nil
			}
		}
		return findInDirs(cli+".exe", filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "OpenAI", "Codex", "bin"), filepath.Join(os.Getenv("USERPROFILE"), ".local", "bin"))
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		out, err := exec.CommandContext(ctx, shell, "-ilc", "command -v "+cli).Output()
		cancel()
		if err == nil {
			lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
			for i := len(lines) - 1; i >= 0; i-- {
				if path := strings.TrimSpace(lines[i]); path != "" {
					if filepath.IsAbs(path) {
						if found, err := absoluteFile(path); err == nil {
							return found, nil
						}
					}
					break
				}
			}
		}
	}
	home, _ := os.UserHomeDir()
	dirs := []string{}
	switch cli {
	case "claude":
		dirs = append(dirs, filepath.Join(home, ".local", "bin"))
	case "codex":
		dirs = append(dirs, filepath.Join(home, ".local", "bin"))
		if root := os.Getenv("CODEX_HOME"); root != "" {
			if strings.HasPrefix(root, "~/") {
				root = filepath.Join(home, root[2:])
			}
			dirs = append(dirs, filepath.Join(root, "packages", "standalone", "current", "bin"))
		}
	case "opencode":
		dirs = append(dirs, filepath.Join(home, ".opencode", "bin"))
	case "copilot":
		dirs = append(dirs, filepath.Join(home, ".local", "bin"))
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".npm-global", "bin"))
	if path, err := exec.LookPath(cli); err == nil {
		if found, err := absoluteFile(path); err == nil {
			return found, nil
		}
	}
	return findInDirs(cli, dirs...)
}

func findInDirs(name string, dirs ...string) (string, error) {
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if path, err := absoluteFile(filepath.Join(dir, name)); err == nil {
			return path, nil
		}
	}
	return "", errors.New("CLI not found")
}

func absoluteFile(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("path is not absolute")
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", errors.New("not a file")
	}
	return filepath.Clean(path), nil
}

func installHint(kind, goos string) *Install {
	windows := goos == "windows"
	if windows && kind == "opencode" {
		return nil
	}
	commands := map[string][2]string{
		"claude":   {"curl -fsSL https://claude.ai/install.sh | bash", "irm https://claude.ai/install.ps1 | iex"},
		"codex":    {"curl -fsSL https://chatgpt.com/codex/install.sh | sh", "irm https://chatgpt.com/codex/install.ps1 | iex"},
		"opencode": {"curl -fsSL https://opencode.ai/v2/install | bash", ""},
		"copilot":  {"curl -fsSL https://gh.io/copilot-install | bash", "winget install GitHub.Copilot"},
	}
	docs := map[string]string{
		"claude": "https://code.claude.com/docs/en/setup", "codex": "https://developers.openai.com/codex/cli",
		"opencode": "https://opencode.ai/docs/", "copilot": "https://docs.github.com/en/copilot/how-tos/set-up/install-copilot-cli",
	}
	command, ok := commands[kind]
	if !ok {
		return nil
	}
	if windows {
		return &Install{Command: command[1], URL: docs[kind]}
	}
	return &Install{Command: command[0], URL: docs[kind]}
}

func Check(ctx context.Context, opts Options) (out Status) {
	out = Status{Kind: opts.Kind}
	defer func() { redactStatus(&out, opts.Secret) }()
	if opts.Kind == "local" {
		return out
	}
	installed := false
	out.Installed = &installed
	if opts.Path == "" {
		path, err := find(opts.Kind)
		if err != nil {
			out.Install = installHint(opts.Kind, runtime.GOOS)
			out.Note = "CLI not found"
			return out
		}
		opts.Path = path
	}
	installed = true
	out.Path = opts.Path
	run := commandOutput
	if opts.Kind == "opencode" {
		run = OpencodeCommandOutput
	}
	version, err := run(ctx, opts.Path, []string{"--version"}, Env(opts.Kind, opts.ConfigDir, opts.Secret))
	if err != nil {
		out.Note = "version: " + err.Error()
		return out
	}
	out.Version = firstLine(version)
	var statusErr error
	switch opts.Kind {
	case "claude":
		statusErr = claudeStatus(ctx, &out, opts)
	case "codex":
		statusErr = codexStatus(ctx, &out, opts)
	case "opencode":
		major, err := parseOpencodeMajor(version)
		if err != nil {
			out.Note = "version: " + err.Error()
			return out
		}
		statusErr = openCodeStatus(ctx, &out, opts, major)
	case "copilot":
		statusErr = copilotStatus(ctx, &out, opts)
	default:
		statusErr = errors.New("unknown connector kind")
	}
	if statusErr != nil {
		out.Note = statusErr.Error()
	}
	return out
}

var opencodeVersion = regexp.MustCompile(`(?:^|[^0-9])v?([0-9]+)\.[0-9]+(?:\.[0-9]+)?`)

func parseOpencodeMajor(version string) (int, error) {
	match := opencodeVersion.FindStringSubmatch(strings.TrimSpace(version))
	if len(match) != 2 {
		return 0, fmt.Errorf("could not parse opencode version %q", strings.TrimSpace(version))
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, err
	}
	return major, nil
}

func opencodeMajor(ctx context.Context, path string, env []string) (int, error) {
	version, err := OpencodeCommandOutput(ctx, path, []string{"--version"}, env)
	if err != nil {
		return 0, err
	}
	return parseOpencodeMajor(version)
}

func OpencodeMajor(ctx context.Context, path string, env []string) (int, error) {
	return opencodeMajor(ctx, path, env)
}

func commandOutput(ctx context.Context, path string, args []string, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	configureCommandCancel(cmd)
	out, err := cmd.Output()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

func firstLine(text string) string {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

type claudeOutput struct {
	LoggedIn         *bool  `json:"loggedIn"`
	Email            string `json:"email"`
	SubscriptionType string `json:"subscriptionType"`
}

func parseClaude(raw string) (loggedIn *bool, account, plan string, err error) {
	var parsed claudeOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, "", "", err
	}
	return parsed.LoggedIn, parsed.Email, parsed.SubscriptionType, nil
}

func claudeStatus(ctx context.Context, out *Status, opts Options) error {
	raw, runErr := commandOutput(ctx, opts.Path, []string{"auth", "status", "--json"}, Env(opts.Kind, opts.ConfigDir, opts.Secret))
	logged, account, plan, parseErr := parseClaude(raw)
	if parseErr != nil {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("parse auth status: %w", parseErr)
	}
	out.LoggedIn, out.Account, out.Plan = logged, account, plan
	if runErr != nil && logged != nil && !*logged {
		return nil
	}
	return runErr
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
var credentialCount = regexp.MustCompile(`(?i)\b(\d+)\s+credentials?\b`)

func parseOpenCodeAuth(raw string) bool {
	text := ansiEscape.ReplaceAllString(raw, "")
	if match := credentialCount.FindStringSubmatch(text); len(match) == 2 {
		return match[1] != "0"
	}
	return strings.Contains(strings.ToLower(text), "credentials") && !strings.Contains(strings.ToLower(text), "no credentials")
}

func parseOpenCodeAuthJSON(raw string) (bool, string, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return false, "", err
	}
	if entries == nil && strings.TrimSpace(raw) != "[]" {
		return false, "", errors.New("expected an auth list array")
	}
	for _, entry := range entries {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(entry, &fields); err != nil || fields == nil {
			continue
		}
		for _, key := range []string{"name", "email", "account", "provider"} {
			var value string
			if json.Unmarshal(fields[key], &value) == nil && strings.TrimSpace(value) != "" {
				return true, value, nil
			}
		}
	}
	return len(entries) > 0, "", nil
}

func openCodeStatus(ctx context.Context, out *Status, opts Options, major int) error {
	args := []string{"auth", "list"}
	if major >= 2 {
		args = append(args, "--format", "json")
	}
	raw, err := OpencodeCommandOutput(ctx, opts.Path, args, Env(opts.Kind, opts.ConfigDir, opts.Secret))
	if err != nil {
		return err
	}
	logged := false
	if major >= 2 {
		var account string
		logged, account, err = parseOpenCodeAuthJSON(raw)
		if err != nil {
			return fmt.Errorf("parse auth list: %w", err)
		}
		out.Account = account
		if !logged {
			out.Note = "Free opencode models only. Run opencode auth login for other providers."
		}
	} else {
		logged = parseOpenCodeAuth(raw)
	}
	out.LoggedIn = &logged
	return nil
}

func copilotStatus(ctx context.Context, out *Status, opts Options) error {
	if opts.Secret == "" {
		out.Note = "checked on first chat"
		return nil
	}
	base := strings.TrimRight(opts.GitHubBaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/user", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+opts.Secret)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := clientCopy.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub /user returned %s", resp.Status)
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&user); err != nil {
		return err
	}
	logged := user.Login != ""
	out.LoggedIn, out.Account, out.Plan = &logged, user.Login, "unknown"
	return nil
}

func redactStatus(out *Status, secret string) {
	if secret == "" {
		return
	}
	for _, value := range []*string{&out.Version, &out.Account, &out.Plan, &out.Note} {
		*value = strings.ReplaceAll(*value, secret, "[redacted]")
	}
}

func codexStatus(ctx context.Context, out *Status, opts Options) error {
	client, err := StartCodex(ctx, opts.Path, Env(opts.Kind, opts.ConfigDir, opts.Secret), opts.ConfigDir != "", opts.Version)
	if err != nil {
		return err
	}
	defer client.Close()
	var response json.RawMessage
	if err := client.Call(ctx, "account/read", map[string]any{}, &response); err != nil {
		return err
	}
	out.LoggedIn, out.Account, out.Plan, err = parseCodexAccount(response)
	if err != nil {
		return fmt.Errorf("parse account/read: %w", err)
	}
	return nil
}

func parseCodexAccount(raw []byte) (loggedIn *bool, account, plan string, err error) {
	var response struct {
		Account *struct {
			Email    string `json:"email"`
			PlanType string `json:"planType"`
		} `json:"account"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, "", "", err
	}
	logged := response.Account != nil
	if response.Account == nil {
		return &logged, "", "", nil
	}
	return &logged, response.Account.Email, response.Account.PlanType, nil
}
