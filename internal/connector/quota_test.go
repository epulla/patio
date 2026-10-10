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
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestReadCodexQuotaOverPipes(t *testing.T) {
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()
	defer clientToServerR.Close()
	defer clientToServerW.Close()
	defer serverToClientR.Close()
	defer serverToClientW.Close()
	serverErr := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(clientToServerR)
		for _, expected := range []string{"initialize", "initialized", "account/rateLimits/read"} {
			line, err := reader.ReadString('\n')
			if err != nil {
				serverErr <- err
				return
			}
			var request struct {
				ID     *uint64 `json:"id"`
				Method string  `json:"method"`
			}
			if err := json.Unmarshal([]byte(line), &request); err != nil || request.Method != expected {
				serverErr <- io.ErrUnexpectedEOF
				return
			}
			if request.ID != nil {
				result := `{"id":0,"result":{}}`
				if expected == "account/rateLimits/read" {
					result = `{"id":1,"result":{"rateLimits":{"primary":{"usedPercent":25,"windowDurationMins":300,"resetsAt":1760000000},"secondary":{"usedPercent":10,"windowDurationMins":10080,"resetsAt":1760200000}}}}`
				}
				if _, err := io.WriteString(serverToClientW, result+"\n"); err != nil {
					serverErr <- err
					return
				}
			}
		}
		serverErr <- nil
	}()
	client := newClient(serverToClientR, clientToServerW, nil)
	if err := client.Initialize(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	windows, err := readCodexQuota(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 2 || windows[0].Label != "5h" || windows[0].UsedPercent == nil || *windows[0].UsedPercent != 25 || windows[0].ResetsAt == nil || *windows[0].ResetsAt != time.Unix(1760000000, 0).UTC().Format(time.RFC3339) || windows[1].Label != "weekly" {
		t.Fatalf("windows = %#v", windows)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestParseCodexQuotaNullableAndLabels(t *testing.T) {
	windows, err := parseCodexQuota([]byte(`{"rateLimits":{"primary":{"windowDurationMins":90},"secondary":{"windowDurationMins":120},"tertiary":{"windowDurationMins":3}}}`))
	if err != nil || len(windows) != 2 || windows[0].Label != "90m" || windows[0].UsedPercent != nil || windows[0].ResetsAt != nil || windows[1].Label != "2h" {
		t.Fatalf("windows = %#v, %v", windows, err)
	}
}

func TestParseOpenCodeStatsV1(t *testing.T) {
	const v1 = `┌────────────────────────────────────────────────────────┐
│                       OVERVIEW                         │
├────────────────────────────────────────────────────────┤
│Sessions                                          2,533 │
└────────────────────────────────────────────────────────┘
┌────────────────────────────────────────────────────────┐
│                    COST & TOKENS                       │
├────────────────────────────────────────────────────────┤
│Total Cost                                     $2884.52 │
│Avg Cost/Day                                     $18.49 │
│Avg Tokens/Session                                 1.7M │
│Median Tokens/Session                            341.1K │
│Input                                            118.9M │
│Output                                            27.3M │
│Cache Read                                      3877.6M │
└────────────────────────────────────────────────────────┘`
	got, err := parseOpenCodeStats(v1)
	if err != nil || got.Input != 118900000 || got.Output != 27300000 || got.Cost != 2884.52 {
		t.Fatalf("stats = %#v, %v", got, err)
	}
	if _, err := parseOpenCodeStats("unsupported stats format"); err == nil {
		t.Fatal("unsupported v1 output parsed without error")
	}
}

func TestReadOpencodeV2QuotaSkipsStats(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test executable script requires a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode")
	record := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(record) + "\nif [ \"$1\" = --version ]; then echo 'opencode v2.0.26'; fi\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	got := ReadQuota(context.Background(), Options{Kind: "opencode", Path: path})
	args, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if got.Note != "Usage not available for opencode v2." || got.Tokens != nil || got.UpdatedAt != nil {
		t.Fatalf("quota = %#v", got)
	}
	if string(args) != "--version\n" {
		t.Fatalf("CLI args = %q", args)
	}
}

func TestCopilotQuota(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		want        int
	}{
		{"documented gross quantity", `{"usageItems":[{"grossQuantity":2},{"grossQuantity":3}]}`, 5},
		{"legacy quantity", `{"usageItems":[{"quantity":4}]}`, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer pat-secret" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
					t.Errorf("headers = %#v", r.Header)
				}
				switch r.URL.Path {
				case "/user":
					_, _ = io.WriteString(w, `{"login":"octocat"}`)
				case "/users/octocat/settings/billing/premium_request/usage":
					if r.URL.Query().Get("year") == "" || r.URL.Query().Get("month") == "" {
						t.Errorf("current month query missing: %s", r.URL.RawQuery)
					}
					_, _ = io.WriteString(w, tc.usage)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			got, err := copilotQuota(context.Background(), Options{Secret: "pat-secret", GitHubBaseURL: server.URL, HTTPClient: server.Client()})
			if err != nil || got != tc.want {
				t.Fatalf("copilot quota = %d, %v", got, err)
			}
		})
	}
}

func TestReadQuotaNonPATCopilot(t *testing.T) {
	got := ReadQuota(context.Background(), Options{Kind: "copilot"})
	if got.Note != "unknown" || got.Requests != nil || !strings.Contains(got.Kind, "copilot") {
		t.Fatalf("quota = %#v", got)
	}
}
