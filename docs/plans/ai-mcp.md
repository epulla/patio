# External MCP servers (phase c1)

## Request

> MCP: external MCP connectors

> So I want MCP connectors like for example: https://microsoft365.mcp.claude.com/mcp (just an example tho)

Builds on `docs/plans/ai-shell.md` (decision 12), the connectors in `docs/plans/ai-connectors.md`, and the chat drivers in `docs/plans/ai-chats.md` and `docs/plans/ai-chats-clis.md`.

## Decisions

1. Split: c1 = external MCP (this plan); c2 = skills (its own plan, decision 8 below carried over).
2. Storage: migration `0008_mcp_server.sql`, table `mcp_server(id, name UNIQUE, kind 'stdio'|'http', command, args_json, env_keys_json, url, auth 'none'|'bearer'|'oauth', enabled, created_at)`. Env values, bearer tokens, and OAuth tokens (access, refresh, expiry, client registration) are stored in the keychain under `mcp:<id>:*`. Neither the DB nor API responses ever contain them; responses carry only `has_*` flags.
3. OAuth: the app runs it with go-sdk v1.8.0 `auth.AuthorizationCodeHandler`, using dynamic client registration and client ID metadata documents where supported, PKCE S256, `resource` param, a loopback redirect on `127.0.0.1:<free port>`, and the system browser. Tokens are refreshed before each chat starts and passed to the CLI as `Authorization: Bearer`. A server that refuses third-party clients shows its error. As of 2026-10-09, `microsoft365.mcp.claude.com` returns 404 for `/.well-known/oauth-protected-resource`, so it is likely Anthropic-only.
4. Chats: every enabled server goes to every chat on every CLI, alongside `my-tickets`:
   - Claude: `--mcp-config` `mcpServers` (stdio `{command,args,env}`, http `{type:"http",url,headers}`).
   - Codex: thread/start `config.mcp_servers` (stdio `{command,args,env}`, http `{url,http_headers}`).
   - Copilot: session/new `mcpServers` (stdio with `env:[{name,value}]`, http `{type:"http",name,url,headers:[{name,value}]}`).
   - opencode: `OPENCODE_CONFIG_CONTENT` `mcp` (`{type:"local",command:[...],environment}` and `{type:"remote",url,headers}`).

   There is no per-chat selection in this version. Server names are sanitized into identifiers, and `my-tickets` is reserved.
5. Approvals: each CLI's own permission flow. Claude, Copilot, and opencode calls show the approval card. Codex external MCP calls run without asking (its app-server has no MCP-tool approval request), and the MCP page says so.
6. MCP page `/ai/mcp`: server list with health (connect + `ListTools`, latency) and tool list, plus an enable toggle, add/edit/delete, "Sign in" for OAuth servers, and "Test".
7. Health check uses go-sdk `mcp.CommandTransport` (stdio) and `mcp.StreamableClientTransport` (http, with a bearer header or the stored OAuth token) under a 15-second timeout.
8. Carried to c2 (skills): stored in the DB (name, description, Markdown body, enabled), with an editor and preview plus import of a `SKILL.md` file (body only, no resource folders). Delivered to every CLI through `my-tickets mcp` tools `list_skills` and `load_skill`, whose description lists each skill's name and description. The CLIs' native skill folders are left untouched.
9. Commits use the caveman-commit style.

## Tasks

- [x] Storage and CRUD: migration plus `/api/mcp-servers` list/create/update/delete, with keychain secrets (env values, bearer) and validation (name, kind, absolute or PATH command, http(s) URL, env key names). Check: Go tests covering CRUD, validation, no secrets in responses, and keychain cleanup on delete; `make release`.
- [x] Health and tools: `GET /api/mcp-servers/{id}/check` returns `{ok, latency_ms, tools:[{name,description}], error}`. Check: Go tests against an in-process stdio server (`my-tickets mcp` test binary or go-sdk in-memory) and an `httptest` streamable server requiring a bearer header.
- [x] OAuth sign-in: `POST /api/mcp-servers/{id}/login` runs the go-sdk handler with a loopback callback and the browser, stores tokens, and provides a refresh helper used before chats. Check: an end-to-end Go test with a fake authorization server and protected MCP server (`httptest`): DCR, PKCE, code exchange, refresh, and 401 retry. A live attempt on a real OAuth server is optional and marked unverified.
- [ ] Inject into chats: all 4 drivers add enabled servers with fresh tokens (decision 4). Check: Go tests assert each driver's args/config; one live Claude chat prompt uses a stdio test server's tool.
- [ ] MCP page UI (main agent). Check: `pnpm --dir web build`; `wails build`; smoke test: add a stdio server, Test shows its tools, toggling it off removes it from new chats.
