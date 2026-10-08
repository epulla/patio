# ui-refresh

## Request

> the UI is horrible, you can have a look at the UI from prod-tickets-v2.1 from `../prod-tickets-v2.1` dir. I want you to make the UI better, less shit

## Decisions

1. **CSS framework.** None. Plain CSS with custom properties ports the reference look (daisyUI `acid` light / `dracula` dark palettes, 1rem box radii, `shadow-md` cards, `base-100/200/300` surfaces). AGENTS.md "no CSS framework" rule stays.
2. **Dark mode.** Light/dark toggle via `data-theme` on `<html>`, persisted in `localStorage["theme"]`, falls back to `prefers-color-scheme`.
3. **Layout shell.** Collapsible left sidebar (Boards, Calendar, list of boards, theme toggle) replaces the top bar.
4. **Ticket detail.** Stays the `/tickets/:id` page. Restyled into a 2-column card (content left, status + details right) with Comments / History / Transitions tabs. No modal, no routing change.
5. **Fonts.** Drop Google Fonts. System UI stack for text, system monospace for codes. Works offline.
6. **Scope.** Restyle every screen: board list, kanban, ticket detail, calendar, settings and modals, and the print view's on-screen look. Also import Quill snow CSS, add `:focus-visible` styles, and give destructive buttons a distinct look. Markup changes only where layout needs them. No behavior changes.
7. **Acceptance.** Each task: `pnpm --dir web build` passes, then `wails build` + smoke test with a throwaway DB. The user signs off visually.

## Tasks

- [x] 1. Foundation: rewrite `styles.css` base on tokens (light + dark vars, spacing scale, radii, shadows, system fonts), plus shared button/input/select/badge/panel/modal styles, `:focus-visible`, danger buttons, Quill snow CSS import mapped to theme vars. Theme toggle logic. **Check:** build passes; existing screens render with new tokens in both themes, Quill toolbar styled.
- [ ] 2. Shell: collapsible sidebar in `App.tsx` (Boards, Calendar, boards list with active state, theme toggle), remove top bar, content column layout. **Check:** build passes; sidebar navigates to every route, collapse persists, active item highlighted.
- [ ] 3. Boards + kanban: board list cards and create/import panels (also removes the duplicated import hint); kanban columns (`base-200`, drag-over `base-300`, temporary badge), ticket cards (code mono, title clamp, type dot, due date), board settings and new-ticket modal. **Check:** build passes; drag/drop, reorder, create ticket, and settings still work and look like the reference.
- [ ] 4. Ticket detail: 2-column card, details box with label/value grid, Comments / History / Transitions tabs. **Check:** build passes; edit title, description, status, type, due date, assignee, and comments all still work; tabs switch.
- [ ] 5. Calendar + print: calendar grid, chips, controls and form restyled with tokens; on-screen print view styled while `@media print` output stays unchanged. **Check:** build passes; month/week views, item create/edit work in both themes; print preview unchanged.
