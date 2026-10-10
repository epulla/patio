package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Quota struct {
	Kind      string       `json:"kind"`
	Windows   []Window     `json:"windows"`
	Requests  *int         `json:"requests"`
	Tokens    *TokenTotals `json:"tokens"`
	Note      string       `json:"note"`
	UpdatedAt *string      `json:"updated_at"`
}

type Window struct {
	Label       string   `json:"label"`
	UsedPercent *float64 `json:"used_percent"`
	ResetsAt    *string  `json:"resets_at"`
}

type TokenTotals struct {
	Input  int64   `json:"input"`
	Output int64   `json:"output"`
	Cost   float64 `json:"cost"`
}

func ReadQuota(ctx context.Context, opts Options) Quota {
	out := Quota{Kind: opts.Kind, Windows: []Window{}}
	switch opts.Kind {
	case "codex":
		if opts.Path == "" {
			var err error
			opts.Path, err = find("codex")
			if err != nil {
				out.Note = err.Error()
				return out
			}
		}
		client, err := StartCodex(ctx, opts.Path, Env(opts.Kind, opts.ConfigDir, opts.Secret), opts.ConfigDir != "", opts.Version)
		if err != nil {
			out.Note = err.Error()
			return out
		}
		defer client.Close()
		windows, err := readCodexQuota(ctx, client)
		if err != nil {
			out.Note = "parse account/rateLimits/read: " + err.Error()
			return out
		}
		out.Windows = windows
		setQuotaUpdated(&out)
	case "opencode":
		if opts.Path == "" {
			var err error
			opts.Path, err = find("opencode")
			if err != nil {
				out.Note = err.Error()
				return out
			}
		}
		env := Env(opts.Kind, opts.ConfigDir, opts.Secret)
		major, err := opencodeMajor(ctx, opts.Path, env)
		if err != nil {
			out.Note = err.Error()
			return out
		}
		if major >= 2 {
			out.Note = "Usage not available for opencode v2."
			return out
		}
		text, err := OpencodeCommandOutput(ctx, opts.Path, []string{"stats"}, env)
		if err != nil {
			out.Note = err.Error()
			return out
		}
		totals, err := parseOpenCodeStats(text)
		if err != nil {
			out.Note = err.Error()
			return out
		}
		out.Tokens = &totals
		setQuotaUpdated(&out)
	case "copilot":
		if opts.Secret == "" {
			out.Note = "unknown"
			return out
		}
		requests, err := copilotQuota(ctx, opts)
		if err != nil {
			out.Note = strings.ReplaceAll(err.Error(), opts.Secret, "[redacted]")
			return out
		}
		out.Requests = &requests
		setQuotaUpdated(&out)
	case "claude":
		out.Note = "Unknown until the first chat"
	case "local":
		out.Note = "No quota for local models"
	default:
		out.Note = "unknown connector kind"
	}
	return out
}

func readCodexQuota(ctx context.Context, client *Client) ([]Window, error) {
	var raw json.RawMessage
	if err := client.Call(ctx, "account/rateLimits/read", map[string]any{}, &raw); err != nil {
		return nil, err
	}
	return parseCodexQuota(raw)
}

func setQuotaUpdated(out *Quota) {
	updated := time.Now().UTC().Format(time.RFC3339)
	out.UpdatedAt = &updated
}

func parseCodexQuota(raw []byte) ([]Window, error) {
	var response struct {
		RateLimits *struct {
			Primary   *codexRateWindow `json:"primary"`
			Secondary *codexRateWindow `json:"secondary"`
		} `json:"rateLimits"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	if response.RateLimits == nil {
		return nil, errors.New("missing rateLimits")
	}
	windows := make([]Window, 0, 2)
	for _, rate := range []*codexRateWindow{response.RateLimits.Primary, response.RateLimits.Secondary} {
		if rate == nil {
			continue
		}
		window := Window{Label: codexWindowLabel(rate.WindowDurationMins), UsedPercent: rate.UsedPercent}
		if rate.ResetsAt != nil {
			reset := time.Unix(*rate.ResetsAt, 0).UTC().Format(time.RFC3339)
			window.ResetsAt = &reset
		}
		windows = append(windows, window)
	}
	return windows, nil
}

func ParseCodexQuota(raw []byte) ([]Window, error) { return parseCodexQuota(raw) }

type codexRateWindow struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins int      `json:"windowDurationMins"`
	ResetsAt           *int64   `json:"resetsAt"`
}

func codexWindowLabel(minutes int) string {
	switch minutes {
	case 300:
		return "5h"
	case 10080:
		return "weekly"
	default:
		if minutes%60 == 0 {
			return strconv.Itoa(minutes/60) + "h"
		}
		return strconv.Itoa(minutes) + "m"
	}
}

var statsValue = regexp.MustCompile(`^\s*│\s*(Total Cost|Input|Output)\s+([^│]+?)\s*│\s*$`)
var statsAmount = regexp.MustCompile(`^\s*\$?([0-9][0-9,]*(?:\.[0-9]+)?)\s*([KkMmBb])?\s*$`)

func parseOpenCodeStats(text string) (TokenTotals, error) {
	values := map[string]float64{}
	for _, line := range strings.Split(text, "\n") {
		match := statsValue.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		amount := statsAmount.FindStringSubmatch(match[2])
		if len(amount) != 3 {
			continue
		}
		number, err := strconv.ParseFloat(strings.ReplaceAll(amount[1], ",", ""), 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(amount[2]) {
		case "k":
			number *= 1e3
		case "m":
			number *= 1e6
		case "b":
			number *= 1e9
		}
		values[match[1]] = number
	}
	input, hasInput := values["Input"]
	output, hasOutput := values["Output"]
	cost, hasCost := values["Total Cost"]
	if !hasInput || !hasOutput || !hasCost || input > math.MaxInt64 || output > math.MaxInt64 {
		return TokenTotals{}, errors.New("could not parse opencode stats totals")
	}
	return TokenTotals{Input: int64(math.Round(input)), Output: int64(math.Round(output)), Cost: cost}, nil
}

func copilotQuota(ctx context.Context, opts Options) (int, error) {
	base := strings.TrimRight(opts.GitHubBaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	user, err := githubGet(ctx, &clientCopy, base+"/user", opts.Secret)
	if err != nil {
		return 0, err
	}
	var account struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(user, &account); err != nil {
		return 0, err
	}
	if account.Login == "" {
		return 0, errors.New("GitHub /user returned empty login")
	}
	now := time.Now().UTC()
	endpoint := base + "/users/" + url.PathEscape(account.Login) + "/settings/billing/premium_request/usage?year=" + strconv.Itoa(now.Year()) + "&month=" + strconv.Itoa(int(now.Month()))
	body, err := githubGet(ctx, &clientCopy, endpoint, opts.Secret)
	if err != nil {
		return 0, err
	}
	var report struct {
		UsageItems []struct {
			GrossQuantity *float64 `json:"grossQuantity"`
			NetQuantity   *float64 `json:"netQuantity"`
			Quantity      *float64 `json:"quantity"`
		} `json:"usageItems"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		return 0, err
	}
	var total float64
	for _, item := range report.UsageItems {
		quantity := item.GrossQuantity
		if quantity == nil {
			quantity = item.NetQuantity
		}
		if quantity == nil {
			quantity = item.Quantity
		}
		if quantity != nil {
			total += *quantity
		}
	}
	if total > math.MaxInt || total < 0 {
		return 0, errors.New("invalid premium request quantity")
	}
	return int(math.Round(total)), nil
}

func githubGet(ctx context.Context, client *http.Client, endpoint, secret string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return body, nil
}
