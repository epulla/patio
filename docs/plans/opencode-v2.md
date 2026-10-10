# opencode v2 support

## Request

> I just realized there's a new opencode version: [v2](https://opencode.ai/v2/docs/migrate-v1/). Could you pls see if something needs to change on this project for the v2 version?

> 1. both, not sure if we need to create a new connector for it, opine here pls
> 2. yeah I'll be migrating soon
> 3. ok
> 4. yeah

Runs before b2c (`docs/plans/ai-local.md`). Builds on `docs/plans/ai-connectors.md` and `docs/plans/ai-chats-clis.md`.

## Decisions

1. Support both v1 and v2 with the same `opencode` connector kind, without a separate v2 connector type. The app reads `opencode --version` (major version) each time it starts the CLI and uses the v1 or v2 protocol to match. Users who upgrade keep their connectors and chats.
2. Resume across the upgrade: if a stored v1 session id doesn't exist on v2 (create or prompt returns not found), start a new opencode session for that chat and add one `error`-role note: "opencode was upgraded; earlier context was not carried over." Saved messages in the app stay.
3. JSON output on v2: `opencode auth list --format json` and `opencode stats --json`. v1 keeps the text parsers.
4. v2 chat protocol (per https://opencode.ai/v2/docs/api/):
   - Readiness: `GET /api/info`.
   - Create: `POST /api/session` with `location.directory` and the model.
   - Prompt: `POST /api/session/{id}/prompt` `{text}`.
   - Stop: `POST /api/session/{id}/interrupt`.
   - Events: SSE `GET /api/event`.
   - Approvals: `POST /api/session/{id}/permission/{requestID}/reply` `{decision:"once"|"reject"}` → 204. The OpenAPI spec uses `decision`, while the docs page says `reply`; the spec wins.
   - Stop: `POST /api/session/{id}/interrupt?resume=false`.
   - Verified in a sandbox on 2.0.26: Basic auth with user `opencode` and `OPENCODE_SERVER_PASSWORD` (401 without it); `OPENCODE_CONFIG_CONTENT` is honored (visible in `GET /api/config`); create and list sessions work. Not in the spec: event payload schemas (text deltas, tool parts, permission asked, idle/error) and where the model is set. Both get recorded with live prompts in task 2.

   Config uses v2-native `permissions` and `mcp.servers` when the version is 2.
5. The install hint becomes `curl -fsSL https://opencode.ai/v2/install | bash`. v2 installs to the same `~/.opencode/bin/opencode` path as v1 (plus an `opencode2` shim) and uses the same XDG data and config dirs, so an upgrade replaces v1 and detection needs no new directory.
6. Live checks run against an isolated v2 sandbox (`$TMPDIR/opencode/v2-sandbox`, own HOME and XDG dirs, installed with `--no-modify-path`; the user's v1 and shell rc files were verified untouched). Recordings live in `$SB/recordings/`. Prompts use a free logged-out model (`opencode/big-pickle` or another `opencode/*` free id), so no credentials or quota are used: at most 3 tiny prompts (reply OK, approval allow, approval deny). v1 stays live-testable with the user's real install until they migrate.
7. Commits use the caveman-commit style.

## Tasks

- [x] v2 CLI: version detection helper, v2 install hint and install directory, `auth list --format json` status, `stats --json` quota, `models` output check. Check: Go tests over recorded v2 output plus existing v1 fixtures; manual check after the user installs v2.
- [ ] v2 chat driver: protocol switch by major version inside `chat_opencode.go` (decision 4), v2-native config, and the resume fallback (decision 2). Check: Go tests with an `httptest` fake v2 server (SSE, approvals, interrupt, missing-session fallback); existing v1 tests unchanged; live: reply streams, one approval allowed, one denied.
