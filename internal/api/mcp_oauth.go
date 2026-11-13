package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

const oauthSecretLimit = 2000

type mcpOAuthCredential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	TokenURL     string    `json:"token_endpoint"`
	Resource     string    `json:"resource"`
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
}

type mcpOAuthMarker struct {
	Chunks int    `json:"chunks"`
	Slot   string `json:"slot"`
}

var mcpOAuthKeyringMu sync.Mutex
var mcpKeyringGet, mcpKeyringSet, mcpKeyringDelete = keyring.Get, keyring.Set, keyring.Delete

type mcpOAuthClient struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

type oauthCallback struct {
	result *auth.AuthorizationResult
	err    error
}

type mcpOAuthLogin struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *Server) cancelMCPOAuthLogin(id string) {
	s.oauthMu.Lock()
	login := s.oauthLogins[id]
	if login != nil {
		login.cancel()
	}
	s.oauthMu.Unlock()
	if login != nil {
		<-login.done
	}
}

func (s *Server) mcpServerLogin(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	server, err := s.loadMCPServer(id)
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "MCP server not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if server.Kind != "http" || server.Auth != "oauth" {
		errw(w, http.StatusBadRequest, "OAuth login requires an HTTP server with oauth auth")
		return
	}

	s.oauthMu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	login := &mcpOAuthLogin{cancel: cancel, done: make(chan struct{})}
	if previous := s.oauthLogins[id]; previous != nil {
		previous.cancel()
	}
	delete(s.oauthErrors, id)
	s.oauthLogins[id] = login
	s.oauthMu.Unlock()

	started := make(chan string, 1)
	finished := make(chan error, 1)
	go func() {
		defer cancel()
		defer func() {
			s.oauthMu.Lock()
			if s.oauthLogins[id] == login {
				delete(s.oauthLogins, id)
			}
			s.oauthMu.Unlock()
			close(login.done)
		}()
		err := s.runMCPOAuthLogin(ctx, server, started)
		s.oauthMu.Lock()
		if s.oauthLogins[id] == login {
			if err != nil {
				log.Printf("mcp oauth login %s (%s) failed: %v", server.Name, id, err)
				s.oauthErrors[id] = err.Error()
			} else {
				delete(s.oauthErrors, id)
			}
		}
		s.oauthMu.Unlock()
		finished <- err
	}()

	select {
	case authURL := <-started:
		write(w, http.StatusAccepted, map[string]string{"auth_url": authURL})
	case err := <-finished:
		if err == nil {
			err = errors.New("OAuth login completed without opening authorization page")
		}
		errw(w, http.StatusBadRequest, err.Error())
	case <-ctx.Done():
		errw(w, http.StatusGatewayTimeout, "OAuth login timed out")
	}
}

