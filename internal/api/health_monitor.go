package api

import (
	"context"
	"fmt"
	"log"
	"time"
)

type healthState struct {
	seen     bool
	failures int
	notified bool
}

func (s *Server) StartHealthMonitor(ctx context.Context, every time.Duration) error {
	if every <= 0 {
		return fmt.Errorf("health check interval must be positive")
	}
	states := map[string]*healthState{}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if err := s.checkHealth(ctx, states); err != nil && ctx.Err() == nil {
			log.Printf("check health: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// checkHealth notifies once per outage, after two failed checks in a row. A target already
// down on its first check is treated as a known outage, so app launches stay quiet.
func (s *Server) checkHealth(ctx context.Context, states map[string]*healthState) error {
	servers, err := s.watchedMCPServers()
	if err != nil {
		return err
	}
	connectors, err := s.watchedConnectors()
	if err != nil {
		return err
	}
	record := func(key, title, reason, link string) {
		state := states[key]
		if state == nil {
			state = &healthState{}
			states[key] = state
		}
		first := !state.seen
		state.seen = true
		if reason == "" {
			*state = healthState{seen: true}
			return
		}
		if first {
			state.notified = true
			return
		}
		state.failures++
		if state.failures < 2 || state.notified {
			return
		}
		state.notified = true
		if err := s.Notify("health", title, reason, link, true); err != nil {
			log.Printf("notify health: %v", err)
		}
	}
	for _, server := range servers {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		check, err := s.mcpCheck(ctx, server)
		if err != nil {
			log.Printf("check MCP server %s: %v", server.Name, err)
			continue
		}
		record("mcp:"+server.ID, "MCP server down: "+server.Name, check.Error, "/ai/mcp")
	}
	for _, c := range connectors {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status, err := s.connectorCheck(ctx, c)
		if err != nil {
			log.Printf("check connector %s: %v", c.Name, err)
			continue
		}
		reason := ""
		if status.Installed != nil && !*status.Installed {
			reason = "Not installed"
		} else if status.LoggedIn != nil && !*status.LoggedIn {
			reason = "Not logged in"
		}
		record("connector:"+c.ID, "Connector down: "+c.Name, reason, "/ai/connectors")
	}
	return nil
}

func (s *Server) watchedMCPServers() ([]mcpServer, error) {
	rows, err := s.db.Query(mcpServerSelect + ` WHERE enabled=1 ORDER BY created_at,rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mcpServer
	for rows.Next() {
		server, err := readMCPServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, server)
	}
	return out, rows.Err()
}

func (s *Server) watchedConnectors() ([]connector, error) {
	rows, err := s.db.Query(`SELECT id,kind,name,config_dir FROM connector WHERE kind<>'local' ORDER BY created_at,rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []connector
	for rows.Next() {
		var c connector
		if err := rows.Scan(&c.ID, &c.Kind, &c.Name, &c.ConfigDir); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
