# PROJ-33: Notify when an MCP server or connector goes down

## Request
> A notification (via `s.Notify` from PROJ-19) when an MCP server or connector goes from healthy to down. Transitions only, not every failed check.

## Decisions
1. Background check every 5 min, reusing the existing check code.
2. Down = enabled MCP server whose check fails (incl. "Sign in first"), or non-local connector not installed / not logged in. Disabled servers and local connectors skipped.
3. First check after launch is a silent baseline; state is in memory only.
4. Notify after 2 consecutive failed checks; once per outage.
5. No recovery notification.
6. In-app + native (`native=true`); link `/ai/mcp` or `/ai/connectors`.
7. Plan file deleted in the last task's commit.

## Tasks
- [x] 1. Extract the check bodies of `checkMCPServer` (`mcp_servers.go:715`) and `connectorStatus` (`connector_status.go:14`) into functions the handlers and the monitor share; handlers unchanged in behavior. *Check:* `go test ./internal/api -count=1`.
- [x] 2. `StartHealthMonitor(ctx, every)` in `internal/api/health_monitor.go`: per-target state (healthy, consecutive failures, notified), silent baseline, notify on 2nd consecutive failure, reset on recovery. *Check:* Go test with the existing HTTP fake MCP server: up → down, down → 1 notification; more failed ticks → still 1; first tick down → 0.
- [ ] 3. Start it from `main.go` next to `StartScheduledRunner` (5 min); delete this plan. *Check:* `go test ./... -count=1`; manual: disable network for an HTTP MCP server or break a stdio command → one notification within ~10 min.