func (s *Server) runMCPOAuthLogin(ctx context.Context, server mcpServer, started chan<- string) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for OAuth callback: %w", err)
	}
	defer listener.Close()
	redirectURL := oauthRedirectURI(listener)

	callback := make(chan oauthCallback, 1)
	var expectedState string
	var stateMu sync.RWMutex
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		result := oauthCallback{}
		state := r.URL.Query().Get("state")
		stateMu.RLock()
		validState := state != "" && state == expectedState
		stateMu.RUnlock()
		if !validState {
			result.err = errors.New("OAuth state mismatch")
		} else if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
			result.err = fmt.Errorf("authorization failed: %s", oauthErr)
		} else if code := r.URL.Query().Get("code"); code == "" {
			result.err = errors.New("authorization response missing code")
		} else {
			result.result = &auth.AuthorizationResult{Code: code, State: state, Iss: r.URL.Query().Get("iss")}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if result.err != nil {
			w.WriteHeader(http.StatusBadRequest)
		}
		_, _ = w.Write([]byte("<!doctype html><title>patio</title>You can close this window."))
		select {
		case callback <- result:
		default:
		}
	})
	callbackServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- callbackServer.Serve(listener) }()
	defer callbackServer.Close()

	var clientConfig mcpOAuthCredential
	client := &http.Client{Timeout: 15 * time.Second}
	handlerConfig := &auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName:    "patio",
			RedirectURIs:  []string{redirectURL},
			GrantTypes:    []string{"authorization_code", "refresh_token"},
			ResponseTypes: []string{"code"},
		}},
		RedirectURL:         redirectURL,
		RequestRefreshToken: true,
		Client:              client,
		AuthorizationCodeFetcher: func(fetchCtx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			parsed, err := url.Parse(args.URL)
			if err != nil || parsed.Query().Get("state") == "" {
				return nil, errors.New("authorization server returned invalid authorization URL")
			}
			args.URL = addGoogleOfflineParams(args.URL)
			parsed, _ = url.Parse(args.URL)
			stateMu.Lock()
			expectedState = parsed.Query().Get("state")
			stateMu.Unlock()
			if err := s.openOAuthURL(args.URL); err != nil {
				return nil, err
			}
			select {
			case started <- args.URL:
			default:
			}
			select {
			case result := <-callback:
				return result.result, result.err
			case <-fetchCtx.Done():
				return nil, fetchCtx.Err()
			case err := <-serveErr:
				if errors.Is(err, http.ErrServerClosed) {
					return nil, errors.New("OAuth callback server closed")
				}
				return nil, err
			}
		},
		NewTokenSource: func(tokenCtx context.Context, config *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
			clientConfig = mcpOAuthCredential{
				ClientID: config.ClientID, ClientSecret: config.ClientSecret,
				TokenURL: config.Endpoint.TokenURL, Resource: oauthResourceURL(server),
			}
			source := config.TokenSource(tokenCtx, token)
			return &persistedOAuthTokenSource{source: source, save: func(rotated *oauth2.Token) error {
				credential := clientConfig
				credential.AccessToken = rotated.AccessToken
				credential.RefreshToken = rotated.RefreshToken
				credential.TokenType = rotated.TokenType
				credential.Expiry = rotated.Expiry
				return storeMCPOAuthCredential(server.ID, credential)
			}}, nil
		},
	}
	if server.OAuthClientID != "" {
		handlerConfig.DynamicClientRegistrationConfig = nil
		credentials := &oauthex.ClientCredentials{ClientID: server.OAuthClientID}
		secret, err := keyring.Get(connectorService, mcpServerSecret(server.ID, "oauth-registered-secret"))
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("read OAuth client secret: %w", err)
		}
		if err == nil {
			credentials.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: secret}
		}
		handlerConfig.PreregisteredClient = credentials
		if len(server.OAuthScopes) > 0 {
			scopes := append([]string(nil), server.OAuthScopes...)
			handlerConfig.ScopeFilter = func([]string) []string { return scopes }
		}
	}
	authHandler, err := auth.NewAuthorizationCodeHandler(handlerConfig)
	if err != nil {
		return fmt.Errorf("create OAuth handler: %w", err)
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:             server.URL,
		HTTPClient:           client,
		OAuthHandler:         authHandler,
		DisableStandaloneSSE: true,
	}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "patio", Version: "1.0.0"}, nil)
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect to OAuth MCP server: %w", err)
	}
	// Some MCP servers allow unauthenticated initialization. Explicit login must
	// still start OAuth in that case; don't repeat it when Connect already did.
	tokenSource, err := authHandler.TokenSource(ctx)
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("get OAuth token source: %w", err)
	}
	var token *oauth2.Token
	if tokenSource != nil {
		token, err = tokenSource.Token()
		if err != nil {
			_ = session.Close()
			return fmt.Errorf("get OAuth token: %w", err)
		}
	}
	if token == nil || !token.Valid() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
		if err != nil {
			_ = session.Close()
			return fmt.Errorf("create OAuth authorization request: %w", err)
		}
		resp := &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}
		if err := authHandler.Authorize(ctx, req, resp); err != nil {
			_ = session.Close()
			return fmt.Errorf("authorize OAuth MCP server: %w", err)
		}
	}
	return session.Close()
}

func addGoogleOfflineParams(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "accounts.google.com") {
		return raw
	}
	q := u.Query()
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	u.RawQuery = q.Encode()
	return u.String()
}

type persistedOAuthTokenSource struct {
	source oauth2.TokenSource
	save   func(*oauth2.Token) error
}

type oauthResourceTransport struct {
	next     http.RoundTripper
	tokenURL string
	resource string
}

