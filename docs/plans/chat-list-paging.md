# Chat list paging and collapsed chat rail

## Request

> I do have chats here but the collapsed chat view doesn't show them, that got me confused since I thought I didn't have any chat
>
> 1. default 2. default, we can do some sort of pagination (maybe cursor-based) so when the user scrolls down, the older chats get loaded so we can avoid loading all of them at once 3. clock with letter pls like letter + clock 4. i think we can include the pin [...] 5. default
>
> mmm now that I am thinking more, what about instead of letter + clock + pin, what about (for the collaped view only) just put at the right corner a small icon with the clock or pin, if clock and pin applies, then they will be next to each other

## Decisions

1. Collapsed rail: one button per chat with the title's first letter, `data-tip` tooltip with full title, current chat highlighted.
2. All chats, scrolling rail, newest first; older chats load on scroll (cursor paging).
3. Scheduled and pinned shown as small icons in the button's top-right corner, side by side when both apply.
4. Corner icons are indicators only; pinning stays in the expanded list.
5. Collapsed with no chats: expand button tooltip says "No chats yet".
6. Same paging for expanded and collapsed lists.
7. `GET /api/chats?limit=50&cursor=<updated_at>|<id>` returns `{chats, next_cursor}`; `next_cursor` null on last page. Assumed: `limit` defaults to 50, clamped to 1..1000.
8. Reload (db-changed) refetches `limit=<loaded count>` in one request.
9. Pins: unpin only when `GET /api/chats/{id}` returns 404; pinned chats beyond loaded pages fetched by id.
10. Plan file deleted in last task.
11. Assumed (user: "make it look good"): expanded row meta is one line with 12px icon, run label kept visible; pin tooltip anchored right; scheduled clock `--info` and pin `--primary` in both views.

## Tasks

- [x] 1. API paging for `GET /api/chats` (limit, cursor, `{chats, next_cursor}`, order `updated_at DESC, rowid DESC`). Check: `go test ./internal/api -run Chat -count=1` with a test paging 3 chats at limit 2 (no duplicates/gaps, last `next_cursor` null) and a 400 on malformed cursor.
- [ ] 2. UI paging in `web/src/Chats.tsx`: load first page, load next page when the list bottom scrolls into view, reload with loaded count, pin pruning by 404. Check: `pnpm --dir web build`; smoke test with >50 chats scrolls and loads older ones; pins of unloaded chats survive reload.
- [ ] 3. Collapsed rail in `web/src/Chats.tsx` + `web/src/styles.css`: letter buttons, corner clock/pin icons, tooltips, "No chats yet" tooltip. Check: `pnpm --dir web build`; smoke test collapsed view shows chats, badges, and current highlight.
- [ ] 4. Verify + cleanup: full Verify block from AGENTS.md, delete this plan. Check: all commands pass.
