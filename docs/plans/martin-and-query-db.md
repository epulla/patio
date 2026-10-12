# Martin persona everywhere + read-only query_db MCP tool

## Request

```
- feat (martin rethink): Martin persona only works on ClaudeCode, it'd be better to remove? or can it get fixed so it could work on any connector chat
- feat (MCP read-only sqlite query tool): there should be a tool for querying the sqlite db for any complex work like heavy reports, also as a fallback if the other tools can't find anything using the current tools. This tool MUST be read-only, never do any 'UPDATE', 'DELETE' or anything that modifies the DB
```

## Decisions

- Fix, don't remove. Persona text in one shared constant, injected into every chat driver's system prompt (Claude `--append-system-prompt`, local system message, Codex/Copilot/OpenCode native mechanism, else first-turn prefix). MCP `instructions` kept for external clients.
- Persona tone: playful, joyful, makes jokes (still concise).
- Settings switch `martin_persona` in `app_setting`, default on. Off: neutral assistant prompt, no voice/jokes; ticket-link rules stay; AI label and user name Martin unchanged. Switch also governs MCP `instructions` for external clients.
- `chatSystemPrompt` (ticket/board links) shared with all drivers, same shared prompt.
- Verify persona via unit tests per driver (args/request body contain persona); live smoke on installed connectors; uninstalled ones listed as unverified.
- `query_db` read-only, three layers: separate `mode=ro` connection, `PRAGMA query_only=ON`, single statement starting `SELECT`/`WITH`/`EXPLAIN`; reject `ATTACH`, `PRAGMA`, multi-statement. Test: UPDATE, DELETE, INSERT, DROP, ATTACH, write-in-CTE all fail and data unchanged.
- `query_db` limits: max 500 rows, 10s timeout, output `{columns, rows, truncated}`.
- Schema context: `internal/mcp/schema.md` (Mermaid `erDiagram` + notes), `go:embed`, MCP resource `patio://schema`. `query_db` description points to it and says `sqlite_schema` is authoritative, doc is fallback; use `query_db` when other tools find nothing.
- Sync: Go test runs migrations, fails if any real table/column missing from `schema.md`. `AGENTS.md` line: "New migration → update `internal/mcp/schema.md`."
- Plan file deleted in last task's commit.

## Findings

- Martin reaches models only via MCP `initialize` instructions (`internal/mcp/server.go:181`); local driver ignores them.
- Claude: `chatSystemPrompt` via `--append-system-prompt` (`internal/api/chat_claude.go:25,108`). Only driver with it.
- Codex: `thread/start` params (`chat_codex.go:93-96`) have no instructions; add a field there or prefix first turn.
- Copilot: ACP `session/prompt` text only (`chat_copilot.go:180`); prefix in `copilotPromptContent` (`:209`).
- OpenCode: `createSession` (`chat_opencode.go:312`) / `opencodePromptParts` (`:411`); session system field or prefix.
- Local: own `localSystemPrompt` (`chat_local.go:599`), no persona, no link rules.
- Scheduled tasks go through the same drivers (`scheduled_runner.go:175`).

## Tasks

- [x] 1. Confirm per driver (Codex, Copilot, OpenCode, local) whether persona reaches the model today and which system-prompt mechanism each supports; record findings under `## Findings` here. Check: findings list one mechanism per driver with file:line evidence.
- [x] 2. Shared playful persona + link prompt injected into all five drivers. Check: per-driver unit tests assert persona and link text in args/request; `go test ./internal/... -count=1` passes.
- [x] 3. `martin_persona` setting (default on) + Settings toggle; drivers and MCP `instructions` honor it. Check: tests for on/off prompt in a driver and in MCP `InitializeResult().Instructions`; `pnpm --dir web build` passes.
- [ ] 4. `query_db` MCP tool with three read-only layers, limits, output shape. Check: test that SELECT/WITH return rows, truncation at 500, and UPDATE/DELETE/INSERT/DROP/ATTACH/PRAGMA/multi-statement/write-in-CTE fail with row counts unchanged.
- [ ] 5. `internal/mcp/schema.md` + `patio://schema` resource + sync test + `AGENTS.md` line; delete this plan. Check: sync test passes, and fails when a column is removed from `schema.md`; full Verify block in `AGENTS.md` passes.
