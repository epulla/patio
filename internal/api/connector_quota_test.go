package api

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
	"my-tickets/internal/db"
)

func TestClaudeQuotaStoredSlot(t *testing.T) {
	database, handler := newQuotaAPI(t)
	for _, tc := range []struct {
		name, stored string
		want         string
	}{
		{"null", "", `"note":"Unknown until the first chat"`},
		{"stored response", `{"kind":"claude","windows":[{"label":"5h","used_percent":42,"resets_at":null}],"requests":null,"tokens":null,"note":"rate limited","updated_at":"2026-10-09T10:00:00Z"}`, `"note":"rate limited"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "claude-" + strings.ReplaceAll(tc.name, " ", "-")
			var quota any
			if tc.stored != "" {
				quota = tc.stored
			}
			if _, err := database.Exec(`INSERT INTO connector(id,kind,name,config_dir,created_at,quota_json) VALUES(?,?,?,?,?,?)`, id, "claude", "Claude", id+"-dir", "now", quota); err != nil {
				t.Fatal(err)
			}
			code, raw := requestRaw(t, handler, http.MethodGet, "/api/connectors/"+id+"/quota", nil)
			if code != http.StatusOK || !strings.Contains(raw, tc.want) {
				t.Fatalf("quota = %d %s", code, raw)
			}
			if tc.name == "null" && !strings.Contains(raw, `"windows":[]`) {
				t.Fatalf("null quota windows = %s", raw)
			}
		})
	}
}

func TestConnectorQuotaNotFound(t *testing.T) {
	_, handler := newQuotaAPI(t)
	if code := requestJSON(t, handler, http.MethodGet, "/api/connectors/missing/quota", nil, nil); code != http.StatusNotFound {
		t.Fatalf("quota status = %d", code)
	}
}

func TestLocalModels(t *testing.T) {
	keyring.MockInit()
	_, handler := newQuotaAPI(t)
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"explicit secret", `{"base_url":"%s","secret":"local-secret"}`, http.StatusOK},
		{"bad URL", `{"base_url":"file:///tmp/v1"}`, http.StatusBadRequest},
		{"upstream error", `{"base_url":"%s","secret":"local-secret"}`, http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.name == "upstream error" {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = io.WriteString(w, `{"error":"nope local-secret"}`)
					return
				}
				if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer local-secret" {
					t.Errorf("request = %s %q", r.URL.Path, r.Header.Get("Authorization"))
				}
				_, _ = io.WriteString(w, `{"data":[{"id":"zeta"},{"id":"alpha"}]}`)
			}))
			defer server.Close()
			body := strings.ReplaceAll(tc.body, "%s", server.URL+"/v1")
			request := httptest.NewRequest(http.MethodPost, "/api/connectors/local-models", strings.NewReader(body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("models status = %d: %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "local-secret") {
				t.Fatalf("secret leaked: %s", response.Body.String())
			}
			if tc.want == http.StatusOK {
				var result struct {
					Models []string `json:"models"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || strings.Join(result.Models, ",") != "alpha,zeta" {
					t.Fatalf("models = %#v, %v", result, err)
				}
			}
		})
	}
}

func TestLocalModelsUsesStoredSecret(t *testing.T) {
	keyring.MockInit()
	database, handler := newQuotaAPI(t)
	const id = "local-with-key"
	if _, err := database.Exec(`INSERT INTO connector(id,kind,name,base_url,created_at) VALUES(?,?,?,?,?)`, id, "local", "Local", "http://localhost/v1", "now"); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(connectorService, connectorSecret(id), "stored-secret"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer stored-secret" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer server.Close()
	code, raw := requestRaw(t, handler, http.MethodPost, "/api/connectors/local-models", map[string]any{"base_url": server.URL + "/v1", "id": id})
	if code != http.StatusOK || strings.Contains(raw, "stored-secret") {
		t.Fatalf("stored key result = %d %s", code, raw)
	}
}

func newQuotaAPI(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "quota.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	return database, New(database, "local", false, "dev", filepath.Dir(path))
}
