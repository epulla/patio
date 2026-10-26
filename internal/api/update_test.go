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
				Available            bool
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
