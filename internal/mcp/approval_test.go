package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"my-tickets/internal/api"
	"my-tickets/internal/db"
)

func TestApprovalTool(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	const chatID = "chat-test"
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at) VALUES(?,?,?,?)`, chatID, "Test", "now", "now"); err != nil {
		t.Fatal(err)
	}
	allowed, _, err := approvalHandler(context.Background(), &ApprovalOptions{DB: database, ChatID: chatID}, approveArgs{
		ToolName: "mcp__my-tickets__load_skill", Input: json.RawMessage(`{"name":"pirate-voice"}`),
	})
	if err != nil || allowed.Content[0].(*sdk.TextContent).Text != `{"behavior":"allow","updatedInput":{"name":"pirate-voice"}}` {
		t.Fatalf("skill approval result = %#v, %v", allowed, err)
	}
	var skillApprovals int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chat_approval WHERE chat_id=?`, chatID).Scan(&skillApprovals); err != nil || skillApprovals != 0 {
		t.Fatalf("skill approval rows = %d, %v", skillApprovals, err)
	}
	h := api.New(database, "local", false, "dev", t.TempDir())
	defer h.Close()
	call := func(timeout time.Duration) (*sdk.ClientSession, func()) {
		t.Helper()
		server, err := NewServerWithApproval(h, "dev", ApprovalOptions{DB: database, ChatID: chatID, Timeout: timeout, PollInterval: time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		st, ct := sdk.NewInMemoryTransports()
		if _, err := server.Connect(context.Background(), st, nil); err != nil {
			t.Fatal(err)
		}
		session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
		if err != nil {
			t.Fatal(err)
		}
		return session, func() {
			if err := session.Close(); err != nil {
				t.Error(err)
			}
		}
	}

	session, closeSession := call(time.Second)
	defer closeSession()
	for _, allow := range []bool{true, false} {
		input := map[string]any{"command": "write", "path": "x"}
		resultCh := make(chan *sdk.CallToolResult, 1)
		errCh := make(chan error, 1)
		go func() {
			result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "approve", Arguments: map[string]any{"tool_name": "write_file", "input": input, "extra": true}})
			resultCh <- result
			errCh <- err
		}()
		id := pendingApprovalID(t, database, chatID)
		var toolName, approvalInput, status string
		if err := database.QueryRow(`SELECT tool_name,input_json,status FROM chat_approval WHERE id=?`, id).Scan(&toolName, &approvalInput, &status); err != nil {
			t.Fatal(err)
		}
		if toolName != "write_file" || approvalInput != `{"command":"write","path":"x"}` || status != "pending" {
			t.Fatalf("approval row = (%q, %q, %q)", toolName, approvalInput, status)
		}
		body, err := json.Marshal(map[string]bool{"allow": allow})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/chats/"+chatID+"/approvals/"+id, bytes.NewReader(body))
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("answer status = %d: %s", res.Code, res.Body)
		}
		select {
		case result := <-resultCh:
			if err := <-errCh; err != nil {
				t.Fatal(err)
			}
			text := result.Content[0].(*sdk.TextContent).Text
			if allow {
				want, err := json.Marshal(map[string]any{"behavior": "allow", "updatedInput": input})
				if err != nil {
					t.Fatal(err)
				}
				if text != string(want) {
					t.Fatalf("allow result = %s, want %s", text, want)
				}
			} else if text != `{"behavior":"deny","message":"Denied by user"}` {
				t.Fatalf("deny result = %s", text)
			}
		case <-time.After(time.Second):
			t.Fatal("approval call did not return")
		}
	}
	short, closeShort := call(20 * time.Millisecond)
	defer closeShort()
	result, err := short.CallTool(context.Background(), &sdk.CallToolParams{Name: "approve", Arguments: map[string]any{"tool_name": "write", "input": map[string]any{}}})
	if err != nil {
		t.Fatal(err)
	}
	if text := result.Content[0].(*sdk.TextContent).Text; text != `{"behavior":"deny","message":"Approval timed out"}` {
		t.Fatalf("timeout result = %s", text)
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM chat_approval ORDER BY rowid DESC LIMIT 1`).Scan(&status); err != nil || status != "expired" {
		t.Fatalf("timeout status = %q, err = %v", status, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultCh := make(chan *sdk.CallToolResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, _, err := approvalHandler(ctx, &ApprovalOptions{DB: database, ChatID: chatID, Timeout: time.Second, PollInterval: time.Millisecond}, approveArgs{ToolName: "write", Input: json.RawMessage(`{"path":"y"}`)})
		resultCh <- result
		errCh <- err
	}()
	cancelledID := pendingApprovalID(t, database, chatID)
	approvalPath := "/api/chats/" + chatID + "/approvals/" + cancelledID
	body, err := json.Marshal(map[string]bool{"allow": true})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, approvalPath, bytes.NewReader(body))
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("answer approval before cancellation = %d: %s", res.Code, res.Body)
	}
	cancel()
	select {
	case result := <-resultCh:
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
		if text := result.Content[0].(*sdk.TextContent).Text; text != `{"behavior":"allow","updatedInput":{"path":"y"}}` {
			t.Fatalf("answer racing cancellation = %s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled approval call did not return")
	}

	expiredCtx, cancelExpired := context.WithCancel(context.Background())
	defer cancelExpired()
	expiredResultCh := make(chan *sdk.CallToolResult, 1)
	expiredErrCh := make(chan error, 1)
	go func() {
		result, _, err := approvalHandler(expiredCtx, &ApprovalOptions{DB: database, ChatID: chatID, Timeout: time.Second, PollInterval: time.Millisecond}, approveArgs{ToolName: "write", Input: json.RawMessage(`{}`)})
		expiredResultCh <- result
		expiredErrCh <- err
	}()
	expiredID := pendingApprovalID(t, database, chatID)
	cancelExpired()
	select {
	case result := <-expiredResultCh:
		if err := <-expiredErrCh; err != nil {
			t.Fatal(err)
		}
		if text := result.Content[0].(*sdk.TextContent).Text; text != `{"behavior":"deny","message":"Denied by user"}` {
			t.Fatalf("cancel result = %s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled approval did not return")
	}
	if err := database.QueryRow(`SELECT status FROM chat_approval WHERE id=?`, expiredID).Scan(&status); err != nil || status != "expired" {
		t.Fatalf("cancelled approval status = %q, err = %v", status, err)
	}
}

func TestApprovalToolAutoAllowedFromDatabase(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "auto-allow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	const chatID = "auto-chat"
	if _, err := database.Exec(`INSERT INTO chat(id,title,created_at,updated_at,auto_allow) VALUES(?,?,?, ?,1)`, chatID, "Auto", "now", "now"); err != nil {
		t.Fatal(err)
	}
	result, _, err := approvalHandler(context.Background(), &ApprovalOptions{DB: database, ChatID: chatID, Timeout: time.Second, PollInterval: time.Millisecond}, approveArgs{
		ToolName: "write_file", Input: json.RawMessage(`{"path":"x"}`),
	})
	if err != nil || result.Content[0].(*sdk.TextContent).Text != `{"behavior":"allow","updatedInput":{"path":"x"}}` {
		t.Fatalf("auto-allowed result = %#v, %v", result, err)
	}
	var status string
	var auto bool
	if err := database.QueryRow(`SELECT status,auto FROM chat_approval WHERE chat_id=?`, chatID).Scan(&status, &auto); err != nil || status != "allowed" || !auto {
		t.Fatalf("auto approval = (%q, %v), %v", status, auto, err)
	}
	if _, err := database.Exec(`UPDATE chat SET auto_allow=0 WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	h := api.New(database, "local", false, "dev", t.TempDir())
	defer h.Close()
	resultCh := make(chan *sdk.CallToolResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, _, err := approvalHandler(context.Background(), &ApprovalOptions{DB: database, ChatID: chatID, Timeout: time.Second, PollInterval: time.Millisecond}, approveArgs{
			ToolName: "write_file", Input: json.RawMessage(`{"path":"pending"}`),
		})
		resultCh <- result
		errCh <- err
	}()
	pendingID := pendingApprovalID(t, database, chatID)
	body := bytes.NewBufferString(`{"auto_allow":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/chats/"+chatID, body)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("enable auto-allow = %d: %s", res.Code, res.Body)
	}
	select {
	case result := <-resultCh:
		if err := <-errCh; err != nil || result.Content[0].(*sdk.TextContent).Text != `{"behavior":"allow","updatedInput":{"path":"pending"}}` {
			t.Fatalf("pending Claude approval = %#v, %v", result, err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending Claude approval waiter did not return")
	}
	if err := database.QueryRow(`SELECT status,auto FROM chat_approval WHERE id=?`, pendingID).Scan(&status, &auto); err != nil || status != "allowed" || !auto {
		t.Fatalf("resolved Claude approval = (%q, %v), %v", status, auto, err)
	}
}

func pendingApprovalID(t *testing.T, database *sql.DB, chatID string) string {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		var id string
		err := database.QueryRow(`SELECT id FROM chat_approval WHERE chat_id=? AND status='pending' ORDER BY rowid DESC LIMIT 1`, chatID).Scan(&id)
		if err == nil {
			return id
		}
		if err != sql.ErrNoRows {
			t.Fatal(err)
		}
		select {
		case <-deadline:
			t.Fatal("approval row not inserted")
		case <-time.After(time.Millisecond):
		}
	}
}
