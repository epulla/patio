package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestThrottleCoalescesBurst(t *testing.T) {
	var calls atomic.Int32
	throttled := throttle(50*time.Millisecond, func() { calls.Add(1) })
	throttled()
	throttled()
	throttled()
	if got := calls.Load(); got != 1 {
		t.Fatalf("immediate calls = %d, want 1", got)
	}
	deadline := time.Now().Add(time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls after trailing edge = %d, want 2", got)
	}
}

func TestDBPath(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		goos    string
		env     map[string]string
		home    string
		want    string
		wantErr bool
	}{
		{
			name: "windows set",
			goos: "windows",
			env:  map[string]string{"LOCALAPPDATA": `C:\Users\test\AppData\Local`},
			want: filepath.Join(`C:\Users\test\AppData\Local`, "patio", "patio.db"),
		},
		{
			name:    "windows unset",
			goos:    "windows",
			wantErr: true,
		},
		{
			name: "darwin",
			goos: "darwin",
			home: "/Users/test",
			want: filepath.Join("/Users/test", "Library", "Application Support", "patio", "patio.db"),
		},
		{
			name: "linux XDG absolute",
			goos: "linux",
			env:  map[string]string{"XDG_DATA_HOME": "/tmp/data"},
			home: "/home/test",
			want: filepath.Join("/tmp/data", "patio", "patio.db"),
		},
		{
			name: "linux XDG relative",
			goos: "linux",
			env:  map[string]string{"XDG_DATA_HOME": "relative/data"},
			home: "/home/test",
			want: filepath.Join("/home/test", ".local", "share", "patio", "patio.db"),
		},
		{
			name: "linux unset",
			goos: "linux",
			home: "/home/test",
			want: filepath.Join("/home/test", ".local", "share", "patio", "patio.db"),
		},
		{
			name: "absolute flag",
			flag: "/tmp/custom.db",
			goos: "windows",
			want: mustAbs("/tmp/custom.db"),
		},
		{
			name: "relative flag",
			flag: "custom.db",
			want: mustAbs("custom.db"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := dbPath(test.flag, test.goos, func(key string) string {
				return test.env[key]
			}, test.home)
			if test.wantErr {
				if err == nil {
					t.Fatal("dbPath() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("dbPath() error = %v", err)
			}
			if got != test.want {
				t.Errorf("dbPath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestIsDefaultDB(t *testing.T) {
	getenv := func(string) string { return "" }
	home := "/Users/e"
	for path, want := range map[string]bool{
		"/Users/e/Library/Application Support/patio/patio.db":          true,
		"/Users/e/Library/Application Support/patio/../patio/patio.db": true,
		"/tmp/patio-test/patio.db":                                     false,
	} {
		if got := isDefaultDB(path, "darwin", getenv, home); got != want {
			t.Errorf("isDefaultDB(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestLogPath(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		env     map[string]string
		home    string
		want    string
		wantErr bool
	}{
		{name: "windows", goos: "windows", env: map[string]string{"LOCALAPPDATA": `C:\Users\test\AppData\Local`}, want: filepath.Join(`C:\Users\test\AppData\Local`, "patio", "logs", "patio.log")},
		{name: "windows unset", goos: "windows", wantErr: true},
		{name: "darwin", goos: "darwin", home: "/Users/test", want: filepath.Join("/Users/test", "Library", "Logs", "patio", "patio.log")},
		{name: "linux XDG absolute", goos: "linux", env: map[string]string{"XDG_STATE_HOME": "/tmp/state"}, home: "/home/test", want: filepath.Join("/tmp/state", "patio", "patio.log")},
		{name: "linux XDG relative", goos: "linux", env: map[string]string{"XDG_STATE_HOME": "relative"}, home: "/home/test", want: filepath.Join("/home/test", ".local", "state", "patio", "patio.log")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := logPath(test.goos, func(key string) string { return test.env[key] }, test.home)
			if test.wantErr {
				if err == nil {
					t.Fatal("logPath() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Errorf("logPath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRotateLog(t *testing.T) {
	for _, over := range []bool{true, false} {
		t.Run(map[bool]string{true: "over limit", false: "under limit"}[over], func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "patio.log")
			size := 32
			if over {
				size = 5*1024*1024 + 1
			}
			if err := os.WriteFile(file, []byte(strings.Repeat("x", size)), 0o600); err != nil {
				t.Fatal(err)
			}
			for i, content := range []string{"one", "two", "three"} {
				if err := os.WriteFile(file+"."+string(rune('1'+i)), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := rotateLog(file); err != nil {
				t.Fatal(err)
			}
			if !over {
				got, err := os.ReadFile(file + ".1")
				if err != nil || string(got) != "one" {
					t.Fatalf("under-limit backup = %q, %v", got, err)
				}
				return
			}
			for suffix, want := range map[string]string{".1": strings.Repeat("x", size), ".2": "one", ".3": "two"} {
				got, err := os.ReadFile(file + suffix)
				if err != nil || string(got) != want {
					t.Errorf("%s = %q, %v; want %q", suffix, got, err, want)
				}
			}
		})
	}
}

func TestNotifierFallsBackToBeeep(t *testing.T) {
	tests := []struct {
		name     string
		ready    bool
		sendErr  error
		wantSent string
		wantBeep bool
	}{
		{name: "ready", ready: true, wantSent: "/calendar"},
		{name: "not ready", wantBeep: true},
		{name: "send fails", ready: true, sendErr: errors.New("denied"), wantSent: "/calendar", wantBeep: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sent, beeped := "", false
			n := &notifier{
				send:     func(_, _, link string) error { sent = link; return test.sendErr },
				fallback: func(string, string) error { beeped = true; return nil },
			}
			n.ready.Store(test.ready)
			if err := n.Notify("Event", "soon", "/calendar"); err != nil {
				t.Fatal(err)
			}
			if sent != test.wantSent || beeped != test.wantBeep {
				t.Fatalf("sent=%q beeped=%v, want %q %v", sent, beeped, test.wantSent, test.wantBeep)
			}
		})
	}
}

func TestNotifierBlocked(t *testing.T) {
	tests := []struct {
		name        string
		asked       bool
		allowed     bool
		wantBlocked bool
		wantReady   bool
	}{
		{name: "not asked yet"},
		{name: "allowed later", asked: true, allowed: true, wantReady: true},
		{name: "denied", asked: true, wantBlocked: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			n := &notifier{check: func() (bool, error) { return test.allowed, nil }}
			n.asked.Store(test.asked)
			if got := n.Blocked(); got != test.wantBlocked || n.ready.Load() != test.wantReady {
				t.Fatalf("blocked=%v ready=%v, want %v %v", got, n.ready.Load(), test.wantBlocked, test.wantReady)
			}
		})
	}
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		panic(err)
	}
	return abs
}

func TestOpenPathArg(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"patio"}, ""},
		{[]string{"patio", "-d", "x.db", "-open", "/boards/b"}, "/boards/b"},
		{[]string{"patio", "-unknown", "-open", "/boards/b"}, ""},
		{nil, ""},
	} {
		if got := openPathArg(tc.args); got != tc.want {
			t.Errorf("openPathArg(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
