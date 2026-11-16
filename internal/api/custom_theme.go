package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
)

type customTheme struct {
	Primary     string `json:"primary"`
	Secondary   string `json:"secondary"`
	Base        string `json:"base"`
	SidebarFrom string `json:"sidebar_from"`
	SidebarTo   string `json:"sidebar_to"`
}

var themeColor = regexp.MustCompile(`(?i)^#[0-9a-f]{6}$`)

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
