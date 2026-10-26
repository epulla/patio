package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const githubLatestRelease = "https://api.github.com/repos/epulla/patio/releases/latest"

type releaseInfo struct {
	Tag string `json:"tag_name"`
	URL string `json:"html_url"`
}

// latestRelease caches GitHub's answer for 10 minutes; unauthenticated calls are limited to 60 an hour per IP.
func (s *Server) latestRelease(ctx context.Context) (releaseInfo, error) {
	s.releaseMu.Lock()
	defer s.releaseMu.Unlock()
	if time.Since(s.releaseAt) < 10*time.Minute {
		return s.release, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	url := s.updateURL
	if url == "" {
		url = githubLatestRelease
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("User-Agent", "Patio")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return releaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var release releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return releaseInfo{}, err
	}
	if _, ok := parseVersion(release.Tag); !ok || !strings.HasPrefix(release.URL, "https://") {
		return releaseInfo{}, fmt.Errorf("unexpected release %q at %q", release.Tag, release.URL)
	}
	s.release, s.releaseAt = release, time.Now()
	return release, nil
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	release, err := s.latestRelease(r.Context())
	if err != nil {
		errw(w, http.StatusBadGateway, err.Error())
		return
	}
	write(w, http.StatusOK, map[string]any{"current": s.version, "latest": release.Tag, "url": release.URL, "download": downloadURL(release), "available": newerVersion(release.Tag, s.version)})
}

// downloadURL is this platform's file in the release, named like the release workflow's artifacts.
func downloadURL(release releaseInfo) string {
	asset := "patio-" + runtime.GOOS + "-" + runtime.GOARCH
	switch runtime.GOOS {
	case "darwin":
		asset += ".zip"
	case "windows":
		asset += ".exe"
	}
	return strings.Replace(release.URL, "/releases/tag/", "/releases/download/", 1) + "/" + asset
}

func parseVersion(v string) ([3]uint64, bool) {
	var parts [3]uint64
	v = strings.TrimPrefix(v, "v")
	fields := strings.Split(v, ".")
	if len(fields) != 3 {
		return parts, false
	}
	for i, field := range fields {
		if field == "" || strings.Trim(field, "0123456789") != "" || len(field) > 1 && field[0] == '0' {
			return parts, false
		}
		var n uint64
		if _, err := fmt.Sscan(field, &n); err != nil {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

func newerVersion(latest, current string) bool {
	a, okA := parseVersion(latest)
	b, okB := parseVersion(current)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func (s *Server) StartUpdateChecker(ctx context.Context, every time.Duration) error {
	if every <= 0 {
		return fmt.Errorf("update check interval must be positive")
	}
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		if err := s.checkForUpdate(ctx); err != nil && ctx.Err() == nil {
			log.Printf("check for update: %v", err)
		}
		timer.Reset(every)
	}
}

func (s *Server) checkForUpdate(ctx context.Context) error {
	if s.version == "dev" {
		return nil
	}
	release, err := s.latestRelease(ctx)
	if err != nil {
		return err
	}
	if !newerVersion(release.Tag, s.version) {
		return nil
	}
	var notified string
	if err := s.db.QueryRow(`SELECT value FROM app_setting WHERE key='update_notified'`).Scan(&notified); err != nil && err != sql.ErrNoRows {
		return err
	}
	if notified == release.Tag {
		return nil
	}
	// Remember the version first so a failed native popup doesn't repeat the notification every day.
	if _, err := s.db.Exec(`INSERT INTO app_setting(key,value) VALUES('update_notified',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, release.Tag); err != nil {
		return err
	}
	return s.Notify("update", "Patio "+release.Tag+" is available", "Open the release page to download it. You have "+s.version+".", release.URL, true)
}