func (t oauthResourceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	endpoint, err := url.Parse(t.tokenURL)
	if err != nil || req.Method != http.MethodPost || req.URL.Scheme != endpoint.Scheme || req.URL.Host != endpoint.Host || req.URL.Path != endpoint.Path {
		return t.next.RoundTrip(req)
	}
	if req.Body == nil {
		return t.next.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	if values.Get("resource") == "" {
		values.Set("resource", t.resource)
	}
	clone := req.Clone(req.Context())
	encoded := values.Encode()
	clone.Body = io.NopCloser(strings.NewReader(encoded))
	clone.ContentLength = int64(len(encoded))
	clone.Header = req.Header.Clone()
	clone.Header.Set("Content-Length", fmt.Sprint(len(encoded)))
	return t.next.RoundTrip(clone)
}

func (s *persistedOAuthTokenSource) Token() (*oauth2.Token, error) {
	token, err := s.source.Token()
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, errors.New("OAuth token source returned no token")
	}
	if err := s.save(token); err != nil {
		return nil, err
	}
	return token, nil
}

func storeMCPOAuthCredential(id string, credential mcpOAuthCredential) error {
	mcpOAuthKeyringMu.Lock()
	defer mcpOAuthKeyringMu.Unlock()
	full, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	if err := storeMCPOAuthJSONLocked(id, full); err != nil {
		return err
	}
	if len(full) <= oauthSecretLimit {
		return deleteMCPOAuthClient(id)
	}
	return nil
}

func storeMCPOAuthJSONLocked(id string, full []byte) error {
	old := mcpOAuthMarker{}
	if raw, e := mcpKeyringGet(connectorService, mcpServerSecret(id, "oauth")); e == nil {
		_ = json.Unmarshal([]byte(raw), &old)
	} else if !errors.Is(e, keyring.ErrNotFound) {
		return e
	}
	if len(full) <= oauthSecretLimit {
		if err := mcpKeyringSet(connectorService, mcpServerSecret(id, "oauth"), string(full)); err != nil {
			return err
		}
		if old.Chunks > 0 {
			return deleteMCPOAuthSlot(id, old.Slot)
		}
		return nil
	}
	n := (len(full) + oauthSecretLimit - 1) / oauthSecretLimit
	if n > 8 {
		return errors.New("OAuth credentials exceed keychain item size limit")
	}
	slot := "a"
	if old.Slot == "a" {
		slot = "b"
	}
	written := false
	cleanup := func() {
		if written {
			_ = deleteMCPOAuthSlot(id, slot)
		}
	}
	for start, i := 0, 1; start < len(full); start, i = start+oauthSecretLimit, i+1 {
		end := min(start+oauthSecretLimit, len(full))
		if err := mcpKeyringSet(connectorService, mcpServerSecret(id, fmt.Sprintf("oauth-%s-%d", slot, i)), string(full[start:end])); err != nil {
			cleanup()
			return err
		}
		written = true
	}
	marker, _ := json.Marshal(mcpOAuthMarker{Chunks: n, Slot: slot})
	if err := mcpKeyringSet(connectorService, mcpServerSecret(id, "oauth"), string(marker)); err != nil {
		cleanup()
		return err
	}
	if old.Chunks > 0 {
		if err := deleteMCPOAuthSlot(id, old.Slot); err != nil {
			return err
		}
	}
	return nil
}

func deleteMCPOAuthSlot(id string, slots ...string) error {
	var first error
	for _, slot := range slots {
		for i := 1; i <= 8; i++ {
			err := mcpKeyringDelete(connectorService, mcpServerSecret(id, fmt.Sprintf("oauth-%s-%d", slot, i)))
			if err != nil && !errors.Is(err, keyring.ErrNotFound) && first == nil {
				first = err
			}
		}
	}
	return first
}

