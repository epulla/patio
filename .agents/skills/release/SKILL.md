---
name: release
description: Guides local app/executable builds and GitHub releases. Trigger on “release”, “cut/make a release”, “GitHub release”, “tag a version”, or “build the app/executables locally”.
---

# Release

Use this workflow for local builds and tagged GitHub releases. Never skip required checks.

## Local build (no tag)

1. Run repo verification from `AGENTS.md`:
   - `gofmt -l internal` — output must be empty.
   - `go vet ./... && go test ./... -count=1`
   - `pnpm --dir web build`
   - `wails build`
   - `make release` — CGO-free Windows compile check.
2. Stop and report any failed check. Do not claim build succeeded.
3. Optional: stamp a version with `wails build -ldflags "-X main.version=<v>"`.
   Untagged builds default to `dev`.
4. Smoke-test macOS build:
   `build/bin/patio.app/Contents/MacOS/patio -d "$(mktemp -d)/test.db"`
5. Report build outputs in `build/bin/`.

## GitHub release

### Preconditions

1. Check `git status --short`. Require empty output; otherwise refuse and explain
   uncommitted changes must be handled first.
2. Check `git branch --show-current`. Require `main`; otherwise refuse and explain
   release must start from main.
3. Run `git fetch`, then inspect `git status -sb`. Require branch equal to
   `origin/main`, with no ahead/behind count; otherwise refuse and explain sync
   with origin is required.
4. Run `gh auth status`. Require successful authentication; otherwise refuse
   and explain GitHub CLI authentication is required.

### Select version

1. Read last tag with `git describe --tags --abbrev=0`. If none exists, propose
   `v0.1.0` and review commits with `git log --oneline HEAD`.
2. Otherwise inspect commits with `git log --oneline <last>..HEAD`.
3. Propose next SemVer from those commits:
   - Before 1.0.0: `!` or `BREAKING CHANGE` means minor; anything else (feat, fix, ui…) means patch.
   - From 1.0.0: `!` or `BREAKING CHANGE` means major; any `feat` means minor; otherwise patch.
   Moving to 1.0.0 is a user decision, never proposed from commits.
   Present proposed tag and rationale.
4. Wait for user to confirm or override version before continuing.

### Verify and gate tag

1. Run full `AGENTS.md` verify list, in order:
   - `gofmt -l internal` — output must be empty.
   - `go vet ./... && go test ./... -count=1`
   - `pnpm --dir web build`
   - `wails build`
   - `make release`
2. Any failure stops release. Report failing command and output; do not tag or push.
3. Show exact tag and commit summary from `git log --oneline <last>..HEAD`
   (or `git log --oneline HEAD` for first release).
4. Wait for explicit user `go`. A request to make a release is not this approval.
   Never tag or push before `go` or when checks fail.
5. After approval, run `git tag -a vX.Y.Z -m "vX.Y.Z"`, then
   `git push origin vX.Y.Z` using confirmed version.

### Monitor and confirm

1. Get latest workflow run: `gh run list --workflow release.yml --limit 1`.
2. Watch its ID: `gh run watch <id> --exit-status`.
3. On failure, run `gh run view <id> --log-failed` and report failure. Do not
   retag without user approval; deleting a pushed tag requires explicit approval.
4. On success, check assets with
   `gh release view vX.Y.Z --json assets -q '.assets[].name'`.
5. Require exactly these four names:
   - `patio-darwin-arm64.zip`
   - `patio-darwin-amd64.zip`
   - `patio-windows-amd64.exe`
   - `patio-linux-amd64`
6. Workflow auto-generates release notes. Tag builds stamp version via
   `-ldflags "-X main.version=<tag>"`; app shows version bottom-right and MCP
   `serverInfo` reports it.
7. Manual `workflow_dispatch` builds upload artifacts only, create no release,
   and use version `dev`.

## Never

- Never force-push, move or delete existing tags, edit published releases, or
  skip checks unless user explicitly asks.
- Never repeat a failed release by moving its tag. Require explicit user approval
  before deleting a pushed tag.
