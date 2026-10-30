package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zalando/go-keyring"
	connectorcli "patio/internal/connector"
	"patio/internal/proc"
)

var titleCommandRunner = func(ctx context.Context, kind, path string, args, env []string) ([]byte, error) {
	if kind == "opencode" {
		return connectorcli.OpencodeCommandCombinedOutput(ctx, path, args, env)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	proc.HideConsole(cmd)
	return cmd.CombinedOutput()
}
var titleCommandFinder = connectorcli.Find

var titleTextGenerator func(context.Context, *chatManager, string, string, string) (string, error)

func init() {
	titleTextGenerator = func(ctx context.Context, m *chatManager, id, model, prompt string) (string, error) {
		return m.generateText(ctx, id, model, prompt)
	}
}

func (m *chatManager) generateText(ctx context.Context, connectorID, model, prompt string) (string, error) {
	var kind, baseURL, configDir sql.NullString
	if err := m.db.QueryRow(`SELECT kind,base_url,config_dir FROM connector WHERE id=?`, connectorID).Scan(&kind, &baseURL, &configDir); err != nil {
		return "", err
	}
	if kind.String == "local" {
		if model == "" {
			_ = m.db.QueryRow(`SELECT model FROM connector WHERE id=?`, connectorID).Scan(&model)
		}
		return generateLocalText(ctx, connectorID, model, prompt, baseURL.String)
	}
	cli, err := titleCommandFinder(kind.String)
	if err != nil {
		return "", err
	}
	args := []string{}
	switch kind.String {
	case "claude":
		args = []string{"-p", prompt}
		if model != "" {
			args = append(args, "--model", model)
		}
	case "codex":
		args = []string{"exec", "--skip-git-repo-check"}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt)
	case "opencode":
		args = []string{"run", prompt}
		if model != "" {
			args = append(args, "--model", model)
		}
	case "copilot":
		args = []string{"-p", prompt}
		if model != "" {
			args = append(args, "--model", model)
		}
	default:
		return "", fmt.Errorf("unsupported connector kind %q", kind.String)
	}
	secret := ""
	if kind.String == "copilot" {
		secret, err = keyring.Get(connectorService, connectorSecret(connectorID))
		if errors.Is(err, keyring.ErrNotFound) {
			err = nil
		}
		if err != nil {
			return "", err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := titleCommandRunner(ctx, kind.String, cli, args, connectorcli.Env(kind.String, configDir.String, secret))
	return strings.TrimSpace(string(out)), err
}

func generateLocalText(ctx context.Context, id, model, prompt, endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || !validConnectorURL(endpoint) {
		return "", errors.New("invalid local model endpoint")
	}
	secret, err := keyring.Get(connectorService, connectorSecret(id))
	if errors.Is(err, keyring.ErrNotFound) {
		err = nil
	}
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "stream": false})
	endpointURL := strings.TrimRight(u.String(), "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("local model returned %s", resp.Status)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 {
		return "", errors.New("local model returned no choices")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

func cleanChatTitle(raw string) string {
	title := strings.TrimSpace(strings.SplitN(raw, "\n", 2)[0])
	title = strings.Trim(title, " \t\r\n\"'`*_#")
	title = strings.Join(strings.Fields(title), " ")
	title = strings.TrimRight(title, ".,!?:;，。！？；：")
	words := strings.Fields(title)
	if len(words) > 6 {
		title = strings.Join(words[:6], " ")
	}
	if len(title) > 60 {
		for !utf8.ValidString(title[:60]) {
			title = title[:59]
		}
		title = strings.TrimSpace(title[:60])
		title = strings.TrimRight(title, " \t")
	}
	return title
}

func (m *chatManager) maybeGenerateChatTitle(chatID, reply string) {
	var locked bool
	var connectorID sql.NullString
	var chatModel string
	if err := m.db.QueryRow(`SELECT title_locked,connector_id,model FROM chat WHERE id=?`, chatID).Scan(&locked, &connectorID, &chatModel); err != nil || locked {
		if err != nil {
			log.Printf("read title settings for chat %s: %v", chatID, err)
		}
		return
	}
	settings := map[string]string{}
	rows, err := m.db.Query(`SELECT key,value FROM app_setting WHERE key IN ('ai_titles','title_connector_id','title_model')`)
	if err != nil {
		log.Printf("read title settings for chat %s: %v", chatID, err)
		return
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			_ = rows.Close()
			log.Printf("read title settings for chat %s: %v", chatID, err)
			return
		}
		settings[k] = v
	}
	if err := rows.Close(); err != nil {
		log.Printf("read title settings for chat %s: %v", chatID, err)
		return
	}
	if raw, ok := settings["ai_titles"]; ok {
		var enabled bool
		if json.Unmarshal([]byte(raw), &enabled) != nil || !enabled {
			return
		}
	}
	var user string
	if err := m.db.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND role='user' ORDER BY created_at,rowid LIMIT 1`, chatID).Scan(&user); err != nil {
		log.Printf("read first prompt for chat %s: %v", chatID, err)
		return
	}
	user = limitTitleInput(user)
	reply = limitTitleInput(reply)
	chosenID := connectorID.String
	if raw := settings["title_connector_id"]; raw != "" {
		var configured *string
		if json.Unmarshal([]byte(raw), &configured) == nil && configured != nil {
			var exists int
			if err := m.db.QueryRow(`SELECT 1 FROM connector WHERE id=?`, *configured).Scan(&exists); err == nil {
				chosenID = *configured
			}
		}
	}
	// The chat's model only fits the chat's own connector; another connector uses its default.
	model := ""
	if raw := settings["title_model"]; raw != "" {
		_ = json.Unmarshal([]byte(raw), &model)
	}
	if model == "" {
		var kind string
		_ = m.db.QueryRow(`SELECT kind FROM connector WHERE id=?`, chosenID).Scan(&kind)
		if kind == "claude" {
			model = "haiku"
		} else if chosenID == connectorID.String {
			model = chatModel
		}
	}
	prompt := "Create a concise title of 3–6 words for this chat. Output only the title, without quotes or punctuation.\n\nUser: " + user + "\n\nAssistant: " + reply
	m.background.Add(1)
	go func() {
		defer m.background.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		generated, err := titleTextGenerator(ctx, m, chosenID, model, prompt)
		if err != nil {
			log.Printf("generate title for chat %s: %v", chatID, err)
			return
		}
		title := cleanChatTitle(generated)
		if title == "" {
			return
		}
		if _, err := m.db.Exec(`UPDATE chat SET title=?,updated_at=? WHERE id=? AND title_locked=0`, title, now(), chatID); err != nil {
			log.Printf("set title for chat %s: %v", chatID, err)
		}
	}()
}

func limitTitleInput(value string) string {
	runes := []rune(value)
	if len(runes) > 2000 {
		return string(runes[:2000])
	}
	return value
}
