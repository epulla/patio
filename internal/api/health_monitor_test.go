package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"
)

func TestHealthMonitorNotifiesOncePerOutage(t *testing.T) {
	keyring.MockInit()
	remote := mcp.NewServer(&mcp.Implementation{Name: "health-test", Version: "1"}, nil)
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, nil))
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	database, server := newMCPCheckServer(t, Options{})
	up := insertMCPCheckServer(t, database, "http", "", "[]", "[]", httpServer.URL, "none", true)
	insertMCPCheckServer(t, database, "http", "", "[]", "[]", dead.URL, "none", true)

	alerts := func() []string {
		t.Helper()
		rows, err := database.Query(`SELECT title FROM notification WHERE kind='health' ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		titles := []string{}
		for rows.Next() {
			var title string
			if err := rows.Scan(&title); err != nil {
				t.Fatal(err)
			}
			titles = append(titles, title)
		}
		return titles
	}
	states := map[string]*healthState{}
	tick := func() {
		t.Helper()
		if err := server.checkHealth(context.Background(), states); err != nil {
			t.Fatal(err)
		}
	}

	tick()
	httpServer.Close()
	tick()
	if got := alerts(); len(got) != 0 {
		t.Fatalf("alerts after first failure = %#v", got)
	}
	tick()
	tick()
	if want := []string{"MCP server down: Check " + up}; !reflect.DeepEqual(alerts(), want) {
		t.Fatalf("alerts = %#v, want %#v", alerts(), want)
	}
}
