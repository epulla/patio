package api

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestChatRuntimeClosesAfterIdleTimeout(t *testing.T) {
	manager, c, driver := newIdleTestManager(t, 60*time.Millisecond)
	if _, err := manager.Send(c, "idle-test", "", ChatSendRequest{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.finishTurn(c.ID, "done", nil); err != nil {
		t.Fatal(err)
	}
	waitForDriverClose(t, driver)
	if got := driver.closeCount.Load(); got != 1 {
		t.Fatalf("close count = %d, want 1", got)
	}
}

func TestReloadMCPClosesIdleRuntime(t *testing.T) {
	m, c, d := newIdleTestManager(t, time.Hour)
	if _, err := m.Send(c, "idle-test", "", ChatSendRequest{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := m.finishTurn(c.ID, "done", nil); err != nil {
		t.Fatal(err)
	}
	closed, pending, err := m.ReloadMCP()
	if err != nil || closed != 1 || pending != 0 {
		t.Fatalf("reload=(%d,%d,%v)", closed, pending, err)
	}
	select {
	case <-d.closed:
	default:
		t.Fatal("driver not closed")
	}
	if _, err := m.Send(c, "idle-test", "", ChatSendRequest{Text: "again"}); err != nil {
		t.Fatal(err)
	}
}

func TestReloadMCPDefersBusyRuntime(t *testing.T) {
	m, c, d := newIdleTestManager(t, time.Hour)
	if _, err := m.Send(c, "idle-test", "", ChatSendRequest{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	closed, pending, err := m.ReloadMCP()
	if err != nil || closed != 0 || pending != 1 {
		t.Fatalf("reload=(%d,%d,%v)", closed, pending, err)
	}
	select {
	case <-d.closed:
		t.Fatal("busy driver closed")
	default:
	}
	if err := m.finishTurn(c.ID, "done", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Send(c, "idle-test", "", ChatSendRequest{Text: "again"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.closed:
	default:
		t.Fatal("old driver not closed")
	}
	var content string
	if err := m.db.QueryRow(`SELECT content FROM chat_message WHERE chat_id=? AND content=?`, c.ID, "MCP servers changed — reconnected.").Scan(&content); err != nil {
		t.Fatal(err)
	}
}

func TestReloadMCPServerOnlyRestartsChatsUsingIt(t *testing.T) {
	m, c, d := newIdleTestManager(t, time.Hour)
	if _, err := m.Send(c, "idle-test", "", ChatSendRequest{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := m.finishTurn(c.ID, "done", nil); err != nil {
		t.Fatal(err)
	}
	rt := m.lookupRuntime(c.ID)
	rt.mu.Lock()
	rt.mcpSpecs = []mcpServerSpec{{Name: "used"}}
	rt.mu.Unlock()
	if closed, pending, err := m.ReloadMCPServer("other"); err != nil || closed != 0 || pending != 0 {
		t.Fatalf("other reload=(%d,%d,%v)", closed, pending, err)
	}
	select {
	case <-d.closed:
		t.Fatal("chat not using the server was closed")
	default:
	}
	if closed, pending, err := m.ReloadMCPServer("used"); err != nil || closed != 1 || pending != 0 {
		t.Fatalf("used reload=(%d,%d,%v)", closed, pending, err)
	}
	select {
	case <-d.closed:
	default:
		t.Fatal("chat using the server not closed")
	}
}

func TestReloadMCPServerHTTPUnknown(t *testing.T) {
	db, _, root := chatAPI(t)
	s := New(db, "local", false, "dev", root)
	defer s.Close()
	if code := requestJSON(t, s, "POST", "/api/mcp-servers/missing/reload", nil, nil); code != 404 {
		t.Fatalf("status=%d", code)
	}
}

func TestReloadMCPHTTP(t *testing.T) {
	db, _, root := chatAPI(t)
	s := New(db, "local", false, "dev", root)
	defer s.Close()
	var result map[string]int
	if code := requestJSON(t, s, "POST", "/api/mcp-servers/reload", nil, &result); code != 200 || result["closed"] != 0 || result["pending"] != 0 {
		t.Fatalf("status=%d result=%v", code, result)
	}
}

func TestMCPReloadRequestFromToolProcessReachesAppRuntimes(t *testing.T) {
	m, c, d := newIdleTestManager(t, time.Hour)
	if _, err := m.Send(c, "idle-test", "", ChatSendRequest{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	tool := New(m.db, "ai", true, "dev", m.dbDir)
	defer tool.Close()
	if code := requestJSON(t, tool, "POST", "/api/mcp-servers/reload-request", nil, nil); code != 202 {
		t.Fatalf("status=%d", code)
	}
	select {
	case <-d.closed:
		t.Fatal("calling chat closed mid-turn")
	default:
	}
	// The next message must reconnect even if the app's database watcher hasn't applied the request yet.
	if err := m.finishTurn(c.ID, "done", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Send(c, "idle-test", "", ChatSendRequest{Text: "again"}); err != nil {
		t.Fatal(err)
	}
	waitForDriverClose(t, d)
	var left int
	if err := m.db.QueryRow(`SELECT count(*) FROM app_setting WHERE key=?`, mcpReloadRequestKey).Scan(&left); err != nil || left != 0 {
		t.Fatalf("request rows left = %d, %v", left, err)
	}
}

func TestFinishedChatStoresNotification(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	id := createDriverTestChat(t, database, connectorID, root)
	server := New(database, "local", false, "dev", root)
	if err := server.chats.finishTurn(id, "reply", nil); err != nil {
		t.Fatal(err)
	}
	var kind, title, body, link string
	if err := database.QueryRow(`SELECT kind,title,body,link FROM notification`).Scan(&kind, &title, &body, &link); err != nil {
		t.Fatal(err)
	}
	if kind != "chat" || !strings.HasPrefix(title, "Reply finished: ") || body != "" || link != "/ai/chats/"+id {
		t.Fatalf("notification=%q %q %q %q", kind, title, body, link)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestChatRuntimeStaysOpenWhileBusy(t *testing.T) {
	manager, c, driver := newIdleTestManager(t, 40*time.Millisecond)
	if _, err := manager.Send(c, "idle-test", "", ChatSendRequest{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-driver.closed:
		t.Fatal("busy runtime closed")
	case <-time.After(100 * time.Millisecond):
	}
	if got := driver.closeCount.Load(); got != 0 {
		t.Fatalf("close count = %d, want 0", got)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestChatRuntimeActivityResetsIdleTimer(t *testing.T) {
	manager, c, first := newIdleTestManager(t, 120*time.Millisecond)
	if _, err := manager.Send(c, "idle-test", "", ChatSendRequest{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.finishTurn(c.ID, "done", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-time.After(70 * time.Millisecond):
	case <-first.closed:
		t.Fatal("runtime closed before idle timeout")
	}
	if _, err := manager.Send(c, "idle-test", "", ChatSendRequest{Text: "second"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.finishTurn(c.ID, "done", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.closed:
		t.Fatal("activity did not reset idle timer")
	case <-time.After(70 * time.Millisecond):
	}
	waitForDriverClose(t, first)
}

func TestChatSendDuringIdleCloseWaitsAndRestarts(t *testing.T) {
	manager, c, first := newIdleTestManager(t, 40*time.Millisecond)
	first.release = make(chan struct{})
	if _, err := manager.Send(c, "idle-test", "", ChatSendRequest{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.finishTurn(c.ID, "done", nil); err != nil {
		t.Fatal(err)
	}
	waitForDriverClose(t, first)
	sent := make(chan error, 1)
	go func() {
		_, err := manager.Send(c, "idle-test", "", ChatSendRequest{Text: "second"})
		sent <- err
	}()
	select {
	case err := <-sent:
		close(first.release)
		t.Fatalf("send returned during idle close: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(first.release)
	select {
	case err := <-sent:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("send did not finish after idle close")
	}
	if manager.lookupRuntime(c.ID).driver == first {
		t.Fatal("send reused closed runtime")
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestChatManagerShutdownStopsIdleTimers(t *testing.T) {
	manager, c, driver := newIdleTestManager(t, 100*time.Millisecond)
	if _, err := manager.Send(c, "idle-test", "", ChatSendRequest{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.finishTurn(c.ID, "done", nil); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if got := driver.closeCount.Load(); got != 1 {
		t.Fatalf("close count after shutdown = %d, want 1", got)
	}
	rt := manager.lookupRuntime(c.ID)
	rt.mu.Lock()
	timer := rt.idleTimer
	rt.mu.Unlock()
	if timer != nil {
		t.Fatal("shutdown left idle timer attached")
	}
	select {
	case <-driver.closed:
	case <-time.After(150 * time.Millisecond):
		t.Fatal("shutdown did not close runtime")
	}
	if got := driver.closeCount.Load(); got != 1 {
		t.Fatalf("close count after timer window = %d, want 1", got)
	}
}

func newIdleTestManager(t *testing.T, timeout time.Duration) (*chatManager, chat, *idleTestDriver) {
	t.Helper()
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	manager := newChatManager(database, Options{})
	manager.dbDir = root
	manager.idleTimeout = timeout
	chatConnectorID := connectorID
	c := chat{ID: chatID, ConnectorID: &chatConnectorID}
	previous, existed := runtimes["idle-test"]
	first := &idleTestDriver{closed: make(chan struct{})}
	starts := 0
	runtimes["idle-test"] = func(*chatManager, chat, string, *chatRuntime) (chatRuntimeDriver, error) {
		var driver *idleTestDriver
		if starts == 0 {
			driver = first
		} else {
			driver = &idleTestDriver{closed: make(chan struct{})}
		}
		starts++
		return driver, nil
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
		if existed {
			runtimes["idle-test"] = previous
		} else {
			delete(runtimes, "idle-test")
		}
	})
	return manager, c, first
}

func waitForDriverClose(t *testing.T, driver *idleTestDriver) {
	t.Helper()
	select {
	case <-driver.closed:
	case <-time.After(time.Second):
		t.Fatal("runtime did not close")
	}
}

type idleTestDriver struct {
	closed     chan struct{}
	closeOnce  sync.Once
	closeCount atomic.Int32
	release    chan struct{}
}

func (d *idleTestDriver) send(ChatSendRequest) error { return nil }
func (d *idleTestDriver) stop() error                { return d.close() }
func (d *idleTestDriver) close() error {
	d.closeCount.Add(1)
	d.closeOnce.Do(func() {
		close(d.closed)
	})
	if d.release != nil {
		<-d.release
	}
	return nil
}

var _ chatRuntimeDriver = (*idleTestDriver)(nil)
