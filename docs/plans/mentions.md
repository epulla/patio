# PROJ-6: @mentions in the Markdown editor

## Request
> Typing `@` in the Markdown editor (descriptions, comments) opens a user autocomplete and inserts a mention link.

## Decisions
1. `@` suggests users (`/api/users`); `#` suggests tickets (`/api/tickets/search?q=`) and inserts `[CODE: Title](/tickets/<id>)` (`[`/`]` in titles escaped).
2. `@user` inserts plain `@Name`; no render change (users have no route).
3. No side effects: no notifications, no AI reply.
4. Tickets: all boards, match code or title, first 8 results.
5. Popup at the caret; ↑/↓ move, Enter/Tab pick, Esc close. Triggers only after start-of-text or whitespace, so `# Heading` and `a@b.com` don't open it.
6. Applies to every `MarkdownEditor` (descriptions, comments, new ticket).
7. Plan file deleted in the last task's commit.

## Tasks
- [x] 1. Trigger detection + popup with `@` users: caret-anchored list, keyboard and mouse pick, inserts `@Name `. *Check:* `pnpm build`; manual: `@ma` → Enter inserts `@Martin `, Esc closes, `a@b.com` and `# Heading` don't open it.
- [x] 2. `#` tickets: debounced search, `CODE · Title` rows, inserts ticket link. *Check:* `pnpm build`; manual: `#proj-11` → Enter inserts `[PROJ-11: …](/tickets/<id>)`, preview link opens the ticket.
- [ ] 3. Polish + cleanup: popup styles in `styles.css` (light/dark), blur closes popup without breaking onBlur saves; delete this plan. *Check:* `pnpm build`; manual on description (blur-save), comment, and new-ticket editors.
