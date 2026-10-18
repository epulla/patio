package proc

import (
	"os"
	"strings"
	"testing"
)

func TestParseShellPath(t *testing.T) {
	cases := []struct {
		name, out, want string
		ok              bool
	}{
		{"rc noise around markers", "Welcome!\n\n" + pathMarker + "/opt/homebrew/bin:/usr/bin" + pathMarker + "\nbye\n", "/opt/homebrew/bin:/usr/bin", true},
		{"marker echoed by rc file first", pathMarker + "junk\n\n" + pathMarker + "/usr/bin" + pathMarker + "\n", "/usr/bin", true},
		{"empty output", "", "", false},
		{"empty path", "\n" + pathMarker + pathMarker + "\n", "", false},
		{"single marker", pathMarker + "/usr/bin", "", false},
	}
	for _, c := range cases {
		got, ok := parseShellPath(c.out)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %q, %v; want %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestMergePath(t *testing.T) {
	sep := string(os.PathListSeparator)
	got := MergePath(strings.Join([]string{"/opt/homebrew/bin", "/usr/bin", ""}, sep), strings.Join([]string{"/usr/bin", "/bin"}, sep))
	want := strings.Join([]string{"/opt/homebrew/bin", "/usr/bin", "/bin"}, sep)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRegistryPath(t *testing.T) {
	env := map[string]string{"SystemRoot": `C:\Windows`, "USERPROFILE": `C:\Users\dome`}
	getenv := func(name string) string { return env[name] }
	machine := `%SystemRoot%\system32;%SystemRoot%;C:\Program Files\nodejs\`
	user := `%USERPROFILE%\.local\bin;%MISSING%\bin;;`
	current := `C:\WINDOWS\System32;C:\Patio\only-here`
	want := `C:\Windows\system32;C:\Windows;C:\Program Files\nodejs\;C:\Users\dome\.local\bin;%MISSING%\bin;C:\Patio\only-here`
	if got := registryPath(machine, user, current, getenv); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestExpandPercent(t *testing.T) {
	getenv := func(name string) string { return map[string]string{"A": "x"}[name] }
	cases := map[string]string{
		"%A%\\bin": "x\\bin",
		"100%":     "100%",
		"%%A%":     "%x",
		"%B%;%A%":  "%B%;x",
		"no vars":  "no vars",
		"%A%%A%":   "xx",
	}
	for in, want := range cases {
		if got := expandPercent(in, getenv); got != want {
			t.Errorf("expandPercent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoginShell(t *testing.T) {
	if got := loginShell("darwin", "/opt/homebrew/bin/fish"); got != "/opt/homebrew/bin/fish" {
		t.Fatalf("explicit shell: %q", got)
	}
	if got := loginShell("darwin", ""); got != "/bin/zsh" {
		t.Fatalf("darwin fallback: %q", got)
	}
	if got := loginShell("linux", "zsh"); got != "/bin/sh" {
		t.Fatalf("linux fallback: %q", got)
	}
}
