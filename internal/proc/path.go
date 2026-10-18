package proc

import (
	"context"
	"os"
	"os/exec"
	"path"
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
	if path.IsAbs(shell) {
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
	return mergePath(os.PathListSeparator, runtime.GOOS == "windows", lists...)
}

func mergePath(sep rune, windows bool, lists ...string) string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, dir := range strings.Split(list, string(sep)) {
			dir = strings.TrimSpace(dir)
			key := dir
			if windows {
				key = strings.ToLower(strings.TrimRight(dir, `\`))
			}
			if dir == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, dir)
		}
	}
	return strings.Join(out, string(sep))
}

// expandPercent expands Windows-style %VAR% references; unknown ones stay as is.
func expandPercent(s string, getenv func(string) string) string {
	var b strings.Builder
	for {
		start := strings.IndexByte(s, '%')
		if start < 0 {
			break
		}
		end := strings.IndexByte(s[start+1:], '%')
		if end < 0 {
			break
		}
		end += start + 1
		name := s[start+1 : end]
		if value := getenv(name); name != "" && value != "" {
			b.WriteString(s[:start])
			b.WriteString(value)
			s = s[end+1:]
			continue
		}
		b.WriteString(s[:end])
		s = s[end:]
	}
	b.WriteString(s)
	return b.String()
}

// registryPath builds PATH the way a new Windows session would (machine, then
// user), keeping entries only present in the current PATH at the end.
func registryPath(machine, user, current string, getenv func(string) string) string {
	return mergePath(';', true, expandPercent(machine, getenv), expandPercent(user, getenv), current)
}
