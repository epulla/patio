# Onboarding + tour

## Request

> - onboarding for anyone who installs Patio for the first time (create initial board, initial connector is optional but it should be in the onboarding, what else should be there? any settings? patio features to adjust? etc). I don't want the onboarding to be too long or boring, straight to the point to have patio ready to work
> - tour: a tour after onboarding would be fantastic for any additional info the user missed in the onboarding and for any info about what features Patio has and where they are. Tour can be skipped tho.

Tickets: PROJ-97 (onboarding), PROJ-155 (tour, split from PROJ-97).

## Decisions

1. Split: PROJ-97 = onboarding, new ticket = tour; tour ships after onboarding.
2. Trigger: onboarding shows only when there are no boards AND no connectors AND `app_setting.onboarding_done` is unset. Finishing or skipping sets the flag. Existing installs never see it.
3. Steps (4 screens, every step skippable except Welcome):
   1. Welcome: language + theme.
   2. First board: presets Personal / Work / Blank, editable name + code, created in one click.
   3. AI connector (optional): detect installed CLIs, install command for missing ones (reuse the Connectors install block), Skip.
   4. Chat defaults: only if a connector was added; default model, working folder, Martin persona toggle.
   Not included: welcome ticket, Patio-as-MCP setup (tour covers it).
4. Board presets (assumed): Personal = To Do / Doing / Done; Work = Backlog / To Do / In Progress / Review / Done; Blank = To Do / Done. Done columns use the temporary type.
5. Tour: custom spotlight component (no new dependency), anchored with `data-tour` attributes. Stops: Boards, Tickets, Calendar, Monitors, Chats, Skills, MCP, Scheduled, Notifications, Settings, plus Cmd+N, pins/chat dock and keyboard shortcuts. Skip/Esc anytime. Offered at the end of onboarding; "Show tour again" in Settings.
6. All new text in en + es.
7. This plan file is deleted in the last task's commit.

## Tasks

- [x] **1. Onboarding state.** `GET /api/onboarding` returns `{show}` per decision 2; `PUT /api/onboarding` sets `onboarding_done`. Check: Go test covers fresh DB → show, existing board or connector → hidden, after PUT → hidden.
- [x] **2. Onboarding shell + Welcome + First board.** Full-screen flow opened from `App.tsx` when `show`; language/theme step; board presets step creates the board and columns. Check: `pnpm build`; fresh DB shows the flow once and the board exists afterwards.
- [ ] **3. Connector + Chat defaults steps.** Reuse the install/status block from `Connectors.tsx`; defaults step writes `/api/settings/defaults`; Connectors empty state says a CLI is required. Check: `pnpm build`; fresh DB with no CLI can skip, with a CLI ends at a working chat.
- [ ] **4. Tour.** Spotlight component + `data-tour` anchors on the sidebar; offered on the last onboarding screen; "Show tour again" in Settings. Check: `pnpm build`; tour runs, Skip/Esc closes, restart from Settings works.
- [ ] **5. Ship check.** `go test ./...`, `wails build`, manual run on a fresh data dir (macOS; Windows if available); delete this plan file. Check: user confirms in the app.
