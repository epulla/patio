package main

import (
	"path/filepath"
	"testing"
)

func TestDataDir(t *testing.T) {
	tests := []struct {
		name    string
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
			want: filepath.Join(`C:\Users\test\AppData\Local`, "my-tickets"),
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
			want: filepath.Join("/Users/test", "Library", "Application Support", "my-tickets"),
		},
		{
			name: "linux XDG absolute",
			goos: "linux",
			env:  map[string]string{"XDG_DATA_HOME": "/tmp/data"},
			home: "/home/test",
			want: filepath.Join("/tmp/data", "my-tickets"),
		},
		{
			name: "linux XDG relative",
			goos: "linux",
			env:  map[string]string{"XDG_DATA_HOME": "relative/data"},
			home: "/home/test",
			want: filepath.Join("/home/test", ".local", "share", "my-tickets"),
		},
		{
			name: "linux unset",
			goos: "linux",
			home: "/home/test",
			want: filepath.Join("/home/test", ".local", "share", "my-tickets"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := dataDir(test.goos, func(key string) string {
				return test.env[key]
			}, test.home)
			if test.wantErr {
				if err == nil {
					t.Fatal("dataDir() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("dataDir() error = %v", err)
			}
			if got != test.want {
				t.Errorf("dataDir() = %q, want %q", got, test.want)
			}
		})
	}
}
