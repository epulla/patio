package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestConnectorLoginRejectsUnsupportedAccounts(t *testing.T) {
	keyring.MockInit()
	database, handler := newAPI(t)
	for _, item := range []struct {
		id, kind string
		config   any
	}{
		{"local", "local", nil},
		{"copilot-pat", "copilot", "/tmp/copilot"},
	} {
		if _, err := database.Exec(`INSERT INTO connector(id,kind,name,config_dir,base_url,model,created_at) VALUES(?,?,?,?,?,?,?)`, item.id, item.kind, item.kind, item.config, "", "", now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := keyring.Set(connectorService, connectorSecret("copilot-pat"), "pat-secret"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id, message string
		status      int
	}{
		{"missing", "connector not found", http.StatusNotFound},
		{"local", "local connectors don't log in", http.StatusBadRequest},
		{"copilot-pat", "PAT connectors don't log in", http.StatusBadRequest},
	} {
		code, body := requestRaw(t, handler, http.MethodPost, "/api/connectors/"+test.id+"/login", nil)
		if code != test.status || !strings.Contains(body, test.message) {
			t.Errorf("login %s = %d %q, want %d containing %q", test.id, code, body, test.status, test.message)
		}
	}
}
