package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
	"my-tickets/internal/db"
)

type testBoard struct {
	ID        string `json:"id"`
	ShortName string `json:"short_name"`
}
type testStatus struct {
	ID            string `json:"id"`
	HideAfterDays int    `json:"hide_after_days"`
}
type testTicket struct {
	ID         string  `json:"id"`
	Code       string  `json:"code"`
	Title      string  `json:"title"`
	ReporterID *string `json:"reporter_id"`
	BoardName  string  `json:"board_name"`
	StatusName string  `json:"status_name"`
}
type testCalendarItem struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	Title         string  `json:"title"`
	StartsAt      string  `json:"starts_at"`
	BoardID       *string `json:"board_id"`
	TicketID      *string `json:"ticket_id"`
	CreatedBy     string  `json:"created_by"`
	CreatedByIsAI bool    `json:"created_by_is_ai"`
}
type testCalendarResponse struct {
	Items   []testCalendarItem `json:"items"`
	Tickets []testTicket       `json:"tickets"`
}
type testData struct {
	Statuses []struct {
		ID            string       `json:"id"`
		HideAfterDays int          `json:"hide_after_days"`
		Tickets       []testTicket `json:"tickets"`
	} `json:"statuses"`
}

func newAPI(t *testing.T) (*sql.DB, http.Handler) {
	return newAPIAs(t, "local")
}

func newAPIAs(t *testing.T, userID string) (*sql.DB, http.Handler) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	return database, New(database, userID, false, "dev", filepath.Dir(dbPath))
}

func TestConfigReturnsVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	}()
	handler := New(database, "local", false, "v1.2.3", filepath.Dir(dbPath))
	var config struct {
		Version string `json:"version"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/config", nil, &config); code != http.StatusOK {
		t.Fatalf("GET /api/config status = %d, want %d", code, http.StatusOK)
	}
	if config.Version != "v1.2.3" {
		t.Fatalf("config version = %q, want %q", config.Version, "v1.2.3")
	}
}

func requestJSON(t *testing.T, handler http.Handler, method, path string, body any, out any) int {
	t.Helper()
	var input *bytes.Reader
	if body == nil {
		input = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		input = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, path, input)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if out != nil && res.Body.Len() > 0 {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return res.Code
}

func requestRaw(t *testing.T, handler http.Handler, method, path string, body any) (int, string) {
	t.Helper()
	var input bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&input).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &input)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res.Code, res.Body.String()
}

func TestConnectorCreatePathsAndListOrder(t *testing.T) {
	keyring.MockInit()
	database, handler := newAPI(t)
	tests := []struct {
		name, kind  string
		body        map[string]any
		dir, secret bool
		status      int
	}{
		{"claude existing", "claude", map[string]any{"kind": "claude", "name": "Claude", "use_existing_login": true}, false, false, 201},
		{"codex isolated", "codex", map[string]any{"kind": "codex", "name": "Codex"}, true, false, 201},
		{"opencode isolated", "opencode", map[string]any{"kind": "opencode", "name": "OpenCode"}, true, false, 201},
		{"copilot PAT", "copilot", map[string]any{"kind": "copilot", "name": "Copilot PAT", "secret": "copilot-pat"}, true, true, 201},
		{"copilot existing", "copilot", map[string]any{"kind": "copilot", "name": "Copilot login", "use_existing_login": true}, false, false, 201},
		{"local key", "local", map[string]any{"kind": "local", "name": "Local", "base_url": "http://localhost:11434/v1", "secret": "local-key"}, false, true, 201},
	}
	ids := make([]string, 0, len(tests))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.kind == "opencode" && runtime.GOOS == "windows" {
				tc.status, tc.dir = 400, false
			}
			var got connector
			code, raw := requestRaw(t, handler, http.MethodPost, "/api/connectors", tc.body)
			if code != tc.status {
				t.Fatalf("create status = %d, want %d", code, tc.status)
			}
			if code != http.StatusCreated {
				return
			}
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatal(err)
			}
			if got.Kind != tc.kind || got.HasSecret != tc.secret || (got.ConfigDir != nil) != tc.dir {
				t.Fatalf("created connector = %#v", got)
			}
			if got.ConfigDir != nil {
				info, err := os.Stat(*got.ConfigDir)
				if err != nil || !info.IsDir() || runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
					t.Fatalf("config dir stat = %v, %v", info, err)
				}
			}
			ids = append(ids, got.ID)
		})
	}
	if _, err := database.Exec(`UPDATE connector SET created_at='same'`); err != nil {
		t.Fatal(err)
	}
	var listed []connector
	if code := requestJSON(t, handler, http.MethodGet, "/api/connectors", nil, &listed); code != http.StatusOK {
		t.Fatalf("list status = %d", code)
	}
	if len(listed) != len(ids) {
		t.Fatalf("listed connectors = %d", len(listed))
	}
	for i := range ids {
		if listed[i].ID != ids[i] {
			t.Fatalf("list order = %v, want %v", listed, ids)
		}
	}
}

func TestConnectorCreateValidationAndExistingLoginUniqueness(t *testing.T) {
	keyring.MockInit()
	_, handler := newAPI(t)
	body := map[string]any{"kind": "claude", "name": "Claude", "use_existing_login": true}
	if code := requestJSON(t, handler, http.MethodPost, "/api/connectors", body, nil); code != http.StatusCreated {
		t.Fatalf("first existing login status = %d", code)
	}
	tests := []struct {
		name string
		body map[string]any
	}{
		{"duplicate existing login", body},
		{"unknown kind", map[string]any{"kind": "unknown", "name": "Connector"}},
		{"empty name", map[string]any{"kind": "claude", "name": "  "}},
		{"copilot PAT required", map[string]any{"kind": "copilot", "name": "Copilot"}},
		{"local URL required", map[string]any{"kind": "local", "name": "Local"}},
		{"local URL scheme", map[string]any{"kind": "local", "name": "Local", "base_url": "ftp://localhost/api"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if code := requestJSON(t, handler, http.MethodPost, "/api/connectors", tc.body, nil); code != http.StatusBadRequest {
				t.Fatalf("create status = %d, want 400", code)
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, "/api/connectors", strings.NewReader("{"))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON status = %d, want 400", res.Code)
	}
}

func TestConnectorUpdateSecretAndDelete(t *testing.T) {
	keyring.MockInit()
	database, handler := newAPI(t)
	var created connector
	if code, raw := requestRaw(t, handler, http.MethodPost, "/api/connectors", map[string]any{"kind": "copilot", "name": "Copilot", "secret": "first-secret"}); code != http.StatusCreated || strings.Contains(raw, "first-secret") {
		t.Fatalf("create response status/body = %d %s", code, raw)
	} else if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatal(err)
	}
	if value, err := keyring.Get(connectorService, connectorSecret(created.ID)); err != nil || value != "first-secret" {
		t.Fatalf("initial keyring value = %q, %v", value, err)
	}
	var stored string
	if err := database.QueryRow(`SELECT id||kind||name||coalesce(config_dir,'')||base_url||model||coalesce(quota_json,'')||created_at FROM connector WHERE id=?`, created.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "first-secret") {
		t.Fatalf("connector row contains secret: %q", stored)
	}
	if code, raw := requestRaw(t, handler, http.MethodGet, "/api/connectors", nil); code != http.StatusOK || strings.Contains(raw, "first-secret") {
		t.Fatalf("list response status/body = %d %s", code, raw)
	}
	for _, tc := range []struct {
		name    string
		body    map[string]any
		secret  string
		present bool
	}{
		{"keep", map[string]any{"name": "Renamed"}, "first-secret", true},
		{"replace", map[string]any{"secret": "second-secret"}, "second-secret", true},
		{"delete", map[string]any{"secret": ""}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, raw := requestRaw(t, handler, http.MethodPut, "/api/connectors/"+created.ID, tc.body)
			if code != http.StatusOK || strings.Contains(raw, "first-secret") || strings.Contains(raw, "second-secret") {
				t.Fatalf("update response status/body = %d %s", code, raw)
			}
			var got connector
			if err := json.Unmarshal([]byte(raw), &got); err != nil || got.HasSecret != tc.present {
				t.Fatalf("updated connector = %#v, %v", got, err)
			}
			value, err := keyring.Get(connectorService, connectorSecret(created.ID))
			if tc.present && (err != nil || value != tc.secret) || !tc.present && !errors.Is(err, keyring.ErrNotFound) {
				t.Fatalf("keyring after %s = %q, %v", tc.name, value, err)
			}
		})
	}
	var isolated connector
	if code := requestJSON(t, handler, http.MethodPost, "/api/connectors", map[string]any{"kind": "codex", "name": "Codex"}, &isolated); code != http.StatusCreated {
		t.Fatalf("create isolated = %d", code)
	}
	var pat connector
	if code := requestJSON(t, handler, http.MethodPost, "/api/connectors", map[string]any{"kind": "copilot", "name": "PAT", "secret": "delete-me"}, &pat); code != http.StatusCreated {
		t.Fatalf("create PAT = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/connectors/"+pat.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete connector = %d", code)
	}
	if _, err := os.Stat(*pat.ConfigDir); !os.IsNotExist(err) {
		t.Fatalf("connector dir remains: %v", err)
	}
	if _, err := keyring.Get(connectorService, connectorSecret(pat.ID)); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("deleted connector secret remains: %v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM connector WHERE id=?`, pat.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted connector count = %d, %v", count, err)
	}

	outside := filepath.Join(filepath.Dir(filepath.Dir(*isolated.ConfigDir)), "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outside, "keep")
	if err := os.WriteFile(marker, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	traversal := filepath.Join(filepath.Dir(*isolated.ConfigDir), "..", "outside")
	if _, err := database.Exec(`UPDATE connector SET config_dir=? WHERE id=?`, traversal, isolated.ID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/connectors/"+isolated.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete traversal connector = %d", code)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("delete removed path outside connector prefix: %v", err)
	}
}

