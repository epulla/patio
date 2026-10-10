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

func TestParseOpenCodeStatsByVersion(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "opencode-v2-stats.json"))
	if err != nil {
		t.Fatal(err)
	}
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
	for _, tc := range []struct {
		name, raw, note string
		major           int
		input, output   int64
		cost            float64
	}{
		{"v1 recorded text", v1, "", 1, 118900000, 27300000, 2884.52},
		{"v2 recorded JSON", string(fixture), "", 2, 0, 0, 0},
		{"v2 missing total", `{"tokens":{"input":7},"cost":0.5}`, "OpenCode stats omitted output tokens", 2, 7, 0, 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got TokenTotals
			var note string
			var err error
			if tc.major >= 2 {
				got, note, err = parseOpenCodeStatsJSON(tc.raw)
			} else {
				got, err = parseOpenCodeStats(tc.raw)
			}
			if err != nil || note != tc.note || got.Input != tc.input || got.Output != tc.output || got.Cost != tc.cost {
				t.Fatalf("stats = %#v, %q, %v", got, note, err)
			}
		})
	}
	if _, err := parseOpenCodeStats("unsupported stats format"); err == nil {
		t.Fatal("unsupported v1 output parsed without error")
	}
	if _, _, err := parseOpenCodeStatsJSON("not json"); err == nil {
		t.Fatal("invalid v2 JSON parsed without error")
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
