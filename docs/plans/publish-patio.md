# Publish Patio

## Request

> I wanna work on the 2 patio tickets that are INPROGRESS. I've got some assets here: /Users/epulla/Downloads/patio-crayon-kit-v3/. Let's plan the publish of patio

Tickets: PROJ-50 (publish it), PROJ-56 (Rework README for marketing).

## Decisions

1. README: use the kit's rewritten `README.md` as-is, plus screenshots.
2. Visuals: 2–3 screenshots from a throwaway demo DB, no GIF.
3. Icons: apply `docs/branding/wails-icons.patch` + all kit icon files in one commit.
4. Release: `v0.8.4`.
5. Repo page: description + topics via `gh`; wording taken from the kit README.
6. Social posts: English only, LinkedIn + X, drafted as a PROJ-50 comment; user posts.
7. This plan file is deleted in the last task's commit.

## Tasks

- [x] **1. Icons.** Copy kit `build/`, `packaging/`, `docs/branding/`; apply `wails-icons.patch`. Check: `go test ./...` passes, `wails build` succeeds, new icon visible on `build/bin/patio.app`.
- [x] **2. README + screenshots.** Copy kit `README.md` and `docs/assets/`; capture 2–3 screenshots from a temp demo DB into `docs/assets/`, add them to the README. Check: no personal data visible; every local link/image in README resolves.
- [x] **3. Release v0.8.4.** Push main, tag `v0.8.4`. Check: release workflow green, 4 artifacts on the release page.
- [ ] **4. Repo page.** `gh repo edit` description + topics. Check: `gh repo view --json description,repositoryTopics` shows them. User uploads `patio-crayon-social.png` as social preview manually.
- [ ] **5. Launch posts.** LinkedIn + X drafts (EN) as a PROJ-50 comment; delete this plan file. Check: user approves drafts.
