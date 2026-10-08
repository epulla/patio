# Release skill and versioned builds

## Request

> I would like to have some kind of releases (a skill, then a executable for MacOS, Windows, Linux)

Follow-ups:

> 1-3: no, I want a repo skill that helps whenever I say 'release', the skill can help with building or making any github release
> 7, that's what I want the 'skill' for
> 6. let's add in the UI as well, maybe at the bottom right with small monospace font

## Decisions

1. Repo skill at `.agents/skills/release/SKILL.md`, symlinked from `.claude/skills/release` (same layout as existing skills). Triggers on "release", "build a release", "make a GitHub release", "local build".
2. Executable matrix unchanged: darwin arm64 + amd64 (zipped .app), windows amd64 exe, linux amd64.
3. Version embedded via `-ldflags "-X main.version=<tag>"` passed to `wails build` in CI. Default `dev` for local/untagged builds. MCP `serverInfo.version` reports it.
4. Version shown in UI: bottom-right corner, small monospace, muted. Served via existing `GET /api/config`.
5. README release link fixed: `epulla-hm/my-tickets` -> `epulla/my-tickets`.
6. Skill version bump: proposes next semver from conventional commits since last tag (`feat` -> minor, `fix`/other -> patch, breaking `!` -> major; first release `v0.1.0`); user confirms.
7. Skill push gate: run AGENTS.md verify list, show tag + commit summary, wait for explicit "go" before `git tag` + `git push origin <tag>`. Never push untested or without go. Refuses on dirty tree or branch not in sync with origin.
8. After push: `gh run watch` on the release workflow, then `gh release view <tag>` confirms 4 assets.
9. Release notes auto-generated (`generate_release_notes: true` in `softprops/action-gh-release`).
10. Skill also covers local build without tag: `wails build` + smoke test from AGENTS.md.

## Tasks

- [x] Embed version: `main.version` var (default `dev`), pass to `api.New` and MCP `serverInfo`; `GET /api/config` returns `version`. Check: `go vet ./... && go test ./... -count=1`; test asserts config returns version; `go run -ldflags "-X main.version=v9.9.9" . mcp` initialize reports `v9.9.9`.
- [x] UI version label bottom-right, small monospace, muted, non-interactive. Check: `pnpm --dir web build`; `wails build` + smoke run shows `dev`.
- [x] Release workflow: pass `-ldflags "-X main.version=${{ github.ref_name }}"` to `wails build` (tag builds only; dispatch builds stay `dev`), `generate_release_notes: true`; README link fix. Check: `actionlint` (via `pnpm dlx` unavailable -> `go run github.com/rhysd/actionlint/cmd/actionlint@latest`) passes; README link grep shows `epulla/my-tickets`.
- [x] Release skill `.agents/skills/release/SKILL.md` + `.claude/skills/release` symlink, covering decisions 6-8, 10. Check: frontmatter valid (name + description), symlink resolves, every command in it exists in repo/Makefile/AGENTS.md.
