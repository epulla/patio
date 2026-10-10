package connector

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseClaude(t *testing.T) {
	for _, tc := range []struct {
		name, raw, account, plan string
		logged                   *bool
	}{
		{"logged in", `{"loggedIn":true,"email":"user@example.com","subscriptionType":"max"}`, "user@example.com", "max", boolPointer(true)},
		{"logged out", `{"loggedIn":false}`, "", "", boolPointer(false)},
		{"missing fields tolerated", `{}`, "", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logged, account, plan, err := parseClaude(tc.raw)
			if err != nil || !sameBool(logged, tc.logged) || account != tc.account || plan != tc.plan {
				t.Fatalf("ParseClaude = %v, %q, %q, %v", logged, account, plan, err)
			}
		})
	}
}

func TestCopilotEnvIncludesStoredSecretWithoutConfigDir(t *testing.T) {
	for _, entry := range Env("copilot", "", "saved-pat") {
		if entry == "COPILOT_GITHUB_TOKEN=saved-pat" {
			return
		}
	}
	t.Fatal("Copilot environment omitted stored token")
}

func TestParseOpenCodeAuth(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      bool
	}{
		{"recorded credentials", "\x1b[0m\n┌  Credentials  ~/.local/share/opencode/auth.json\n│\n●  OpenCode Go api\n●  OpenAI oauth\n●  Anthropic oauth\n└  3 credentials\n", true},
		{"empty", "┌ Credentials\n└ 0 credentials\n", false},
		{"unstructured listing", "No credentials found", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseOpenCodeAuth(tc.raw); got != tc.want {
				t.Fatalf("parseOpenCodeAuth = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseOpenCodeAuthJSON(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "opencode-v2-auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, raw, account string
		logged             bool
		wantErr            bool
	}{
		{"v2 logged in fixture", string(fixture), "OpenAI", true, false},
		{"v2 logged out", `[]`, "", false, false},
		{"v2 non-empty list", `[{"provider":"openai"}]`, "openai", true, false},
		{"v2 account field", `[{"email":"user@example.com"}]`, "user@example.com", true, false},
		{"invalid JSON", "not json", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logged, account, err := parseOpenCodeAuthJSON(tc.raw)
			if (err != nil) != tc.wantErr || logged != tc.logged || account != tc.account {
				t.Fatalf("auth = %v, %q, %v", logged, account, err)
			}
		})
	}
}

func TestParseOpencodeMajor(t *testing.T) {
	for _, tc := range []struct {
		version string
		major   int
		wantErr bool
	}{
		{"1.18.35", 1, false},
		{"v1.18.35", 1, false},
		{"opencode v2.0.26", 2, false},
		{"2.0.26", 2, false},
		{"unknown", 0, true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			got, err := parseOpencodeMajor(tc.version)
			if (err != nil) != tc.wantErr || got != tc.major {
				t.Fatalf("major = %d, %v", got, err)
			}
		})
	}
	if runtime.GOOS == "windows" {
		t.Skip("test executable script requires a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' 'opencode v2.0.26'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := OpencodeMajor(context.Background(), path, []string{}); err != nil || got != 2 {
		t.Fatalf("OpencodeMajor = %d, %v", got, err)
	}
}

func TestOpencodeCommandsSerialize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test executable script requires a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode")
	lock := filepath.Join(dir, "running")
	overlap := filepath.Join(dir, "overlap")
	script := "#!/bin/sh\nif ! mkdir \"$LOCK_DIR\" 2>/dev/null; then touch \"$OVERLAP_FILE\"; fi\nsleep 0.2\nrmdir \"$LOCK_DIR\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	env := replaceEnv(os.Environ(), []string{"LOCK_DIR=" + lock, "OVERLAP_FILE=" + overlap})
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := OpencodeCommandOutput(context.Background(), path, []string{"models"}, env)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(overlap); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("commands overlapped: %v", err)
	}
}

func TestOpencodeCommandWaitHonorsContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test executable script requires a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode")
	started := filepath.Join(dir, "started")
	script := "#!/bin/sh\ntouch \"$STARTED_FILE\"\nsleep 0.3\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	env := replaceEnv(os.Environ(), []string{"STARTED_FILE=" + started})
	firstDone := make(chan error, 1)
	go func() {
		_, err := OpencodeCommandOutput(context.Background(), path, []string{"models"}, env)
		firstDone <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first command did not start")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := OpencodeCommandOutput(ctx, path, []string{"models"}, env)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting command error = %v", err)
	}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestCommandOutputSendsTermOnCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal behavior")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode")
	marker := filepath.Join(dir, "term")
	started := filepath.Join(dir, "started")
	script := "#!/bin/sh\ntrap 'printf term > \"$TERM_FILE\"; exit 0' TERM\ntouch \"$STARTED_FILE\"\nwhile :; do sleep 0.05; done\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := commandOutput(ctx, path, nil, replaceEnv(os.Environ(), []string{"TERM_FILE=" + marker, "STARTED_FILE=" + started}))
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command did not start")
		}
		time.Sleep(time.Millisecond)
	}
	err := <-done
	if err == nil {
		t.Fatal("command unexpectedly succeeded")
	}
	got, readErr := os.ReadFile(marker)
	if readErr != nil || string(got) != "term" {
		t.Fatalf("TERM marker = %q, %v", got, readErr)
	}
}

func TestInstallHint(t *testing.T) {
	tests := []struct {
		name, goos, kind, command, url string
	}{
		{"macOS Claude", "darwin", "claude", "curl -fsSL https://claude.ai/install.sh | bash", "https://code.claude.com/docs/en/setup"},
		{"Linux Codex", "linux", "codex", "curl -fsSL https://chatgpt.com/codex/install.sh | sh", "https://developers.openai.com/codex/cli"},
		{"macOS OpenCode", "darwin", "opencode", "curl -fsSL https://opencode.ai/v2/install | bash", "https://opencode.ai/docs/"},
		{"Linux Copilot", "linux", "copilot", "curl -fsSL https://gh.io/copilot-install | bash", "https://docs.github.com/en/copilot/how-tos/set-up/install-copilot-cli"},
		{"Windows Claude", "windows", "claude", "irm https://claude.ai/install.ps1 | iex", "https://code.claude.com/docs/en/setup"},
		{"Windows Codex", "windows", "codex", "irm https://chatgpt.com/codex/install.ps1 | iex", "https://developers.openai.com/codex/cli"},
		{"Windows Copilot", "windows", "copilot", "winget install GitHub.Copilot", "https://docs.github.com/en/copilot/how-tos/set-up/install-copilot-cli"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := installHint(tc.kind, tc.goos)
			if got == nil || got.Command != tc.command || got.URL != tc.url {
				t.Fatalf("install hint = %#v", got)
			}
		})
	}
	if got := installHint("opencode", "windows"); got != nil {
		t.Fatalf("Windows OpenCode hint = %#v", got)
	}
}