func deleteMCPOAuthClient(id string) error {
	err := mcpKeyringDelete(connectorService, mcpServerSecret(id, "oauth-client"))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func readMCPOAuthJSONLocked(id string) (string, error) {
	raw, err := mcpKeyringGet(connectorService, mcpServerSecret(id, "oauth"))
	if err != nil {
		return "", err
	}
	var marker mcpOAuthMarker
	if json.Unmarshal([]byte(raw), &marker) == nil && marker.Chunks > 0 {
		for attempt := 0; attempt < 2; attempt++ {
			var joined strings.Builder
			missing := false
			for i := 1; i <= marker.Chunks; i++ {
				chunk, e := mcpKeyringGet(connectorService, mcpServerSecret(id, fmt.Sprintf("oauth-%s-%d", marker.Slot, i)))
				if errors.Is(e, keyring.ErrNotFound) {
					missing = true
					break
				}
				if e != nil {
					return "", e
				}
				joined.WriteString(chunk)
			}
			if !missing {
				return joined.String(), nil
			}
			if attempt == 0 {
				raw, err = mcpKeyringGet(connectorService, mcpServerSecret(id, "oauth"))
				if err != nil {
					return "", errors.New("Sign in again")
				}
				_ = json.Unmarshal([]byte(raw), &marker)
			}
		}
		return "", errors.New("Sign in again")
	}
	return raw, nil
}

func loadMCPOAuthCredential(id string) (mcpOAuthCredential, error) {
	raw, err := readMCPOAuthJSONLocked(id)
	if errors.Is(err, keyring.ErrNotFound) {
		return mcpOAuthCredential{}, errors.New("Sign in again")
	}
	if err != nil {
		return mcpOAuthCredential{}, err
	}
	var credential mcpOAuthCredential
	if err := json.Unmarshal([]byte(raw), &credential); err != nil {
		return credential, fmt.Errorf("decode OAuth credentials: %w", err)
	}
	if credential.ClientID == "" {
		clientRaw, e := mcpKeyringGet(connectorService, mcpServerSecret(id, "oauth-client"))
		if errors.Is(e, keyring.ErrNotFound) {
			return credential, errors.New("Sign in again")
		}
		if e != nil {
			return credential, e
		}
		var client mcpOAuthClient
		if e = json.Unmarshal([]byte(clientRaw), &client); e != nil {
			return credential, fmt.Errorf("decode OAuth client registration: %w", e)
		}
		credential.ClientID, credential.ClientSecret = client.ClientID, client.ClientSecret
	}
	return credential, nil
}

func (s *Server) oauthHeader(ctx context.Context, serverID string) (string, error) {
	mcpOAuthKeyringMu.Lock()
	credential, err := loadMCPOAuthCredential(serverID)
	mcpOAuthKeyringMu.Unlock()
	if err != nil {
		return "", err
	}
	if credential.AccessToken == "" {
		return "", errors.New("Sign in again")
	}
	token := &oauth2.Token{
		AccessToken: credential.AccessToken, RefreshToken: credential.RefreshToken,
		TokenType: credential.TokenType, Expiry: credential.Expiry,
	}
	if !token.Expiry.IsZero() && !token.Expiry.After(time.Now()) && token.RefreshToken == "" {
		return "", errors.New("Sign in again")
	}
	config := &oauth2.Config{
		ClientID: credential.ClientID, ClientSecret: credential.ClientSecret,
		Endpoint: oauth2.Endpoint{TokenURL: credential.TokenURL},
	}
	refreshCtx := context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: 15 * time.Second, Transport: oauthResourceTransport{
		next: http.DefaultTransport, tokenURL: credential.TokenURL, resource: credential.Resource,
	}})
	refreshed, err := config.TokenSource(refreshCtx, token).Token()
	if err != nil {
		if token.RefreshToken != "" || !token.Expiry.IsZero() && !token.Expiry.After(time.Now()) {
			return "", errors.New("Sign in again")
		}
		return "", err
	}
	if refreshed.AccessToken != token.AccessToken {
		mcpOAuthKeyringMu.Lock()
		current, e := loadMCPOAuthCredential(serverID)
		if e != nil {
			mcpOAuthKeyringMu.Unlock()
			return "", errors.New("Sign in again")
		}
		if current.AccessToken != token.AccessToken {
			mcpOAuthKeyringMu.Unlock()
			return "Bearer " + refreshed.AccessToken, nil
		}
		credential.AccessToken = refreshed.AccessToken
		credential.RefreshToken = refreshed.RefreshToken
		credential.TokenType = refreshed.TokenType
		credential.Expiry = refreshed.Expiry
		full, e := json.Marshal(credential)
		if e == nil {
			e = storeMCPOAuthJSONLocked(serverID, full)
		}
		mcpOAuthKeyringMu.Unlock()
		if e != nil {
			return "", e
		}
	}
	return "Bearer " + refreshed.AccessToken, nil
}

func (s *Server) oauthHeaderIfPresent(ctx context.Context, server mcpServer) (*string, error) {
	if !server.HasOAuthToken {
		return nil, nil
	}
	header, err := s.oauthHeader(ctx, server.ID)
	if err != nil {
		return nil, err
	}
	return &header, nil
}

func oauthRedirectURI(listener net.Listener) string {
	return "http://" + listener.Addr().String() + "/callback"
}

func oauthResourceURL(server mcpServer) string {
	return server.URL
}
