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
	status, err := s.connectorCheck(r.Context(), c)
	if err != nil {
		errw(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusOK, status)
}

func (s *Server) connectorCheck(ctx context.Context, c connector) (connectorstatus.Status, error) {
	secret := ""
	if c.Kind == "copilot" {
		var err error
		secret, err = keyring.Get(connectorService, connectorSecret(c.ID))
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return connectorstatus.Status{}, err
		}
	}
	configDir := ""
	if c.ConfigDir != nil {
		configDir = *c.ConfigDir
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return connectorstatus.Check(ctx, connectorstatus.Options{Kind: c.Kind, ConfigDir: configDir, Secret: secret, Version: s.version}), nil
}