func TestCodexAppServerAccountRead(t *testing.T) {
	for _, tc := range []struct {
		name, response, account, plan string
		logged                        bool
	}{
		{"logged in", `{"account":{"type":"chatgpt","email":"user@example.com","planType":"plus"},"requiresOpenaiAuth":true}`, "user@example.com", "plus", true},
		{"logged out", `{"account":null,"requiresOpenaiAuth":true}`, "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clientToServerR, clientToServerW := io.Pipe()
			serverToClientR, serverToClientW := io.Pipe()
			defer clientToServerR.Close()
			defer clientToServerW.Close()
			defer serverToClientR.Close()
			defer serverToClientW.Close()
			serverErr := make(chan error, 1)
			go func() {
				reader := bufio.NewReader(clientToServerR)
				first, err := reader.ReadString('\n')
				if err != nil {
					serverErr <- err
					return
				}
				var initialize map[string]any
				if err := json.Unmarshal([]byte(first), &initialize); err != nil {
					serverErr <- err
					return
				}
				if initialize["method"] != "initialize" || initialize["id"] != float64(0) || initialize["jsonrpc"] != nil {
					serverErr <- io.ErrUnexpectedEOF
					return
				}
				if _, err := io.WriteString(serverToClientW, `{"id":0,"result":{}}`+"\n"); err != nil {
					serverErr <- err
					return
				}
				initialized, err := reader.ReadString('\n')
				if err != nil || !strings.Contains(initialized, `"method":"initialized"`) {
					serverErr <- io.ErrUnexpectedEOF
					return
				}
				request, err := reader.ReadString('\n')
				if err != nil {
					serverErr <- err
					return
				}
				var call map[string]any
				if err := json.Unmarshal([]byte(request), &call); err != nil || call["method"] != "account/read" {
					serverErr <- io.ErrUnexpectedEOF
					return
				}
				if _, err := io.WriteString(serverToClientW, `{"method":"account/login/completed","params":{"success":true}}`+"\n"+`{"id":1,"result":`+tc.response+"}\n"); err != nil {
					serverErr <- err
					return
				}
				serverErr <- nil
			}()

			client := newClient(serverToClientR, clientToServerW, nil)
			if err := client.Initialize(context.Background(), "test-version"); err != nil {
				t.Fatal(err)
			}
			var raw json.RawMessage
			if err := client.Call(context.Background(), "account/read", map[string]any{}, &raw); err != nil {
				t.Fatal(err)
			}
			logged, account, plan, err := parseCodexAccount(raw)
			if err != nil || logged == nil || *logged != tc.logged || account != tc.account || plan != tc.plan {
				t.Fatalf("account = %v, %q, %q, %v", logged, account, plan, err)
			}
			select {
			case notification := <-client.Notifications():
				if notification.Method != "account/login/completed" {
					t.Fatalf("notification method = %q", notification.Method)
				}
			default:
				t.Fatal("expected account/login/completed notification")
			}
			if err := <-serverErr; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCodexRPCMatchesIDsAndSurfacesErrors(t *testing.T) {
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()
	client := newClient(serverToClientR, clientToServerW, nil)
	defer clientToServerR.Close()
	defer serverToClientR.Close()
	defer serverToClientW.Close()
	serverErr := make(chan error, 1)
	go func() {
		request, err := bufio.NewReader(clientToServerR).ReadString('\n')
		if err != nil {
			serverErr <- err
			return
		}
		var message map[string]any
		if err := json.Unmarshal([]byte(request), &message); err != nil {
			serverErr <- err
			return
		}
		if message["id"] != float64(0) {
			serverErr <- io.ErrUnexpectedEOF
			return
		}
		_, err = io.WriteString(serverToClientW, `{"id":99,"result":{}}`+"\n"+`{"id":0,"error":{"code":-32000,"message":"denied"}}`+"\n")
		serverErr <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Call(ctx, "account/read", map[string]any{}, nil); err == nil || !strings.Contains(err.Error(), "-32000: denied") {
		t.Fatalf("RPC error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	_ = serverToClientW.Close()
	select {
	case _, open := <-client.Notifications():
		if open {
			t.Fatal("unexpected notification")
		}
	case <-ctx.Done():
		t.Fatal("notification stream did not close after server exit")
	}
}

func TestCodexModelListOverFakeAppServerPipes(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "codex-model-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()
	defer clientToServerR.Close()
	defer clientToServerW.Close()
	defer serverToClientR.Close()
	defer serverToClientW.Close()
	serverErr := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(clientToServerR)
		for _, method := range []string{"initialize", "initialized", "model/list"} {
			request, err := reader.ReadString('\n')
			if err != nil {
				serverErr <- err
				return
			}
			if method == "initialized" {
				if !strings.Contains(request, `"method":"initialized"`) {
					serverErr <- io.ErrUnexpectedEOF
					return
				}
				continue
			}
			var message map[string]any
			wantID := float64(0)
			if method == "model/list" {
				wantID = 1
			}
			if err := json.Unmarshal([]byte(request), &message); err != nil || message["method"] != method || message["id"] != wantID {
				serverErr <- io.ErrUnexpectedEOF
				return
			}
			if method == "initialize" {
				_, err = io.WriteString(serverToClientW, `{"id":0,"result":{}}`+"\n")
			} else {
				_, err = io.WriteString(serverToClientW, `{"id":1,"result":`+strings.TrimSpace(string(fixture))+"}\n")
			}
			if err != nil {
				serverErr <- err
				return
			}
		}
		serverErr <- nil
	}()
	client := newClient(serverToClientR, clientToServerW, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Initialize(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	models, err := client.Models(ctx)
	if err != nil || len(models) != 2 || models[0].ID != "gpt-5-codex" || models[0].Label != "GPT-5 Codex" || models[1].ID != "gpt-4.1" {
		t.Fatalf("models = %#v, %v", models, err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestCodexNotificationsDoNotBlockReader(t *testing.T) {
	reader, writer := io.Pipe()
	client := newClient(reader, io.Discard, nil)
	for range 32 {
		if _, err := io.WriteString(writer, `{"method":"event"}`+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, open := <-client.Notifications():
		if !open {
			t.Fatal("notification stream closed without buffering events")
		}
	case <-time.After(time.Second):
		t.Fatal("reader blocked on notification buffer")
	}
}

func TestCopilotStatusGitHubUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" || r.Header.Get("Authorization") != "Bearer pat-secret" {
			t.Errorf("request path/auth = %q, %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"login":"octocat"}`)
	}))
	defer server.Close()
	out := Status{}
	err := copilotStatus(context.Background(), &out, Options{Secret: "pat-secret", GitHubBaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil || out.LoggedIn == nil || !*out.LoggedIn || out.Account != "octocat" || out.Plan != "unknown" {
		t.Fatalf("Copilot status = %#v, %v", out, err)
	}
	encoded, err := json.Marshal(out)
	if err != nil || strings.Contains(string(encoded), "pat-secret") {
		t.Fatalf("serialized status leaked secret: %s, %v", encoded, err)
	}
}

func TestCopilotStatusDoesNotFollowRedirect(t *testing.T) {
	redirected := make(chan struct{}, 1)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected <- struct{}{}
		_, _ = io.WriteString(w, `{"login":"octocat"}`)
	}))
	defer destination.Close()
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/user", http.StatusFound)
	}))
	defer base.Close()
	var out Status
	err := copilotStatus(context.Background(), &out, Options{Secret: "pat-secret", GitHubBaseURL: base.URL, HTTPClient: base.Client()})
	if err == nil {
		t.Fatal("redirect was accepted")
	}
	select {
	case <-redirected:
		t.Fatal("authorization token was redirected to another URL")
	default:
	}
}

func TestRedactStatus(t *testing.T) {
	out := Status{Version: "pat-secret", Account: "pat-secret-user", Plan: "unknown", Note: "error for pat-secret"}
	redactStatus(&out, "pat-secret")
	encoded, err := json.Marshal(out)
	if err != nil || strings.Contains(string(encoded), "pat-secret") {
		t.Fatalf("serialized status leaked secret: %s, %v", encoded, err)
	}
}

func TestCheckReturnsCLIErrorAsNote(t *testing.T) {
	status := Check(context.Background(), Options{Kind: "claude", Path: filepath.Join(t.TempDir(), "missing")})
	if status.Installed == nil || !*status.Installed || status.Note == "" {
		t.Fatalf("CLI failure status = %#v", status)
	}
}

func boolPointer(value bool) *bool { return &value }

func sameBool(a, b *bool) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
