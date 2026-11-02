package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestTelegramSettingsAndTokenTest(t *testing.T) {
	keyring.MockInit()
	_, handler := newAPI(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/getMe" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"username":"patio_bot"}}`))
	}))
	defer fake.Close()
	old := telegramAPIBase
	telegramAPIBase = fake.URL
	defer func() { telegramAPIBase = old }()
	var got map[string]any
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/telegram", map[string]any{"enabled": true, "allowed_user_ids": []int64{12}, "token": "token"}, &got); code != 200 {
		t.Fatalf("put status %d", code)
	}
	if got["has_token"] != true || got["enabled"] != true {
		t.Fatalf("settings = %#v", got)
	}
	secret, err := keyring.Get(connectorService, telegramTokenKey)
	if err != nil || secret != "token" {
		t.Fatalf("keyring token = %q, %v", secret, err)
	}
	if _, ok := got["token"]; ok {
		t.Fatal("response exposed token")
	}
	var tested map[string]string
	if code := requestJSON(t, handler, http.MethodPost, "/api/settings/telegram/test", map[string]any{"token": nil}, &tested); code != 200 || tested["username"] != "patio_bot" {
		t.Fatalf("test = %d %#v", code, tested)
	}
	var raw map[string]any
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/telegram", nil, &raw); code != 200 {
		t.Fatal(code)
	}
}
