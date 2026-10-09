# Codex, Copilot, and opencode chats (phase b2b)

## Request

> Connectors: can have multiple Claude & ChatGPT connections and should show the usage quota. Chats: simple chats with text as Claude Desktop (minimal MVP)

> i would like to connect to Codex and Copilot CLI.

Phase (b2b) after `docs/plans/ai-chats.md` (b2a). The Chats page, storage, events (`chat:<id>` delta/tool/done/error), approval card, and model picker are reused unchanged.

## Decisions

1. Approvals are in-process: the driver inserts a `chat_approval` row, waits for the API answer (same 10-minute timeout = deny), and replies to the CLI.
   - Codex: `approvalPolicy: "on-request"`, sandbox `workspace-write` (writes confined to the chat workdir, no network). `item/commandExecution/requestApproval` and `item/fileChange/requestApproval` are answered with `accept` or `decline`.
   - Copilot: `session/request_permission`. Allow once picks the `allow_once` option and Deny picks `reject_once`; if an option is missing, the closest `kind` is used.
   - opencode: `{"permission":{"*":"ask","edit":"ask","bash":"ask","webfetch":"ask"}}`. `permission.asked` is answered with `once` or `reject`.
2. Ticket tools: `my-tickets mcp -d <db>` for every CLI (no `-approve-chat`). Codex gets it through thread/start `config.mcp_servers`, with a fallback to `-c mcp_servers...` at app-server launch if the per-thread route fails live. Copilot gets it through `session/new` `mcpServers`, and opencode through `OPENCODE_CONFIG_CONTENT` `mcp`. Ticket tool calls are auto-allowed.
3. Processes:
   - Codex: one `codex app-server` per chat, using connector env/args (`-c cli_auth_credentials_store="file"` for isolated connectors).
   - Copilot: one `copilot --acp --stdio` per chat.
   - opencode: one `opencode serve --hostname 127.0.0.1 --port <free>` per connector with `OPENCODE_SERVER_PASSWORD` set to a random value, shared by that connector's chats, with one opencode session per chat.

   All processes are killed on chat delete, connector delete, and app quit.
4. Resume: `chat.session_id` stores the Codex thread id, the Copilot session id, or the opencode session id. Codex uses `thread/resume`, Copilot uses `session/load`, and opencode reuses the id.
5. Model: Codex passes `model` to thread/start, and opencode passes `{providerID, modelID}` split from `provider/model`. Copilot ignores the model (its list is Default only).
6. Quota: Codex `account/rateLimits/updated` during chats is written to the connector `quota_json`. Copilot and opencode are unchanged.
7. Live checks: up to 3 tiny prompts per CLI ("reply OK", one approval allowed, one denied). opencode uses a provider that `opencode auth list` shows as signed in.
8. Commits use the caveman-commit style (Conventional Commits, for example `feat(chat): add codex driver`).
9. Stop: Codex `turn/interrupt`, Copilot `session/cancel`, opencode `POST /session/{id}/abort`.

## Tasks

- [x] Driver seam: split `chat_driver.go` into a per-kind runtime (start, send, stop, close) with Claude as the first implementation, plus an in-process approval helper (insert, wait, expire). Check: the existing Claude fixture tests pass unchanged; a new test answers an in-process approval through the API (allow, deny, timeout); `go test -race ./internal/api/...`.
- [ ] Codex driver: app-server initialize → thread/start or thread/resume → turn/start, mapping deltas, items, approvals, errors, turn/completed, and rate-limit updates (decisions 1–6, 9). Check: Go tests over recorded JSON-RPC fixtures via fake pipes; live: "reply OK" streams, a file write raises an approval card that is allowed and then denied, and resume works after a process restart.
- [ ] Copilot driver: ACP initialize → session/new or session/load → session/prompt, mapping agent_message_chunk, tool_call, request_permission, and cancel (decisions 1–5, 9). Check: Go tests over recorded ACP fixtures; live checks the same as Codex.
- [ ] opencode driver: a per-connector serve with a password, session per chat, prompt_async, `/event` SSE mapping of text deltas, tool parts, permission.asked and session.idle, plus abort (decisions 1–5, 9). Check: Go tests with an `httptest` fake server (SSE); live checks the same as Codex.
- [ ] Chats UI: allow every kind except `local` in the New chat dialog, drop "coming soon", and update the empty-state copy. Check: `pnpm --dir web build`; `wails build`; smoke test: one chat per CLI streams a reply and shows an approval card.
