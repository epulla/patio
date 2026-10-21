package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"
	"patio/internal/db"
)

type fakeOAuthServer struct {
	mu                    sync.Mutex
	issuer                string
	resource              string
	challenge             string
	state                 string
	authorizationResource string
	challengeMethod       string
	badState              bool
	failRefresh           bool
	registered            bool
	verifiedPKCE          bool
	refreshCount          int
	resourceInCode        bool
	resourceInRefresh     bool
	authorizationClientID string
}

func (f *fakeOAuthServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/oauth-authorization-server":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.issuer, "authorization_endpoint": f.issuer + "/authorize",
			"token_endpoint": f.issuer + "/token", "registration_endpoint": f.issuer + "/register",
			"jwks_uri": f.issuer + "/jwks", "response_types_supported": []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
			"code_challenge_methods_supported":      []string{"S256"}, "scopes_supported": []string{"offline_access"},
		})
	case "/register":
		var registration struct {
			ClientName   string   `json:"client_name"`
			RedirectURIs []string `json:"redirect_uris"`
		}
		if json.NewDecoder(r.Body).Decode(&registration) != nil || registration.ClientName != "patio" || len(registration.RedirectURIs) != 1 {
			http.Error(w, "invalid client registration", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.registered = true
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"client_id":"test-client","client_secret":"test-secret","token_endpoint_auth_method":"client_secret_post"}`)
	case "/authorize":
		query := r.URL.Query()
		f.mu.Lock()
		f.challenge, f.state = query.Get("code_challenge"), query.Get("state")
		f.authorizationResource, f.challengeMethod = query.Get("resource"), query.Get("code_challenge_method")
		f.authorizationClientID = query.Get("client_id")
		badState := f.badState
		f.mu.Unlock()
		if query.Get("code_challenge_method") != "S256" || query.Get("resource") != f.resource {
			http.Error(w, "missing PKCE or resource", http.StatusBadRequest)
			return
		}
		redirect, err := url.Parse(query.Get("redirect_uri"))
		if err != nil {
			http.Error(w, "invalid redirect", http.StatusBadRequest)
			return
		}
		values := redirect.Query()
		values.Set("code", "authorization-code")
		state := query.Get("state")
		if badState {
			state += "-mismatch"
		}
		values.Set("state", state)
		redirect.RawQuery = values.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
	case "/token":
		if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != "test-client" || r.Form.Get("client_secret") != "test-secret" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		if r.Form.Get("grant_type") == "authorization_code" {
			verifier := r.Form.Get("code_verifier")
			digest := sha256.Sum256([]byte(verifier))
			f.mu.Lock()
			f.verifiedPKCE = base64.RawURLEncoding.EncodeToString(digest[:]) == f.challenge && r.Form.Get("code") == "authorization-code"
			f.resourceInCode = r.Form.Get("resource") == f.resource
			valid := f.verifiedPKCE && f.resourceInCode
			f.mu.Unlock()
			if !valid {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			writeFakeToken(w, "access-1", "refresh-1")
			return
		}
		if r.Form.Get("grant_type") == "refresh_token" {
			f.mu.Lock()
			f.refreshCount++
			f.resourceInRefresh = r.Form.Get("resource") == f.resource
			fail := f.failRefresh
			resourceOK := f.resourceInRefresh
			f.mu.Unlock()
			if fail || !resourceOK || (r.Form.Get("refresh_token") != "refresh-1" && r.Form.Get("refresh_token") != "refresh-2") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
				return
			}
			writeFakeToken(w, "access-2", "refresh-2")
			return
		}
		http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
	default:
		http.NotFound(w, r)
	}
}

func writeFakeToken(w http.ResponseWriter, access, refresh string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"access_token":%q,"refresh_token":%q,"token_type":"Bearer","expires_in":3600}`, access, refresh)
}

type oauthBrowserGate struct {
	mu      sync.Mutex
	blocked bool
	entered chan struct{}
	release chan struct{}
}

func (g *oauthBrowserGate) open(raw string) error {
	g.mu.Lock()
	blocked, entered, release := g.blocked, g.entered, g.release
	g.mu.Unlock()
	if blocked {
		close(entered)
		<-release
	}
	response, err := http.Get(raw)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.Request.URL.Path != "/callback" {
		return fmt.Errorf("authorization page returned %s: %s", response.Status, body)
	}
	return err
}