func createBoard(t *testing.T, handler http.Handler, short string, statuses []string) testBoard {
	t.Helper()
	var out testBoard
	body := map[string]any{"name": short, "short_name": short}
	if statuses != nil {
		body["statuses"] = statuses
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards", body, &out); code != http.StatusCreated {
		t.Fatalf("create board status = %d", code)
	}
	return out
}

func boardStatuses(t *testing.T, handler http.Handler, boardID string) []testStatus {
	t.Helper()
	var data testData
	if code := requestJSON(t, handler, http.MethodGet, "/api/boards/"+boardID+"/data", nil, &data); code != http.StatusOK {
		t.Fatalf("board data status = %d", code)
	}
	out := make([]testStatus, len(data.Statuses))
	for i := range data.Statuses {
		out[i].ID = data.Statuses[i].ID
	}
	return out
}

func createTicket(t *testing.T, handler http.Handler, boardID string, statusID string) testTicket {
	t.Helper()
	var out testTicket
	body := map[string]any{"title": "Ticket"}
	if statusID != "" {
		body["status_id"] = statusID
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+boardID+"/tickets", body, &out); code != http.StatusCreated {
		t.Fatalf("create ticket status = %d", code)
	}
	return out
}

func TestTicketNumberingIndependentPerBoard(t *testing.T) {
	database, handler := newAPI(t)
	a := createBoard(t, handler, "AAA", nil)
	b := createBoard(t, handler, "BBB", nil)
	one := createTicket(t, handler, a.ID, "")
	two := createTicket(t, handler, a.ID, "")
	other := createTicket(t, handler, b.ID, "")
	if one.Code != "AAA-1" || two.Code != "AAA-2" || other.Code != "BBB-1" {
		t.Fatalf("codes = %q, %q, %q", one.Code, two.Code, other.Code)
	}
	var aLast, bLast int
	if err := database.QueryRow("SELECT last_number FROM board_counter WHERE board_id=?", a.ID).Scan(&aLast); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT last_number FROM board_counter WHERE board_id=?", b.ID).Scan(&bLast); err != nil {
		t.Fatal(err)
	}
	if aLast != 2 || bLast != 1 {
		t.Fatalf("counters = %d, %d", aLast, bLast)
	}
}

func TestMoveAcrossColumnsAuditsAndOrders(t *testing.T) {
	database, handler := newAPI(t)
	b := createBoard(t, handler, "MOVE", []string{"To Do", "In Progress"})
	statuses := boardStatuses(t, handler, b.ID)
	first := createTicket(t, handler, b.ID, statuses[0].ID)
	second := createTicket(t, handler, b.ID, statuses[0].ID)
	var beforeHistory int
	if err := database.QueryRow("SELECT COUNT(*) FROM ticket_history WHERE ticket_id=?", second.ID).Scan(&beforeHistory); err != nil {
		t.Fatal(err)
	}
	var moved testTicket
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+second.ID+"/move", map[string]any{"status_id": statuses[1].ID, "index": 0}, &moved); code != http.StatusOK {
		t.Fatalf("move status = %d", code)
	}
	var transitions, histories int
	if err := database.QueryRow("SELECT COUNT(*) FROM ticket_status_transition WHERE ticket_id=? AND from_status_id IS NOT NULL", second.ID).Scan(&transitions); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM ticket_history WHERE ticket_id=?", second.ID).Scan(&histories); err != nil {
		t.Fatal(err)
	}
	if transitions != 1 || histories-beforeHistory != 1 {
		t.Fatalf("move audit counts = transition %d, history delta %d", transitions, histories-beforeHistory)
	}
	var history string
	if err := database.QueryRow("SELECT content FROM ticket_history WHERE ticket_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1", second.ID).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if history != "Status changed from To Do to In Progress" {
		t.Fatalf("move history = %q", history)
	}
	assertOrder(t, database, statuses[0].ID, []string{first.ID})
	assertOrder(t, database, statuses[1].ID, []string{second.ID})
}

func TestActingUserIsConfigurable(t *testing.T) {
	database, handler := newAPIAs(t, "ai")
	b := createBoard(t, handler, "AI", []string{"Todo", "Done"})
	statuses := boardStatuses(t, handler, b.ID)
	ticket := createTicket(t, handler, b.ID, statuses[0].ID)
	if ticket.ReporterID == nil || *ticket.ReporterID != "ai" {
		t.Fatalf("reporter = %#v", ticket.ReporterID)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+ticket.ID+"/move", map[string]any{"status_id": statuses[1].ID, "index": 0}, nil); code != http.StatusOK {
		t.Fatalf("move status = %d", code)
	}
	var users []string
	rows, err := database.Query("SELECT user_id FROM ticket_status_transition WHERE ticket_id=? ORDER BY rowid", ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			t.Fatal(err)
		}
		users = append(users, userID)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(users, []string{"ai", "ai"}) {
		t.Fatalf("transition users = %#v", users)
	}
}

