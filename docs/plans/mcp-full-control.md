# MCP full control

## Request

> I would like to have a MCP to create MCPs in the app, also if there's anything missing for manipulating the app through an mcp, it should be done as well. I just need to have the ability to create/adjust/modify anything from the app (tickets, boards, skills, mcps, connectors, etc) via MCP

## Decisions

1. Scope: MCP servers, connectors, board update, columns, ticket types, ticket history/comments, all skills (incl. disabled), settings defaults. Out: chats, OAuth/connector login, export/import, allowed folders.
2. Secrets (connector key, MCP bearer token, env values) are write-only via MCP; never returned.
3. No extra guard on stdio MCP server commands; in-app chat approval and external client approval apply.
4. Allowed folders not exposed (AI must not widen its own file access).
5. No new deletes except `delete_column`, which the API already refuses when the column has tickets. No delete for boards, tickets, ticket types, connectors, MCP servers. AI disables MCP servers via `update_mcp_server` `enabled=false`.
6. One tool per action, following existing `addGeneric` pattern.
7. Plan file deleted in last task.
8. Existing `delete_skill`, `delete_scheduled_task`, `delete_calendar_item` kept.
9. Settings defaults tool cannot change `auto_allow`; user sets it in Settings.

## Tasks

- [ ] 1. MCP server tools: `list_mcp_servers`, `create_mcp_server`, `update_mcp_server`, `check_mcp_server`. Check: `go test ./internal/mcp -run MCPServer` creates stdio + http servers, updates one to disabled, asserts tool listing and no secret in output.
- [ ] 2. Connector tools: `list_connectors`, `create_connector`, `update_connector`, `list_connector_models`. Check: `go test ./internal/mcp -run Connector` creates and updates a connector with a secret; list output has `has_secret: true` and never the secret.
- [ ] 3. Board structure tools: `update_board`, `create_column`, `update_column`, `reorder_columns`, `delete_column`, `create_ticket_type`, `update_ticket_type`. Check: `go test ./internal/mcp -run BoardStructure` covers each; `delete_column` on a column with tickets returns the 409 error.
- [ ] 4. Remaining tools: `list_comments`, `get_ticket_history`, `list_users`, `list_skills` gains `include_disabled`, `get_settings_defaults`, `update_settings_defaults` (no `auto_allow`). Check: `go test ./internal/mcp -count=1` passes, tool-name set test in `server_test.go` updated.
- [ ] 5. Docs + cleanup: README MCP tool list, delete this plan. Check: full Verify block from AGENTS.md passes (`gofmt`, `go vet`, `go test ./...`, `pnpm --dir web build`, `wails build`, `make release`).