func TestMCPOAuthLoginAndRefresh(t *testing.T) {
	keyring.MockInit()
	state := &fakeOAuthServer{}
	authServer := httptest.NewServer(state)
	defer authServer.Close()
	state.issuer = authServer.URL

	sdkServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return sdkServer }, &mcp.StreamableHTTPOptions{Stateless: true})
	var resourceURL string
	protected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-protected-resource" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": resourceURL, "authorization_servers": []string{state.issuer}})
			return
		}
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer access-1" && r.Header.Get("Authorization") != "Bearer access-2" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+strings.TrimSuffix(resourceURL, "/mcp")+`/.well-known/oauth-protected-resource"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		streamable.ServeHTTP(w, r)
	}))
	defer protected.Close()
	resourceURL = protected.URL + "/mcp"
	state.resource = resourceURL

	root := t.TempDir()
	database, err := db.Open(filepath.Join(root, "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	gate := &oauthBrowserGate{}
	server := New(database, "local", false, "dev", root, Options{openOAuthURL: gate.open})

	createServer := func(name, auth string) mcpServer {
		t.Helper()
		var created mcpServer
		if code := requestJSON(t, server, http.MethodPost, "/api/mcp-servers", map[string]any{
			"name": name, "kind": "http", "url": resourceURL, "auth": auth,
		}, &created); code != http.StatusCreated {
			t.Fatalf("create MCP server status = %d", code)
		}
		return created
	}
	login := func(id string) (int, map[string]string) {
		t.Helper()
		var result map[string]string
		code := requestJSON(t, server, http.MethodPost, "/api/mcp-servers/"+id+"/login", nil, &result)
		return code, result
	}

	created := createServer("Protected OAuth", "oauth")
	code, response := login(created.ID)
	if code != http.StatusAccepted || response["auth_url"] == "" {
		t.Fatalf("login = %d %#v", code, response)
	}
	waitOAuthLogin(t, server, created.ID)
	state.mu.Lock()
	registered, pkce, resourceSent := state.registered, state.verifiedPKCE, state.resourceInCode
	authorizationResource, challengeMethod := state.authorizationResource, state.challengeMethod
	state.mu.Unlock()
	if !registered || !pkce || !resourceSent {
		t.Fatalf("registration/PKCE/resource = %v/%v/%v; authorization resource/method = %q/%q", registered, pkce, resourceSent, authorizationResource, challengeMethod)
	}
	state.mu.Lock()
	state.registered = false
	state.mu.Unlock()
	var preregistered mcpServer
	if code := requestJSON(t, server, http.MethodPost, "/api/mcp-servers", map[string]any{
		"name": "Pre-registered OAuth", "kind": "http", "url": resourceURL, "auth": "oauth",
		"oauth_client_id": "test-client", "oauth_client_secret": "test-secret", "oauth_scopes": []string{"offline_access"},
	}, &preregistered); code != http.StatusCreated {
		t.Fatalf("create preregistered server = %d", code)
	}
	if code, response := login(preregistered.ID); code != http.StatusAccepted || response["auth_url"] == "" {
		t.Fatalf("preregistered login = %d %#v", code, response)
	}
	waitOAuthLogin(t, server, preregistered.ID)
	state.mu.Lock()
	registered, clientID := state.registered, state.authorizationClientID
	state.mu.Unlock()
	if registered || clientID != "test-client" {
		t.Fatalf("pre-registered client registration/client_id = %v/%q", registered, clientID)
	}
	if code := requestJSON(t, server, http.MethodDelete, "/api/mcp-servers/"+preregistered.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete preregistered server = %d", code)
	}
	var listed []mcpServer
	if code := requestJSON(t, server, http.MethodGet, "/api/mcp-servers", nil, &listed); code != http.StatusOK || len(listed) != 1 || !listed[0].HasOAuthToken {
		t.Fatalf("OAuth token not listed: status=%d servers=%#v", code, listed)
	}
	credentialRaw, err := keyring.Get(connectorService, mcpServerSecret(created.ID, "oauth"))
	if err != nil {
		t.Fatal(err)
	}
	var credential mcpOAuthCredential
	if err := json.Unmarshal([]byte(credentialRaw), &credential); err != nil || credential.AccessToken != "access-1" || credential.RefreshToken != "refresh-1" || credential.TokenURL != authServer.URL+"/token" || credential.Resource != resourceURL {
		t.Fatalf("stored OAuth credentials = %#v, %v", credential, err)
	}
	if header, err := server.oauthHeader(t.Context(), created.ID); err != nil || header != "Bearer access-1" {
		t.Fatalf("initial OAuth header = %q, %v", header, err)
	}
	specs, err := server.enabledMCPServers()
	if err != nil || len(specs) != 1 || specs[0].OAuthHeader == nil || *specs[0].OAuthHeader != "Bearer access-1" {
		t.Fatalf("chat OAuth header hook = %#v, %v", specs, err)
	}
	credential.Expiry = time.Now().Add(-time.Minute)
	if err := storeMCPOAuthCredential(created.ID, credential); err != nil {
		t.Fatal(err)
	}
	if header, err := server.oauthHeader(t.Context(), created.ID); err != nil || header != "Bearer access-2" {
		t.Fatalf("refreshed OAuth header = %q, %v", header, err)
	}
	credentialRaw, err = keyring.Get(connectorService, mcpServerSecret(created.ID, "oauth"))
	if err != nil || json.Unmarshal([]byte(credentialRaw), &credential) != nil || credential.AccessToken != "access-2" || credential.RefreshToken != "refresh-2" {
		t.Fatalf("rotated OAuth credentials = %s, %v", credentialRaw, err)
	}
	state.mu.Lock()
	resourceInRefresh := state.resourceInRefresh
	state.mu.Unlock()
	if !resourceInRefresh {
		t.Fatal("refresh grant omitted resource parameter")
	}
	credential.Expiry = time.Now().Add(-time.Minute)
	if err := storeMCPOAuthCredential(created.ID, credential); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	state.failRefresh = true
	state.mu.Unlock()
	if _, err := server.oauthHeader(t.Context(), created.ID); err == nil || err.Error() != "Sign in again" {
		t.Fatalf("refresh failure = %v, want Sign in again", err)
	}
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	if specs, err := server.enabledMCPServers(); err != nil || len(specs) != 1 || specs[0].AuthError == nil {
		t.Fatalf("enabled specs after refresh failure = %#v, %v", specs, err)
	}
	if specs, err := server.chats.chatMCPServers(chatID); err != nil || len(specs) != 0 {
		t.Fatalf("chat MCP specs after refresh failure = %#v, %v", specs, err)
	}
	var role, content string
	if err := database.QueryRow(`SELECT role,content FROM chat_message WHERE chat_id=?`, chatID).Scan(&role, &content); err != nil || role != "error" || content != "MCP server protected-oauth skipped: sign in again" {
		t.Fatalf("refresh failure note = %q %q, %v", role, content, err)
	}

	state.mu.Lock()
	state.failRefresh, state.badState = false, true
	state.mu.Unlock()
	mismatch := createServer("Mismatched State", "oauth")
	if code, result := login(mismatch.ID); code != http.StatusAccepted || result["auth_url"] == "" {
		t.Fatalf("mismatched-state login start = %d %#v", code, result)
	}
	waitOAuthLogin(t, server, mismatch.ID)
	if _, err := keyring.Get(connectorService, mcpServerSecret(mismatch.ID, "oauth")); err == nil {
		t.Fatal("state mismatch stored OAuth token")
	}

	state.mu.Lock()
	state.badState = false
	state.mu.Unlock()
	plain := createServer("No OAuth", "none")
	if code, _ := login(plain.ID); code != http.StatusBadRequest {
		t.Fatalf("non-OAuth login status = %d", code)
	}

	concurrent := createServer("Concurrent OAuth", "oauth")
	gate.mu.Lock()
	gate.blocked = true
	gate.entered = make(chan struct{})
	gate.release = make(chan struct{})
	entered, release := gate.entered, gate.release
	gate.mu.Unlock()
	firstResponse := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPost, "/api/mcp-servers/"+concurrent.ID+"/login", nil)
	firstDone := make(chan struct{})
	go func() {
		server.ServeHTTP(firstResponse, firstRequest)
		close(firstDone)
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("browser opener was not reached")
	}
	if code := requestJSON(t, server, http.MethodPost, "/api/mcp-servers/"+concurrent.ID+"/login", nil, nil); code != http.StatusConflict {
		t.Fatalf("concurrent login status = %d", code)
	}
	gate.mu.Lock()
	gate.blocked = false
	gate.mu.Unlock()
	close(release)
	select {
	case <-firstDone:
	case <-time.After(10 * time.Second):
		t.Fatal("first login did not finish")
	}
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("first concurrent login = %d %s", firstResponse.Code, firstResponse.Body.String())
	}
	waitOAuthLogin(t, server, concurrent.ID)
}

