package connector

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildLoginCommandQuotesAndFiltersSecrets(t *testing.T) {
	path, config := filepath.Join("/tmp", "cli with space", "claude"), filepath.Join("/tmp", "config with 'quote'")
	command, err := BuildLoginCommand("darwin", "claude", path, []string{
		"CLAUDE_CONFIG_DIR=" + config, "COPILOT_GITHUB_TOKEN=hidden", "OTHER_SECRET=hidden",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(command, `export CLAUDE_CONFIG_DIR='/tmp/config with '"'"'quote'"'"''`) || !strings.Contains(command, `'/tmp/cli with space/claude' auth login`) {
		t.Fatalf("login command = %q", command)
	}
	if strings.Contains(command, "hidden") || strings.Contains(command, "SECRET") {
		t.Fatalf("login command includes secret: %q", command)
	}
	name, args, err := TerminalInvocation("darwin", `echo \ "`)
	if err != nil || name != "osascript" || len(args) != 4 || !strings.Contains(args[1], `\\`) || !strings.Contains(args[1], `\"`) {
		t.Fatalf("osascript invocation = %q %#v, %v", name, args, err)
	}
}

func TestBuildLoginCommandWindows(t *testing.T) {
	command, err := BuildLoginCommand("windows", "copilot", `C:\Program Files\copilot.exe`, []string{`COPILOT_HOME=C:\Users\me\copilot`, "COPILOT_GITHUB_TOKEN=secret"})
	if err != nil || !strings.Contains(command, `set "COPILOT_HOME=C:\Users\me\copilot" && "C:\Program Files\copilot.exe" login --device-code`) || strings.Contains(command, "secret") {
		t.Fatalf("Windows login command = %q, %v", command, err)
	}
	if _, err := BuildLoginCommand("linux", "local", "/tmp/local", nil); err == nil {
		t.Fatal("local connector accepted terminal login")
	}
}

func TestCodexBrowserLoginFlow(t *testing.T) {
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()
	closed := make(chan struct{})
	client := newClient(serverToClientR, clientToServerW, func() error {
		close(closed)
		_ = clientToServerW.Close()
		return nil
	})
	serverErr := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(clientToServerR)
		request, err := reader.ReadString('\n')
		var message map[string]any
		if err != nil || json.Unmarshal([]byte(request), &message) != nil || message["method"] != "initialize" {
			serverErr <- io.ErrUnexpectedEOF
			return
		}
		if _, err := io.WriteString(serverToClientW, `{"id":0,"result":{}}`+"\n"); err != nil {
			serverErr <- err
			return
		}
		if _, err := reader.ReadString('\n'); err != nil {
			serverErr <- err
			return
		}
		request, err = reader.ReadString('\n')
		if err != nil || json.Unmarshal([]byte(request), &message) != nil || message["method"] != "account/login/start" || message["params"].(map[string]any)["type"] != "chatgpt" {
			serverErr <- io.ErrUnexpectedEOF
			return
		}
		_, err = io.WriteString(serverToClientW, `{"id":1,"result":{"type":"chatgpt","authUrl":"https://auth.example/login","loginId":"id"}}`+"\n"+`{"method":"account/login/completed","params":{"success":true}}`+"\n")
		serverErr <- err
	}()
	defer clientToServerR.Close()
	defer serverToClientW.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Initialize(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	authURL, err := BeginCodexLogin(ctx, client)
	if err != nil || authURL != "https://auth.example/login" {
		t.Fatalf("BeginCodexLogin = %q, %v", authURL, err)
	}
	if err := WaitCodexLogin(ctx, client); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("app-server was not closed")
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}
