package connector

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestInstallHint(t *testing.T) {
	tests := []struct {
		name, goos, kind, command, url string
	}{
		{"macOS Claude", "darwin", "claude", "curl -fsSL https://claude.ai/install.sh | bash", "https://code.claude.com/docs/en/setup"},
		{"Linux Codex", "linux", "codex", "curl -fsSL https://chatgpt.com/codex/install.sh | sh", "https://developers.openai.com/codex/cli"},
		{"macOS OpenCode", "darwin", "opencode", "curl -fsSL https://opencode.ai/install | bash", "https://opencode.ai/docs/"},
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
