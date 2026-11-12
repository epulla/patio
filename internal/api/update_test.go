package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestUpdateAndNotification(t *testing.T) {
	tests := []struct {
		name, current, latest string
		want                  bool
	}{
		{"newer", "v0.9.0", "v0.9.1", true},
		{"same", "v0.9.1", "v0.9.1", false},
		{"dev", "dev", "v0.9.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("User-Agent") == "" {
					t.Error("missing User-Agent")
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tt.latest, "html_url": "https://example.test/releases/tag/" + tt.latest})
			}))
			defer releases.Close()
			db, _, root := chatAPI(t)
			s := New(db, "local", false, tt.current, root, Options{UpdateURL: releases.URL})
			var got struct {
				Current, Latest, URL, Download string
				Available                      bool
			}
			if code := requestJSON(t, s, http.MethodGet, "/api/update", nil, &got); code != http.StatusOK || got.Available != tt.want {
				t.Fatalf("update = %d, %+v", code, got)
			}
			if got.Current != tt.current || got.Latest != tt.latest || got.URL != "https://example.test/releases/tag/"+tt.latest || !strings.HasPrefix(got.Download, "https://example.test/releases/download/"+tt.latest+"/patio-"+runtime.GOOS+"-"+runtime.GOARCH) {
				t.Fatalf("update response = %+v", got)
			}
			if err := s.checkForUpdate(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := s.checkForUpdate(context.Background()); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM notification WHERE kind='update'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			wantCount := 0
			if tt.want {
				wantCount = 1
			}
			if count != wantCount {
				t.Fatalf("notifications = %d; want %d", count, wantCount)
			}
		})
	}
}

func TestReleaseNotes(t *testing.T) {
	hits := 0
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.HasSuffix(r.URL.Path, "/v0.9.1") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if hits == 1 {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"body": "Release **notes**", "html_url": "https://github.com/epulla/patio/releases/tag/v0.9.1"})
	}))
	defer releases.Close()
	db, _, root := chatAPI(t)
	s := New(db, "local", false, "v0.9.1", root, Options{ReleaseNotesURL: releases.URL})
	if code := requestJSON(t, s, http.MethodGet, "/api/release-notes", nil, nil); code != http.StatusBadGateway {
		t.Fatalf("release notes while GitHub is down = %d", code)
	}
	for i := 0; i < 2; i++ {
		var got struct{ Version, Notes, URL string }
		if code := requestJSON(t, s, http.MethodGet, "/api/release-notes", nil, &got); code != http.StatusOK || got.Version != "v0.9.1" || got.Notes != "Release **notes**" {
			t.Fatalf("release notes = %d, %+v", code, got)
		}
	}
	if hits != 2 {
		t.Fatalf("GitHub hits = %d", hits)
	}
	var show struct{ Show bool }
	if code := requestJSON(t, s, http.MethodGet, "/api/whats-new", nil, &show); code != http.StatusOK || !show.Show {
		t.Fatalf("whats-new = %d, %+v", code, show)
	}
	if code := requestJSON(t, s, http.MethodPut, "/api/whats-new", nil, nil); code != http.StatusNoContent {
		t.Fatalf("PUT whats-new = %d", code)
	}
	if code := requestJSON(t, s, http.MethodGet, "/api/whats-new", nil, &show); code != http.StatusOK || show.Show {
		t.Fatalf("whats-new after PUT = %d, %+v", code, show)
	}
	dev := New(db, "local", false, "dev", root)
	if code := requestJSON(t, dev, http.MethodGet, "/api/release-notes", nil, nil); code != http.StatusNotFound {
		t.Fatalf("dev release-notes = %d", code)
	}
	if code := requestJSON(t, dev, http.MethodGet, "/api/whats-new", nil, &show); code != http.StatusOK || show.Show {
		t.Fatalf("dev whats-new = %d, %+v", code, show)
	}
}
