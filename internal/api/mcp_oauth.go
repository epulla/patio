package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type mcpOAuthClient struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

type oauthCallback struct {
	result *auth.AuthorizationResult
	err    error
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
	if s.oauthLogins[id] {
		s.oauthMu.Unlock()
		errw(w, http.StatusConflict, "OAuth login already in progress")
		return
	}
	s.oauthLogins[id] = true
	s.oauthMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	started := make(chan string, 1)
	finished := make(chan error, 1)
	go func() {
		defer cancel()
		defer func() {
			s.oauthMu.Lock()
			delete(s.oauthLogins, id)
			s.oauthMu.Unlock()
		}()
		finished <- s.runMCPOAuthLogin(ctx, server, started)
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
	full, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	if len(full) <= oauthSecretLimit {
		if err := keyring.Set(connectorService, mcpServerSecret(id, "oauth"), string(full)); err != nil {
			return err
		}
		return deleteMCPOAuthClient(id)
	}

	client := mcpOAuthClient{ClientID: credential.ClientID, ClientSecret: credential.ClientSecret}
	clientJSON, err := json.Marshal(client)
	if err != nil {
		return err
	}
	credential.ClientID, credential.ClientSecret = "", ""
	tokenJSON, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	if len(tokenJSON) > oauthSecretLimit || len(clientJSON) > oauthSecretLimit {
		return errors.New("OAuth credentials exceed keychain item size limit")
	}
	if err := keyring.Set(connectorService, mcpServerSecret(id, "oauth-client"), string(clientJSON)); err != nil {
		return err
	}
	if err := keyring.Set(connectorService, mcpServerSecret(id, "oauth"), string(tokenJSON)); err != nil {
		return err
	}
	return nil
}

func deleteMCPOAuthClient(id string) error {
	err := keyring.Delete(connectorService, mcpServerSecret(id, "oauth-client"))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func (s *Server) oauthHeader(ctx context.Context, serverID string) (string, error) {
	raw, err := keyring.Get(connectorService, mcpServerSecret(serverID, "oauth"))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", errors.New("Sign in again")
	}
	if err != nil {
		return "", err
	}
	var credential mcpOAuthCredential
	if err := json.Unmarshal([]byte(raw), &credential); err != nil {
		return "", fmt.Errorf("decode OAuth credentials: %w", err)
	}
	if credential.ClientID == "" {
		clientRaw, err := keyring.Get(connectorService, mcpServerSecret(serverID, "oauth-client"))
		if errors.Is(err, keyring.ErrNotFound) {
			return "", errors.New("Sign in again")
		}
		if err != nil {
			return "", err
		}
		var client mcpOAuthClient
		if err := json.Unmarshal([]byte(clientRaw), &client); err != nil {
			return "", fmt.Errorf("decode OAuth client registration: %w", err)
		}
		credential.ClientID, credential.ClientSecret = client.ClientID, client.ClientSecret
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
	credential.AccessToken = refreshed.AccessToken
	credential.RefreshToken = refreshed.RefreshToken
	credential.TokenType = refreshed.TokenType
	credential.Expiry = refreshed.Expiry
	if err := storeMCPOAuthCredential(serverID, credential); err != nil {
		return "", err
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