func waitOAuthLogin(t *testing.T, server *Server, id string) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		server.oauthMu.Lock()
		pending := server.oauthLogins[id]
		server.oauthMu.Unlock()
		if !pending {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("OAuth login remained in progress")
		case <-ticker.C:
		}
	}
}

func TestMCPOAuthCredentialUsesSeparateKeychainItemWhenLarge(t *testing.T) {
	keyring.MockInit()
	id := "large-oauth"
	credential := mcpOAuthCredential{
		AccessToken: strings.Repeat("a", 850), RefreshToken: "refresh", ClientID: "client",
		ClientSecret: strings.Repeat("s", 1300), TokenURL: "https://issuer.test/token", Resource: "https://mcp.test/mcp",
		Expiry: time.Now().Add(time.Hour),
	}
	if err := storeMCPOAuthCredential(id, credential); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(connectorService, mcpServerSecret(id, "oauth-client")); err != nil {
		t.Fatalf("separate client registration not stored: %v", err)
	}
	s := &Server{}
	header, err := s.oauthHeader(t.Context(), id)
	if err != nil || header != "Bearer "+credential.AccessToken {
		t.Fatalf("header from split credential = %q, %v", header, err)
	}
}

func TestGoogleOAuthAuthorizationURLRequestsOfflineAccess(t *testing.T) {
	for _, tc := range []struct{ raw, access, prompt string }{
		{"https://accounts.google.com/o/oauth2/auth?state=s", "offline", "consent"},
		{"https://auth.example.test/authorize?state=s", "", ""},
	} {
		u, err := url.Parse(addGoogleOfflineParams(tc.raw))
		if err != nil || u.Query().Get("access_type") != tc.access || u.Query().Get("prompt") != tc.prompt {
			t.Fatalf("URL rewrite %q => %v, %v", tc.raw, u, err)
		}
	}
}
