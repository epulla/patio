package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/zalando/go-keyring"
	connectorstatus "patio/internal/connector"
)

func (s *Server) connectorStatus(w http.ResponseWriter, r *http.Request) {
	var c connector
	err := s.db.QueryRow(`SELECT id,kind,config_dir FROM connector WHERE id=?`, idParam(r, "id")).Scan(&c.ID, &c.Kind, &c.ConfigDir)
	if errors.Is(err, sql.ErrNoRows) {
		errw(w, http.StatusNotFound, "connector not found")
		return
	}
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.Kind == "local" {
		clearLocalModelCapabilities(c.ID)
		write(w, http.StatusOK, connectorstatus.Status{Kind: "local"})
		return
	}
	secret := ""
	if c.Kind == "copilot" {
		secret, err = keyring.Get(connectorService, connectorSecret(c.ID))
		if errors.Is(err, keyring.ErrNotFound) {
			err = nil
		}
		if err != nil {
			errw(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	configDir := ""
	if c.ConfigDir != nil {
		configDir = *c.ConfigDir
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	status := connectorstatus.Check(ctx, connectorstatus.Options{Kind: c.Kind, ConfigDir: configDir, Secret: secret, Version: s.version})
	write(w, http.StatusOK, status)
}
