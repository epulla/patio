# Rebrand my-tickets to Patio

## Request

feat (re-brand): (centralized workspace) (think more a name) (nucleus? capital? centrally? chores? something that helps with only see one window instead of having multiple places to see for AI like terminal or browser, and manage your tasks accordingly)

## Decisions

- Name: Patio. Binary/app `patio`, window title "Patio".
- Dedication "For Dolores, Domenica, Gary and José Martín" in Settings → General and at the bottom of the README.
- Tagline: "One window for your AI agents and your tasks". Kanban becomes one feature. Icon unchanged.
- `my-tickets` removed everywhere: Go module, data dir (`patio/patio.db`), keychain service `patio`, MCP server name `patio` (prefix `mcp__patio__`, old prefix not accepted), Codex clientInfo, backup format `patio-board` (old format not accepted; AGENTS.md rule updated), release artifacts, repo URL, `.agents/skills`, AGENTS.md.
- No in-app migration. Patio starts fresh. Old `~/Library/Application Support/my-tickets` is deleted.
- Keychain: old `my-tickets` entries are keyed by connector ids from the old DB, so copying them into a fresh DB is useless; they get deleted instead (pending user confirmation).
- No "formerly my-tickets" note in README.
- MCP client configs: neither `~/.claude.json` nor `~/.codex/config.toml` registers the server; only project trust entries keyed by the checkout path, updated after the move.
- Checkout moves to `~/Desktop/projects/patio`; GitHub repo renamed with `gh repo rename patio`, `origin` updated.
- Plan file deleted in the last commit.

## Tasks

- [x] 1. Rename every identifier in code, build, and CI to patio. Check: `grep -rIi "my-tickets\|my_tickets\|mytickets" --exclude-dir={node_modules,dist,.git,bin} .` matches only this plan file; gofmt/vet/test, `pnpm --dir web build`, `wails build`, `make release` pass; smoke-test `build/bin/patio.app` with a temp DB and `patio mcp` reports server name `patio`.
- [ ] 2. README and UI copy: tagline, AI-first intro, dedication in README and Settings → General. Check: `pnpm --dir web build` passes; Settings → General shows the dedication in the smoke-test app.
- [ ] 3. Local cleanup (no repo change besides ticking): delete `~/Library/Application Support/my-tickets` and `my-tickets` keychain entries, after a final confirmation. Check: folder gone; `security find-generic-password -s my-tickets` finds nothing.
- [ ] 4. Delete this plan file, `gh repo rename patio`, update `origin`. Check: `git remote -v` shows `epulla/patio`; `git push` works.
- [ ] 5. Move checkout to `~/Desktop/projects/patio`, update the trust entries in `~/.claude.json` and `~/.codex/config.toml`, move this session. Check: `git -C ~/Desktop/projects/patio status` clean; both configs reference the new path only.
