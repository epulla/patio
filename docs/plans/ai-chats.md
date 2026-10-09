# AI chats (phase b2a)

## Request

> Chats: simple chats with text as Claude Desktop (minimal MVP) (follow up would be file submission: text-based, image) (it should be able to do commands for searching files or whatever from the computer, maybe ripgrep, find, sed, etc).

Phase (b2a) of `docs/plans/ai-shell.md` (decisions 6, 10) and `docs/plans/ai-connectors.md`. b2b (Codex, Copilot, opencode drivers) and b2c (local AI with our file tools) get their own plans.

## Decisions

1. Split: b2a = chat storage, Chats page, and the Claude driver with approvals and quota; b2b = Codex (app-server threads/turns), Copilot (ACP `session/*`), opencode (`serve` + SSE); b2c = local AI with our Go file tools.
2. Transport: sending a message is `POST`. Go pushes reply chunks, tool events, approvals and turn end to the window as Wails events (`chat:<id>`), because the darwin webview response writer has no `Flush`. `api.New` gets an emit hook (no-op in `my-tickets mcp` and tests). Reopening a chat loads saved history from the API.
3. Claude approvals: `claude -p --permission-prompt-tool mcp__my-tickets__approve`. `my-tickets mcp -approve-chat <chat id>` exposes an `approve` tool only when that flag is set. The tool inserts a pending `chat_approval` row and polls it until answered (10-minute timeout = deny). The app sees the write through `db.Watch`, shows the request, and the UI answer is stored through the API. Returns `{"behavior":"allow","updatedInput":<input>}` or `{"behavior":"deny","message":"Denied by user"}`. The input schema `{tool_name, input, tool_use_id?}` is unverified for 2.1.292 and is confirmed live in task 3.
4. Folder: each chat optionally picks one folder from Settings → Allowed folders. Without one, the CLI runs in an empty per-chat dir `<db dir>/chats/<id>`. The folder is the CLI's cwd.
5. Approval UI: an inline card in the chat showing the tool name and arguments, with "Allow once" and "Deny". No "always allow". Reads inside the chat folder are auto-allowed by the CLI. Every chat gets the ticket tools via `my-tickets mcp` in `--mcp-config` with `--strict-mcp-config`.
6. History and model: SQLite stores chats (connector, model, folder, CLI session id, title) and messages (user text, assistant text, a one-line summary per tool call). The model picker offers Claude aliases `sonnet`, `opus`, `haiku` (b2b adds Codex `model/list`, opencode `/config/providers`, and Copilot default only).
7. Live checks: allowed. Tiny prompts ("reply OK") plus one approval-triggering prompt per CLI confirm undocumented wire shapes. They use a little of the user's quota.
8. Claude quota: `rate_limit_event.rate_limit_info` (`utilization`, `resetsAt`, `rateLimitType`) updates the connector's `quota_json` in the existing quota response shape.

## Tasks

- [x] Storage: migration `0007_chat.sql` (`allowed_folder`, `chat`, `chat_message`, `chat_approval`) and API: allowed folders list/add/remove (absolute existing dirs only), chats list/create/get-with-messages/rename/delete (delete removes the per-chat dir), `POST /api/chats/{id}/approvals/{approval_id}` answer. Check: `go test ./... -count=1` covering validation, ordering (timestamp then rowid), cascade delete, approval answer once only; `make release`.
- [ ] Approval tool: `my-tickets mcp -approve-chat <id>` registers `approve`, which writes a pending row and polls for the answer, with a timeout. Check: a Go test runs the MCP server in-process, calls `approve`, answers through the API, and asserts the allow and deny JSON plus the timeout deny; the tool is absent without the flag.
- [ ] Claude driver: a per-chat `claude -p` stream-json process (cwd, `--model`, `--mcp-config` with ticket and approval servers, `--strict-mcp-config`, `--permission-prompt-tool`, `--resume <session>`), using the connector env. Parses init, text deltas, tool use, result and rate-limit events into stored messages and Wails events; `POST /api/chats/{id}/messages` sends, `POST /api/chats/{id}/stop` interrupts. Check: Go tests over recorded stream-json fixtures; live check: "reply OK" streams and saves, a file-write prompt in a temp folder triggers an approval card answered through the API, and quota_json updates.
- [ ] Settings → Allowed folders section (add by path, remove, list). Check: `pnpm --dir web build`; smoke test: adding a non-existent path shows an error.
- [ ] Chats page `/ai/chats`: chat list, new chat (connector, model, optional folder), streaming messages, tool-call lines, approval card, stop, rename/delete, reload restores history. Check: `pnpm --dir web build`; `wails build`; smoke test with a throwaway DB on the Claude connector: a streamed reply, one approval allowed and one denied, and history intact after an app restart.
