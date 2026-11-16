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

func TestCustomThemes(t *testing.T) {
	_, handler, _ := chatAPI(t)
	if code, body := requestRaw(t, handler, http.MethodGet, "/api/settings/custom-themes", nil); code != http.StatusOK || strings.TrimSpace(body) != "[]" {
		t.Fatalf("unset = %d, %q", code, body)
	}
	colors := customTheme{"#112233", "#223344", "#334455", "#445566", "#556677"}
	want := []namedCustomTheme{{"theme_1", " 123456789012345678901234 ", colors}, {"theme-2", "Second", colors}}
	var got []namedCustomTheme
	if code := requestJSON(t, handler, http.MethodPut, "/api/settings/custom-themes", want, &got); code != http.StatusOK || got[0].Name != "123456789012345678901234" {
		t.Fatalf("put = %d, %#v", code, got)
	}
	got = nil
	if code := requestJSON(t, handler, http.MethodGet, "/api/settings/custom-themes", nil, &got); code != http.StatusOK || len(got) != 2 || got[0].Name != "123456789012345678901234" {
		t.Fatalf("get = %d, %#v", code, got)
	}
	for _, tc := range []struct {
		name string
		list []namedCustomTheme
	}{
		{"long name", []namedCustomTheme{{"a", "1234567890123456789012345", colors}}},
		{"duplicate name", []namedCustomTheme{{"a", "Same", colors}, {"b", "same", colors}}},
		{"bad color", []namedCustomTheme{{"a", "Valid", customTheme{"red", colors.Secondary, colors.Base, colors.SidebarFrom, colors.SidebarTo}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := requestJSON(t, handler, http.MethodPut, "/api/settings/custom-themes", tc.list, nil); code != http.StatusBadRequest {
				t.Fatalf("put = %d", code)
			}
		})
	}
}
