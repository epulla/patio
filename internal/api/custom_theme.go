package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

type customTheme struct {
	Primary     string `json:"primary"`
	Secondary   string `json:"secondary"`
	Base        string `json:"base"`
	SidebarFrom string `json:"sidebar_from"`
	SidebarTo   string `json:"sidebar_to"`
}

var themeColor = regexp.MustCompile(`(?i)^#[0-9a-f]{6}$`)
var customThemeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

type namedCustomTheme struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Colors customTheme `json:"colors"`
}

func (s *Server) getCustomTheme(w http.ResponseWriter, _ *http.Request) {
	var raw string
	err := s.db.QueryRow(`SELECT value FROM app_setting WHERE key='custom_theme'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		write(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	var theme customTheme
	if err := json.Unmarshal([]byte(raw), &theme); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, theme)
}

func (s *Server) putCustomTheme(w http.ResponseWriter, r *http.Request) {
	var theme customTheme
	if decode(r, &theme) != nil || !themeColor.MatchString(theme.Primary) || !themeColor.MatchString(theme.Secondary) || !themeColor.MatchString(theme.Base) || !themeColor.MatchString(theme.SidebarFrom) || !themeColor.MatchString(theme.SidebarTo) {
		errw(w, http.StatusBadRequest, "invalid custom theme")
		return
	}
	data, _ := json.Marshal(theme)
	if _, err := s.db.Exec(`INSERT INTO app_setting(key,value) VALUES('custom_theme',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(data)); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, theme)
}

func (s *Server) getCustomThemes(w http.ResponseWriter, _ *http.Request) {
	themes := []namedCustomTheme{}
	var raw string
	err := s.db.QueryRow(`SELECT value FROM app_setting WHERE key='custom_themes'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		write(w, http.StatusOK, themes)
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := json.Unmarshal([]byte(raw), &themes); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, themes)
}

func (s *Server) putCustomThemes(w http.ResponseWriter, r *http.Request) {
	var themes []namedCustomTheme
	if decode(r, &themes) != nil || len(themes) > 50 {
		errw(w, http.StatusBadRequest, "invalid custom themes")
		return
	}
	if themes == nil {
		themes = []namedCustomTheme{}
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for i := range themes {
		t := &themes[i]
		t.Name = strings.TrimSpace(t.Name)
		if !customThemeID.MatchString(t.ID) || ids[t.ID] || t.Name == "" || utf8.RuneCountInString(t.Name) > 24 || names[strings.ToLower(t.Name)] ||
			!themeColor.MatchString(t.Colors.Primary) || !themeColor.MatchString(t.Colors.Secondary) || !themeColor.MatchString(t.Colors.Base) || !themeColor.MatchString(t.Colors.SidebarFrom) || !themeColor.MatchString(t.Colors.SidebarTo) {
			errw(w, http.StatusBadRequest, "invalid custom themes")
			return
		}
		ids[t.ID], names[strings.ToLower(t.Name)] = true, true
	}
	data, _ := json.Marshal(themes)
	if _, err := s.db.Exec(`INSERT INTO app_setting(key,value) VALUES('custom_themes',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(data)); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, themes)
}
