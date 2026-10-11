# Auto-allow per chat, chat defaults, and General settings

## Request

> make the "allow" buttons to have a switch on each chat, so it will allow automatically anything (in other words, have an option to automatically allow everything on the chat)

> 1. I think let's keep the default to be 'on', but let's add an additional 'Defaults' section in settings for having defaults when creating chats. Remove the 'hello' from General and replace it with some general info about the app like version, and other things that you consider important
> 2. yes 3. yes 4. yes 5. yes 6. yeah 7. sounds good

Builds on the approval flow in `docs/plans/ai-chats.md` and `docs/plans/ai-chats-clis.md`, the local driver in `docs/plans/ai-local.md`, and `GET /api/config` (version) from `docs/plans/release-skill.md`.

## Decisions

1. Each chat gets an "Auto-allow" switch (the shared `Switch`) in the chat header. It works for every connector: Claude, Codex, Copilot, opencode and Local AI. It's stored per chat (new migration `chat.auto_allow INTEGER NOT NULL DEFAULT 0`; new chats take the value from Defaults, which starts **on**) and changed with `PUT /api/chats/{id}` `{auto_allow}`. It can change while a reply runs; the next approval request uses the new value. Existing chats keep 0 (off), since their user never chose it.
2. Turning it on in a chat asks once: "Allow every file change and command in this chat without asking? You can turn it off anytime." Turning it off doesn't ask. New chats that start with it on (from Defaults) don't ask.
3. Answered inside the app, without restarting the CLI or changing its permission flags (no `--dangerously-skip-permissions` or Codex `never`): `requestApproval` (Codex, Copilot, opencode, Local AI) and the Claude `approve` tool in `my-tickets mcp -approve-chat` both read the flag from the DB and answer "allow" right away.
4. Approval cards that are already waiting when the switch is turned on are allowed at once.
5. Auto-allowed actions still write a `chat_approval` row with status `allowed` and a new `auto` flag, and the tool line in the chat is marked "auto-allowed".
6. Limits that stay: Local AI file tools still can't leave the chat folder, and delete still goes to the Trash.
7. When it's on, the switch shows in the warning color so it's obvious in the header. No colored stripes on box edges (AGENTS.md).
8. Settings → **Defaults** (new section, used when creating chats): connector (first one if unset), folder (scratch if unset), Auto-allow (on), Tools (on, local chats only), Thinking (off, local chats only). Stored in a new key/value table `app_setting` (migration), served by `GET/PUT /api/settings/defaults`. `POST /api/chats` applies them for fields the request leaves out; the New chat dialog preselects the default connector and folder. A default connector or folder that was deleted falls back to first connector / scratch.
9. Settings → **General** replaces "Hello!" with read-only info from `GET /api/settings/about`:
   - Version (`dev` for local builds), with the OS and architecture.
   - Database file path, with "Show in Finder/Explorer" and "Copy path".
   - Data folder (the database's folder, which holds `chats/` and `connectors/`) and its total size.
   - Counts: boards, tickets, chats, connectors, MCP servers, skills.
   - Schema version (`PRAGMA user_version`).
   - Hidden fields mode (`-show-hidden` on or off).
   - Where secrets live (the OS keychain) and a link to the GitHub repo.
10. Commits use the caveman-commit style.

## Tasks

- [x] Backend auto-allow: migration (`chat.auto_allow`, `chat_approval.auto`), API field and PUT, `requestApproval` and the Claude approve tool answer automatically when the flag is on, waiting approvals are resolved when it's turned on. Check: Go tests for each path (in-process approval, the Claude approve tool against the DB, waiting approvals resolved on toggle, flag off still asks); race; `make release`. No live prompts.
- [x] Backend settings: `app_setting` table, `GET/PUT /api/settings/defaults` with validation and fallbacks, `POST /api/chats` applies them, `GET /api/settings/about`, and a reveal endpoint for the data folder. Check: Go tests (defaults round-trip, invalid ids rejected, deleted connector/folder fallback, create uses defaults, about fields); `make release`.
- [x] UI: "Auto-allow" switch in the chat header with the confirm and warning look; "auto-allowed" tool lines; Settings → General info and Defaults section; New chat preselects defaults. Check: `pnpm --dir web build`; `wails build`; user smoke test (new Local AI chat starts with Auto-allow on and a file write runs without a card).
