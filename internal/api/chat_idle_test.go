package api

import (
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
	if kind != "chat" || title == "" || body != "["+title+"](/ai/chats/"+id+")" || link != "/ai/chats/"+id {
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

func TestChatSendRestartsRuntimeAfterIdleClose(t *testing.T) {
	manager, c, first := newIdleTestManager(t, 40*time.Millisecond)
	if _, err := manager.Send(c, "idle-test", "", ChatSendRequest{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.finishTurn(c.ID, "done", nil); err != nil {
		t.Fatal(err)
	}
	waitForDriverClose(t, first)
	if _, err := manager.Send(c, "idle-test", "", ChatSendRequest{Text: "second"}); err != nil {
		t.Fatal(err)
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
}

func (d *idleTestDriver) send(ChatSendRequest) error { return nil }
func (d *idleTestDriver) stop() error                { return d.close() }
func (d *idleTestDriver) close() error {
	d.closeCount.Add(1)
	d.closeOnce.Do(func() {
		close(d.closed)
	})
	return nil
}

var _ chatRuntimeDriver = (*idleTestDriver)(nil)