func TestAIPersonaInTicketAuditResponses(t *testing.T) {
	_, handler := newAPIAs(t, "ai")
	b := createBoard(t, handler, "PERSONA", []string{"Todo", "Done"})
	statuses := boardStatuses(t, handler, b.ID)
	ticket := createTicket(t, handler, b.ID, statuses[0].ID)
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+ticket.ID+"/move", map[string]any{"status_id": statuses[1].ID, "index": 0}, nil); code != http.StatusOK {
		t.Fatalf("move status = %d", code)
	}
	var added struct {
		UserName string `json:"user_name"`
		UserIsAI bool   `json:"user_is_ai"`
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/tickets/"+ticket.ID+"/comments", map[string]string{"content": "AI comment"}, &added); code != http.StatusCreated {
		t.Fatalf("add comment status = %d", code)
	}
	if added.UserName != "Martin" || !added.UserIsAI {
		t.Fatalf("added comment actor = %#v", added)
	}
	for _, endpoint := range []string{"history", "transitions", "comments"} {
		t.Run(endpoint, func(t *testing.T) {
			var actors []struct {
				UserName string `json:"user_name"`
				UserIsAI bool   `json:"user_is_ai"`
			}
			if code := requestJSON(t, handler, http.MethodGet, "/api/tickets/"+ticket.ID+"/"+endpoint, nil, &actors); code != http.StatusOK {
				t.Fatalf("%s status = %d", endpoint, code)
			}
			if len(actors) == 0 {
				t.Fatalf("%s returned no audit entries", endpoint)
			}
			for _, actor := range actors {
				if actor.UserName != "Martin" || !actor.UserIsAI {
					t.Errorf("%s actor = %#v", endpoint, actor)
				}
			}
		})
	}
	var users []struct {
		Name string `json:"name"`
		IsAI bool   `json:"is_ai"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/users", nil, &users); code != http.StatusOK {
		t.Fatalf("users status = %d", code)
	}
	foundMartin := false
	for _, user := range users {
		if user.Name == "Martin" && user.IsAI {
			foundMartin = true
		}
	}
	if !foundMartin {
		t.Fatalf("Martin AI user missing: %#v", users)
	}
}

func TestSearchTickets(t *testing.T) {
	_, handler := newAPI(t)
	a := createBoard(t, handler, "SEA", []string{"Todo"})
	b := createBoard(t, handler, "OTH", []string{"Todo"})
	var first, second testTicket
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+a.ID+"/tickets", map[string]any{"title": "Partial Match"}, &first); code != http.StatusCreated {
		t.Fatalf("first ticket status = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+b.ID+"/tickets", map[string]any{"title": "Other"}, &second); code != http.StatusCreated {
		t.Fatalf("second ticket status = %d", code)
	}
	for _, test := range []struct {
		path string
		want int
	}{{"/api/tickets/search?q=partial", 1}, {"/api/tickets/search?q=" + first.Code, 1}, {"/api/tickets/search?q=match&board_id=" + b.ID, 0}} {
		var got []testTicket
		if code := requestJSON(t, handler, http.MethodGet, test.path, nil, &got); code != http.StatusOK || len(got) != test.want {
			t.Fatalf("search %q = status %d, %#v", test.path, code, got)
		}
	}
	var result []testTicket
	if code := requestJSON(t, handler, http.MethodGet, "/api/tickets/search?q=partial", nil, &result); code != http.StatusOK || len(result) != 1 {
		t.Fatalf("search result status = %d, %#v", code, result)
	}
	if result[0].BoardName != "SEA" || result[0].StatusName != "Todo" {
		t.Fatalf("search result board/status = %q/%q", result[0].BoardName, result[0].StatusName)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/tickets/search?q=%20", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("empty search status = %d", code)
	}
}

func TestSameColumnMoveOnlyReorders(t *testing.T) {
	database, handler := newAPI(t)
	b := createBoard(t, handler, "SAME", []string{"Todo"})
	statusID := boardStatuses(t, handler, b.ID)[0].ID
	first := createTicket(t, handler, b.ID, statusID)
	second := createTicket(t, handler, b.ID, statusID)
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+first.ID+"/move", map[string]any{"status_id": statusID, "index": 1}, nil); code != http.StatusOK {
		t.Fatalf("move status = %d", code)
	}
	var transitions int
	if err := database.QueryRow("SELECT COUNT(*) FROM ticket_status_transition WHERE ticket_id=?", first.ID).Scan(&transitions); err != nil {
		t.Fatal(err)
	}
	if transitions != 1 {
		t.Fatalf("same-column transitions = %d, want creation only", transitions)
	}
	assertOrder(t, database, statusID, []string{second.ID, first.ID})
}

func TestStatusOrderRejectsForeignOrMissingIDs(t *testing.T) {
	_, handler := newAPI(t)
	a := createBoard(t, handler, "ORD", []string{"Todo"})
	b := createBoard(t, handler, "EXT", []string{"Todo"})
	foreign := boardStatuses(t, handler, b.ID)[0].ID
	tests := []struct {
		name string
		ids  []string
	}{{"foreign", []string{foreign}}, {"missing", []string{"missing"}}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if code := requestJSON(t, handler, http.MethodPut, "/api/boards/"+a.ID+"/status-order", map[string]any{"status_ids": test.ids}, nil); code != http.StatusBadRequest {
				t.Fatalf("status = %d", code)
			}
		})
	}
}

func TestShortNameChangeKeepsOldCodeLookup(t *testing.T) {
	_, handler := newAPI(t)
	b := createBoard(t, handler, "OLD", []string{"Todo"})
	old := createTicket(t, handler, b.ID, "")
	if code := requestJSON(t, handler, http.MethodPut, "/api/boards/"+b.ID, map[string]any{"name": "Renamed", "short_name": "NEW", "template": ""}, nil); code != http.StatusOK {
		t.Fatalf("update board status = %d", code)
	}
	var got testTicket
	if code := requestJSON(t, handler, http.MethodGet, "/api/tickets/"+old.Code, nil, &got); code != http.StatusOK {
		t.Fatalf("old code status = %d", code)
	}
	if got.ID != old.ID || got.Code != "NEW-1" {
		t.Fatalf("lookup = %#v", got)
	}
}

func TestInvalidShortNameReturnsBadRequest(t *testing.T) {
	_, handler := newAPI(t)
	for _, short := range []string{"", "bad-name", "12345678901"} {
		t.Run(fmt.Sprintf("%q", short), func(t *testing.T) {
			if code := requestJSON(t, handler, http.MethodPost, "/api/boards", map[string]any{"name": "Bad", "short_name": short}, nil); code != http.StatusBadRequest {
				t.Fatalf("status = %d", code)
			}
		})
	}
}

func TestCalendarRangeBoardFilterAndSoftDelete(t *testing.T) {
	database, handler := newAPI(t)
	a := createBoard(t, handler, "CAL", []string{"Todo"})
	b := createBoard(t, handler, "OTH", []string{"Todo"})
	ticketA := createTicket(t, handler, a.ID, "")
	ticketB := createTicket(t, handler, b.ID, "")
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+ticketA.ID, map[string]any{"due_date": "2026-04-12"}, nil); code != http.StatusOK {
		t.Fatalf("set due date status = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+ticketB.ID, map[string]any{"due_date": "2026-04-13"}, nil); code != http.StatusOK {
		t.Fatalf("set other due date status = %d", code)
	}
	var within, outside, linked, boardItem testCalendarItem
	for _, item := range []struct {
		out  *testCalendarItem
		body map[string]any
	}{{&within, map[string]any{"kind": "event", "title": "In range", "starts_at": "2026-04-12", "all_day": true, "board_id": a.ID}}, {&outside, map[string]any{"kind": "event", "title": "Outside", "starts_at": "2026-04-14", "all_day": true, "board_id": a.ID}}, {&linked, map[string]any{"kind": "reminder", "title": "Ticket link", "starts_at": "2026-04-12T12:00:00-05:00", "ticket_id": ticketA.ID}}, {&boardItem, map[string]any{"kind": "event", "title": "Board item", "starts_at": "2026-04-10", "ends_at": "2026-04-12", "all_day": true, "board_id": a.ID}}} {
		if code := requestJSON(t, handler, http.MethodPost, "/api/calendar-items", item.body, item.out); code != http.StatusCreated {
			t.Fatalf("create calendar item status = %d", code)
		}
	}
	if linked.BoardID == nil || *linked.BoardID != a.ID || linked.CreatedBy != "local" || linked.CreatedByIsAI {
		t.Fatalf("linked item actor/board = %#v", linked)
	}
	if _, err := database.Exec(`UPDATE calendar_item SET board_id=NULL WHERE id=?`, linked.ID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/calendar-items/"+within.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete calendar item status = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/calendar-items/"+within.ID, nil, nil); code != http.StatusNotFound {
		t.Fatalf("delete already deleted status = %d", code)
	}
	for _, test := range []struct {
		path        string
		itemCount   int
		ticketCount int
	}{{"/api/calendar?from=2026-04-12&to=2026-04-12", 2, 1}, {"/api/calendar?from=2026-04-12&to=2026-04-12&board_id=" + a.ID, 2, 1}, {"/api/calendar?from=2026-04-12&to=2026-04-12&board_id=" + b.ID, 0, 0}} {
		var got testCalendarResponse
		if code := requestJSON(t, handler, http.MethodGet, test.path, nil, &got); code != http.StatusOK {
			t.Fatalf("calendar %q status = %d", test.path, code)
		}
		if len(got.Items) != test.itemCount || len(got.Tickets) != test.ticketCount {
			t.Errorf("calendar %q counts = %d items, %d tickets; want %d, %d", test.path, len(got.Items), len(got.Tickets), test.itemCount, test.ticketCount)
		}
		if test.path == "/api/calendar?from=2026-04-12&to=2026-04-12&board_id="+a.ID && !containsCalendarItem(got.Items, linked.ID) {
			t.Errorf("board filter missed ticket-linked item: %#v", got.Items)
		}
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/boards/"+a.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete board status = %d", code)
	}
	var got testCalendarResponse
	requestJSON(t, handler, http.MethodGet, "/api/calendar?from=2026-04-12&to=2026-04-12", nil, &got)
	if len(got.Items) != 1 || got.Items[0].ID != linked.ID || len(got.Tickets) != 0 {
		t.Fatalf("after board delete = %#v", got)
	}
}

func containsCalendarItem(items []testCalendarItem, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func TestCalendarItemPartialUpdateAndAIActor(t *testing.T) {
	_, handler := newAPIAs(t, "ai")
	var created testCalendarItem
	body := map[string]any{"kind": "event", "title": "Before", "starts_at": "2026-04-12", "all_day": true}
	if code := requestJSON(t, handler, http.MethodPost, "/api/calendar-items", body, &created); code != http.StatusCreated {
		t.Fatalf("create status = %d", code)
	}
	if created.CreatedBy != "ai" || !created.CreatedByIsAI {
		t.Fatalf("created actor = %#v", created)
	}
	var updated testCalendarItem
	if code := requestJSON(t, handler, http.MethodPut, "/api/calendar-items/"+created.ID, map[string]any{"title": "After"}, &updated); code != http.StatusOK {
		t.Fatalf("update status = %d", code)
	}
	if updated.Title != "After" || updated.Kind != "event" || updated.StartsAt != "2026-04-12" {
		t.Fatalf("partial update = %#v", updated)
	}
}

func TestCalendarItemValidation(t *testing.T) {
	_, handler := newAPI(t)
	for _, test := range []struct {
		name string
		body map[string]any
	}{{"bad kind", map[string]any{"kind": "other", "title": "Event", "starts_at": "2026-04-12", "all_day": true}}, {"empty title", map[string]any{"kind": "event", "title": "  ", "starts_at": "2026-04-12", "all_day": true}}, {"all-day timestamp", map[string]any{"kind": "event", "title": "Event", "starts_at": "2026-04-12T10:00:00Z", "all_day": true}}, {"timed date-only", map[string]any{"kind": "event", "title": "Event", "starts_at": "2026-04-12"}}, {"ends before starts", map[string]any{"kind": "event", "title": "Event", "starts_at": "2026-04-12T10:00:00Z", "ends_at": "2026-04-12T09:00:00Z"}}} {
		t.Run(test.name, func(t *testing.T) {
			if code := requestJSON(t, handler, http.MethodPost, "/api/calendar-items", test.body, nil); code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", code)
			}
		})
	}
}

func TestDueDateValidationOnTicketCreateAndUpdate(t *testing.T) {
	_, handler := newAPI(t)
	b := createBoard(t, handler, "DUE", []string{"Todo"})
	for _, dueDate := range []string{"2026-02-30", "2026-02-01T00:00:00Z"} {
		if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+b.ID+"/tickets", map[string]any{"title": "Bad due", "due_date": dueDate}, nil); code != http.StatusBadRequest {
			t.Errorf("create due_date %q status = %d", dueDate, code)
		}
	}
	ticket := createTicket(t, handler, b.ID, "")
	for _, dueDate := range []string{"2026-02-30", "2026-02-01T00:00:00Z"} {
		if code := requestJSON(t, handler, http.MethodPut, "/api/tickets/"+ticket.ID, map[string]any{"due_date": dueDate}, nil); code != http.StatusBadRequest {
			t.Errorf("update due_date %q status = %d", dueDate, code)
		}
	}
}

func TestDeleteStatusWithTicketsConflicts(t *testing.T) {
	_, handler := newAPI(t)
	b := createBoard(t, handler, "DEL", []string{"Todo"})
	statusID := boardStatuses(t, handler, b.ID)[0].ID
	createTicket(t, handler, b.ID, statusID)
	if code := requestJSON(t, handler, http.MethodDelete, "/api/statuses/"+statusID, nil, nil); code != http.StatusConflict {
		t.Fatalf("status = %d", code)
	}
}

func TestBoardDataRejectsCorruptTicketOrder(t *testing.T) {
	database, handler := newAPI(t)
	b := createBoard(t, handler, "CORRUPT", []string{"Todo"})
	statusID := boardStatuses(t, handler, b.ID)[0].ID
	if _, err := database.Exec("UPDATE ticket_order SET ticket_ids=? WHERE status_id=?", "not json", statusID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/boards/"+b.ID+"/data", nil, nil); code != http.StatusInternalServerError {
		t.Fatalf("status = %d", code)
	}
}

func TestBoardDataRejectsCorruptTemporaryStatusTimestamp(t *testing.T) {
	database, handler := newAPI(t)
	b := createBoard(t, handler, "TEMP", []string{"Todo"})
	var status testStatus
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+b.ID+"/statuses", map[string]any{"name": "Temporary", "type": "temporary"}, &status); code != http.StatusCreated {
		t.Fatalf("create temporary status = %d", code)
	}
	ticket := createTicket(t, handler, b.ID, status.ID)
	if _, err := database.Exec("UPDATE ticket SET status_updated_at=? WHERE id=?", "not a timestamp", ticket.ID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/boards/"+b.ID+"/data", nil, nil); code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", code)
	}
}

func TestBoardDataTemporaryTTLAndOrdering(t *testing.T) {
	database, handler := newAPI(t)
	b := createBoard(t, handler, "TTL", []string{"Classic"})
	classicStatus := boardStatuses(t, handler, b.ID)[0].ID
	var temporaryStatus testStatus
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+b.ID+"/statuses", map[string]any{"name": "Temporary", "type": "temporary"}, &temporaryStatus); code != http.StatusCreated {
		t.Fatalf("create temporary status = %d", code)
	}
	create := func(title, statusID string) testTicket {
		t.Helper()
		var out testTicket
		if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+b.ID+"/tickets", map[string]any{"title": title, "status_id": statusID}, &out); code != http.StatusCreated {
			t.Fatalf("create ticket %q = %d", title, code)
		}
		return out
	}
	classicFirst := create("Classic First", classicStatus)
	classicSecond := create("Classic Second", classicStatus)
	temporaryOld := create("Temporary Old", temporaryStatus.ID)
	temporaryAlpha := create("Fresh Alpha", temporaryStatus.ID)
	temporaryBeta := create("Fresh Beta", temporaryStatus.ID)
	for _, ticket := range []testTicket{classicFirst, classicSecond, temporaryOld} {
		if _, err := database.Exec("UPDATE ticket SET status_updated_at=? WHERE id=?", "2020-01-01T00:00:00.000Z", ticket.ID); err != nil {
			t.Fatal(err)
		}
	}
	for statusID, order := range map[string][]string{
		classicStatus:      {classicSecond.ID, classicFirst.ID},
		temporaryStatus.ID: {temporaryBeta.ID, temporaryOld.ID, temporaryAlpha.ID},
	} {
		if _, err := database.Exec("UPDATE ticket_order SET ticket_ids=? WHERE status_id=?", listJSON(order), statusID); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name      string
		query     string
		classic   []string
		temporary []string
	}{
		{name: "ttl and ticket order", classic: []string{classicSecond.ID, classicFirst.ID}, temporary: []string{temporaryBeta.ID, temporaryAlpha.ID}},
		{name: "q title filter", query: "ALPHA", classic: []string{}, temporary: []string{temporaryAlpha.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var data testData
			path := "/api/boards/" + b.ID + "/data"
			if tc.query != "" {
				path += "?q=" + tc.query
			}
			if code := requestJSON(t, handler, http.MethodGet, path, nil, &data); code != http.StatusOK {
				t.Fatalf("board data status = %d", code)
			}
			if len(data.Statuses) != 2 {
				t.Fatalf("statuses = %d, want 2", len(data.Statuses))
			}
			for i, want := range [][]string{tc.classic, tc.temporary} {
				got := make([]string, len(data.Statuses[i].Tickets))
				for j, ticket := range data.Statuses[i].Tickets {
					got[j] = ticket.ID
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("status %d ticket order = %v, want %v", i, got, want)
				}
			}
		})
	}
}

func TestTemporaryStatusHideAfterDays(t *testing.T) {
	database, handler := newAPI(t)
	b := createBoard(t, handler, "DAYS", []string{"Todo"})
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+b.ID+"/statuses", map[string]any{"name": "Bad", "type": "temporary", "hide_after_days": 0}, nil); code != http.StatusBadRequest {
		t.Fatalf("create hide_after_days=0 status = %d", code)
	}
	var status testStatus
	if code := requestJSON(t, handler, http.MethodPost, "/api/boards/"+b.ID+"/statuses", map[string]any{"name": "Temporary", "type": "temporary"}, &status); code != http.StatusCreated {
		t.Fatalf("create temporary status = %d", code)
	}
	if status.HideAfterDays != 1 {
		t.Fatalf("created hide_after_days = %d, want 1", status.HideAfterDays)
	}
	if _, err := database.Exec(`UPDATE status SET additional_fields=? WHERE id=?`, `{"custom":"kept"}`, status.ID); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"name": "Temporary", "type": "temporary", "hide_after_days": 0}
	if code := requestJSON(t, handler, http.MethodPut, "/api/statuses/"+status.ID, body, nil); code != http.StatusBadRequest {
		t.Fatalf("update hide_after_days=0 status = %d", code)
	}
	body["hide_after_days"] = 3
	if code := requestJSON(t, handler, http.MethodPut, "/api/statuses/"+status.ID, body, &status); code != http.StatusOK {
		t.Fatalf("update temporary status = %d", code)
	}
	if status.HideAfterDays != 3 {
		t.Fatalf("updated hide_after_days = %d, want 3", status.HideAfterDays)
	}
	var fields map[string]json.RawMessage
	var raw string
	if err := database.QueryRow(`SELECT additional_fields FROM status WHERE id=?`, status.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || string(fields["custom"]) != `"kept"` {
		t.Fatalf("preserved status fields = %v, err = %v", fields, err)
	}
	old := createTicket(t, handler, b.ID, status.ID)
	fresh := createTicket(t, handler, b.ID, status.ID)
	for ticket, age := range map[string]int{old.ID: 4, fresh.ID: 2} {
		if _, err := database.Exec(`UPDATE ticket SET status_updated_at=? WHERE id=?`, time.Now().UTC().AddDate(0, 0, -age).Format("2006-01-02T15:04:05.000Z"), ticket); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		id   string
		want bool
	}{{"aged 4 days hidden", old.ID, false}, {"aged 2 days shown", fresh.ID, true}} {
		t.Run(tc.name, func(t *testing.T) {
			var data testData
			if code := requestJSON(t, handler, http.MethodGet, "/api/boards/"+b.ID+"/data", nil, &data); code != http.StatusOK {
				t.Fatalf("board data status = %d", code)
			}
			if len(data.Statuses) != 2 || data.Statuses[1].HideAfterDays != 3 {
				t.Fatalf("temporary status data = %#v", data.Statuses)
			}
			found := false
			for _, ticket := range data.Statuses[1].Tickets {
				found = found || ticket.ID == tc.id
			}
			if found != tc.want {
				t.Errorf("ticket %s visible = %t, want %t", tc.id, found, tc.want)
			}
		})
	}
	if _, err := database.Exec(`UPDATE status SET additional_fields=? WHERE id=?`, "not json", status.ID); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/boards/"+b.ID+"/data", nil, nil); code != http.StatusInternalServerError {
		t.Fatalf("corrupt additional_fields board data status = %d, want 500", code)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/statuses/"+status.ID, map[string]any{"name": "Temporary", "type": "temporary", "hide_after_days": 3}, nil); code != http.StatusInternalServerError {
		t.Fatalf("corrupt additional_fields update status = %d, want 500", code)
	}
}

func TestCreateBoardTemporaryStatuses(t *testing.T) {
	_, handler := newAPI(t)
	for _, tc := range []struct {
		name     string
		statuses []any
		wantCode int
	}{{"mixed", []any{"To Do", map[string]any{"name": "Done", "type": "temporary", "hide_after_days": 2}}, http.StatusCreated}, {"bad type", []any{map[string]any{"name": "Done", "type": "other"}}, http.StatusBadRequest}, {"zero days", []any{map[string]any{"name": "Done", "hide_after_days": 0}}, http.StatusBadRequest}} {
		t.Run(tc.name, func(t *testing.T) {
			var b testBoard
			body := map[string]any{"name": tc.name, "short_name": tc.name, "statuses": tc.statuses}
			if code := requestJSON(t, handler, http.MethodPost, "/api/boards", body, &b); code != tc.wantCode {
				t.Fatalf("create board status = %d, want %d", code, tc.wantCode)
			}
			if tc.wantCode != http.StatusCreated {
				return
			}
			var data struct {
				Statuses []struct {
					Name, Type    string
					HideAfterDays int `json:"hide_after_days"`
				} `json:"statuses"`
			}
			if code := requestJSON(t, handler, http.MethodGet, "/api/boards/"+b.ID+"/data", nil, &data); code != http.StatusOK {
				t.Fatalf("board data status = %d", code)
			}
			if len(data.Statuses) != 2 || data.Statuses[0].Name != "To Do" || data.Statuses[0].Type != "classic" || data.Statuses[1].Name != "Done" || data.Statuses[1].Type != "temporary" || data.Statuses[1].HideAfterDays != 2 {
				t.Fatalf("created statuses = %#v", data.Statuses)
			}
		})
	}
}

func TestCreateBoardDefaultStatuses(t *testing.T) {
	_, handler := newAPI(t)
	b := createBoard(t, handler, "DEFAULT", nil)
	var data struct {
		Statuses []struct {
			Name          string `json:"name"`
			Type          string `json:"type"`
			HideAfterDays int    `json:"hide_after_days"`
		} `json:"statuses"`
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/boards/"+b.ID+"/data", nil, &data); code != http.StatusOK {
		t.Fatalf("board data status = %d", code)
	}
	want := []struct {
		name, typ string
		days      int
	}{{"To Do", "classic", 1}, {"In Progress", "classic", 1}, {"Done", "temporary", 1}}
	if len(data.Statuses) != len(want) {
		t.Fatalf("default statuses = %#v", data.Statuses)
	}
	for i, status := range data.Statuses {
		if status.Name != want[i].name || status.Type != want[i].typ || status.HideAfterDays != want[i].days {
			t.Errorf("status %d = %#v, want %#v", i, status, want[i])
		}
	}
}

func assertOrder(t *testing.T, database *sql.DB, statusID string, want []string) {
	t.Helper()
	var raw string
	if err := database.QueryRow("SELECT ticket_ids FROM ticket_order WHERE status_id=?", statusID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %#v, want %#v", got, want)
	}
}
