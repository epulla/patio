package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
	connectorcli "patio/internal/connector"
)

func TestConnectorModelsAndRecordedCLIParsers(t *testing.T) {
	database, handler, _ := chatAPI(t)
	id := createTestConnector(t, database)
	var response struct {
		Models []selectableModel `json:"models"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/connectors/"+id+"/models", nil, &response); code != http.StatusOK || len(response.Models) != 4 || response.Models[0].ID != "" || response.Models[1].ID != "opus" || response.Models[1].Label != "Opus" || response.Models[3].ID != "haiku" {
		t.Fatalf("Claude models = %d %#v", code, response.Models)
	}
	if response.Models[0].Efforts == nil || len(*response.Models[0].Efforts) != 5 || response.Models[1].Efforts == nil {
		t.Fatalf("Claude efforts = %#v", response.Models)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/connectors/missing/models", nil, nil); code != http.StatusNotFound {
		t.Fatalf("missing connector status = %d", code)
	}
	for _, tc := range []struct {
		name  string
		file  string
		count int
	}{
		{"v1", "opencode-models.txt", 3},
		{"v2", "opencode-v2-models.txt", 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			openCode, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			models := parseOpenCodeModels(string(openCode))
			if len(models) != tc.count || models[0].ID != "opencode/big-pickle" || models[0].Label != models[0].ID {
				t.Fatalf("OpenCode fixture models = %#v", models)
			}
		})
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

func TestConnectorModelsOpenCodeEfforts(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "catalog", true: "catalog failure"}[fail], func(t *testing.T) {
			database, _, root := chatAPI(t)
			id := createTestConnector(t, database)
			if _, err := database.Exec(`UPDATE connector SET kind='opencode' WHERE id=?`, id); err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			script := `#!/bin/sh
case "$*" in
  models) printf '%s\n' 'p/model' 'p/empty' 'p/missing' ;;
  "api GET /api/model")
    [ "$FAIL_OPENCODE_CATALOG" = 1 ] && exit 1
    printf '%s' '{"data":[{"id":"model","providerID":"p","variants":[{"id":"low"}]},{"id":"empty","providerID":"p","variants":[]},{"id":"missing","providerID":"p"}]}' ;;
  "api GET /api/model/default")
    [ "$FAIL_OPENCODE_CATALOG" = 1 ] && exit 1
    printf '%s' '{"data":{"id":"model","providerID":"p","variants":[{"id":"medium"}]}}' ;;
  *) exit 2 ;;
esac
`
			path := filepath.Join(bin, "opencode")
			if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			if fail {
				t.Setenv("FAIL_OPENCODE_CATALOG", "1")
			} else {
				t.Setenv("FAIL_OPENCODE_CATALOG", "0")
			}
			server := New(database, "local", false, "dev", root)
			var result struct {
				Models []selectableModel `json:"models"`
				Note   string            `json:"note"`
			}
			if code := requestJSON(t, server, http.MethodGet, "/api/connectors/"+id+"/models", nil, &result); code != http.StatusOK {
				t.Fatalf("models status=%d", code)
			}
			if len(result.Models) != 4 {
				t.Fatalf("models=%#v", result.Models)
			}
			if fail {
				if result.Note == "" {
					t.Fatal("failed catalog response missing note")
				}
				for _, model := range result.Models {
					if model.Efforts != nil {
						t.Fatalf("failed catalog effort for %q = %v", model.ID, *model.Efforts)
					}
				}
				return
			}
			if result.Note != "" {
				t.Fatalf("success response has note %q", result.Note)
			}
			if result.Models[0].Efforts == nil || len(*result.Models[0].Efforts) != 1 || (*result.Models[0].Efforts)[0] != "medium" {
				t.Fatalf("default efforts=%v", result.Models[0].Efforts)
			}
			if result.Models[1].Efforts == nil || len(*result.Models[1].Efforts) != 1 || (*result.Models[1].Efforts)[0] != "low" {
				t.Fatalf("model efforts=%v", result.Models[1].Efforts)
			}
			if result.Models[2].Efforts == nil || len(*result.Models[2].Efforts) != 0 {
				t.Fatalf("empty variants=%v", result.Models[2].Efforts)
			}
			if result.Models[3].Efforts != nil {
				t.Fatalf("missing variants=%v, want unknown", *result.Models[3].Efforts)
			}
		})
	}
}

func TestConvertModelsPreservesCodexEfforts(t *testing.T) {
	models := convertModels([]connectorcli.Model{{ID: "codex-model", Label: "Codex", Efforts: []string{"minimal", "low"}}})
	if len(models) != 1 || models[0].Efforts == nil || len(*models[0].Efforts) != 2 || (*models[0].Efforts)[0] != "minimal" {
		t.Fatalf("converted Codex models=%#v", models)
	}
}
