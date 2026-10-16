package main

import (
	"errors"
	"path/filepath"
	"testing"
)

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

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		panic(err)
	}
	return abs
}
