package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestCustomTheme(t *testing.T) {
	_, handler, _ := chatAPI(t)
	if code, body := requestRaw(t, handler, http.MethodGet, "/api/settings/custom-theme", nil); code != http.StatusOK || strings.TrimSpace(body) != "null" {
		t.Fatalf("get unset = %d, %q", code, body)
	}
	want := customTheme{"#112233", "#223344", "#334455", "#445566", "#556677"}
	var got customTheme
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/custom-theme", want, &got); code != http.StatusOK || got != want {
		t.Fatalf("put = %d, %#v", code, got)
	}
	got = customTheme{}
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/custom-theme", nil, &got); code != http.StatusOK || got != want {
		t.Fatalf("get = %d, %#v", code, got)
	}
	invalid := want
	invalid.Primary = "red"
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/custom-theme", invalid, nil); code != http.StatusBadRequest {
		t.Fatalf("invalid put = %d", code)
	}
}
