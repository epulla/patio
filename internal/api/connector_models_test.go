package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestConnectorModelsAndRecordedCLIParsers(t *testing.T) {
	database, handler, _ := chatAPI(t)
	id := createTestConnector(t, database)
	var response struct {
		Models []selectableModel `json:"models"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/connectors/"+id+"/models", nil, &response); code != http.StatusOK || len(response.Models) != 4 || response.Models[0].ID != "" || response.Models[1] != (selectableModel{"opus", "Opus", "Most capable"}) || response.Models[3].ID != "haiku" {
		t.Fatalf("Claude models = %d %#v", code, response.Models)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/connectors/missing/models", nil, nil); code != http.StatusNotFound {
		t.Fatalf("missing connector status = %d", code)
	}
	openCode, err := os.ReadFile(filepath.Join("testdata", "opencode-models.txt"))
	if err != nil {
		t.Fatal(err)
	}
	models := parseOpenCodeModels(string(openCode))
	if len(models) != 3 || models[0].ID != "opencode/big-pickle" || models[0].Label != models[0].ID {
		t.Fatalf("OpenCode fixture models = %#v", models)
	}
	copilotHelp, err := os.ReadFile(filepath.Join("testdata", "copilot-help.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if models := parseCopilotModels(string(copilotHelp)); len(models) != 0 {
		t.Fatalf("Copilot help models = %#v", models)
	}
	if models := parseCopilotModels("--model <model> Possible values: auto, gpt-5.4"); len(models) != 2 || models[1].ID != "gpt-5.4" {
		t.Fatalf("Copilot choices = %#v", models)
	}
}

func TestConnectorModelsLocalUsesSavedSecret(t *testing.T) {
	keyring.MockInit()
	database, _, root := chatAPI(t)
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer saved-secret" {
			t.Errorf("local model request = %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"model-z"},{"id":"model-a"}]}`))
	}))
	defer local.Close()
	id := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,base_url,created_at) VALUES(?,'local','Local',?,?)`, id, local.URL+"/v1", now()); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(connectorService, connectorSecret(id), "saved-secret"); err != nil {
		t.Fatal(err)
	}
	server := New(database, "local", false, "dev", root)
	var response struct {
		Models []selectableModel `json:"models"`
	}
	if code := requestJSON(t, server, http.MethodGet, "/api/connectors/"+id+"/models", nil, &response); code != http.StatusOK || len(response.Models) != 3 || response.Models[1].ID != "model-a" || response.Models[2].ID != "model-z" {
		t.Fatalf("local models = %d %#v", code, response.Models)
	}
}

func TestConnectorModelsCLIFailureNote(t *testing.T) {
	database, _, root := chatAPI(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := upstream.URL + "/v1"
	upstream.Close()
	id := uid()
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,base_url,created_at) VALUES(?,'local','Local',?,?)`, id, baseURL, now()); err != nil {
		t.Fatal(err)
	}
	server := New(database, "local", false, "dev", root)
	code, raw := requestRaw(t, server, http.MethodGet, "/api/connectors/"+id+"/models", nil)
	var response struct {
		Models []selectableModel `json:"models"`
		Note   string            `json:"note"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil || code != http.StatusOK || len(response.Models) != 1 || response.Models[0].ID != "" || response.Note == "" {
		t.Fatalf("failure result = %d %s (%v)", code, raw, err)
	}
}
