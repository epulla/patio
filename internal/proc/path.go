package proc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	pathMarker    = "__PATIO_PATH__"
	loadedPathEnv = "PATIO_LOGIN_PATH"
)

// LoadLoginShellPath merges the login shell's PATH into this process's PATH,
// so apps started from the Dock or Finder find the user's tools. It does
// nothing on Windows, when started from a terminal, or when a parent Patio
// process already did it. Failures keep the current PATH.
func LoadLoginShellPath() {
	if runtime.GOOS == "windows" || os.Getenv("TERM_PROGRAM") != "" || os.Getenv(loadedPathEnv) != "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, loginShell(runtime.GOOS, os.Getenv("SHELL")), "-ilc", `printf '\n`+pathMarker+`%s`+pathMarker+`\n' "$PATH"`)
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return
	}
	shellPath, ok := parseShellPath(string(out))
	if !ok {
		return
	}
	_ = os.Setenv("PATH", MergePath(shellPath, os.Getenv("PATH")))
	_ = os.Setenv(loadedPathEnv, "1")
}

func loginShell(goos, shell string) string {
	if filepath.IsAbs(shell) {
		return shell
	}
	if goos == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

func parseShellPath(out string) (string, bool) {
	start := strings.LastIndex(out, pathMarker)
	if start < 0 {
		return "", false
	}
	before := out[:start]
	open := strings.LastIndex(before, pathMarker)
	if open < 0 {
		return "", false
	}
	value := strings.TrimSpace(before[open+len(pathMarker):])
	return value, value != ""
}

// MergePath joins PATH lists, keeping the first occurrence of each entry.
func MergePath(lists ...string) string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, dir := range filepath.SplitList(list) {
			key := dir
			if runtime.GOOS == "windows" {
				key = strings.ToLower(strings.TrimRight(dir, `\`))
			}
			if dir == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, dir)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}
