package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

type keepRunningSettings struct {
	KeepRunningOnClose bool `json:"keep_running_on_close"`
}

// KeepRunningOnClose reports whether closing the main window should leave Patio running
// (hidden on macOS, minimized elsewhere) so background work continues. Defaults to on.
func KeepRunningOnClose(database *sql.DB) bool {
	var raw string
	err := database.QueryRow(`SELECT value FROM app_setting WHERE key='keep_running_on_close'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	keep := true
	if err == nil {
		err = json.Unmarshal([]byte(raw), &keep)
	}
	if err != nil {
		log.Printf("read keep_running_on_close: %v", err)
		return true
	}
	return keep
}

func (s *Server) getKeepRunning(w http.ResponseWriter, _ *http.Request) {
	write(w, http.StatusOK, keepRunningSettings{KeepRunningOnClose(s.db)})
}

func (s *Server) putKeepRunning(w http.ResponseWriter, r *http.Request) {
	var in keepRunningSettings
	if decode(r, &in) != nil {
		errw(w, http.StatusBadRequest, "invalid settings JSON")
		return
	}
	data, _ := json.Marshal(in.KeepRunningOnClose)
	if _, err := s.db.Exec(`INSERT INTO app_setting(key,value) VALUES('keep_running_on_close',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(data)); err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, in)
}
