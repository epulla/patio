# calendar

## Request

> also it'd be nice to add a new feat: a calendar interface where tickets with limit time could be shown in it. Also, the MCP and the user could be able to create other events like: events, reminders

Depends on `desktop-and-mcp.md` (Wails, MCP subcommand, `ai` user). Start after it ships.

## Decisions

1. **View.** Month grid with week toggle, all boards with a board filter. Shows tickets with `due_date` and calendar items. Clicking a ticket opens it. No drag-to-reschedule.
2. **Model.** New table `calendar_item(id, kind 'event'|'reminder', title, description, starts_at, ends_at NULL, all_day, board_id NULL, ticket_id NULL, created_by, created_at, updated_at, deleted_at, notified_at NULL)`. Optional link to board or ticket. No recurrence. `notified_at` is derived from decision 4 (fire once). All-day items store `starts_at` as `YYYY-MM-DD`; timed items store ISO-8601 UTC.
3. **Due date.** Stays date-only (`YYYY-MM-DD`) and shows as all-day. API now rejects any other format (currently unvalidated). Timed deadlines = reminder linked to the ticket.
4. **Reminders.** Native OS notification from the desktop app while it runs (checked every 30s). A reminder missed while the app was closed fires once on next launch. Events do not notify. MCP creates reminders, never fires them. Library: `github.com/gen2brain/beeep` (pure Go: osascript on macOS, toast on Windows, D-Bus on Linux); Wails v2 has no notification API.
5. **MCP tools.** `list_calendar(from, to, board?)`, `create_calendar_item`, `update_calendar_item`, `delete_calendar_item` (soft delete). Due dates are set through `update_ticket`.
6. **Backup.** Board JSON includes calendar items linked to that board or its tickets. This is an additive field, so version 1 stays backward compatible. Import remaps board/ticket ids. Unlinked items are not exported.

## Tasks

- [x] 1. Schema + API: migration for `calendar_item`; `GET /api/calendar?from=&to=&board_id=` returns items plus due tickets in range; `POST/PUT/DELETE /api/calendar-items[/{id}]` (soft delete); `due_date` validated as `YYYY-MM-DD` on create/update. **Check:** `go test ./... -count=1` covers range filter, board filter, soft delete hidden, invalid due_date → 400, all-day vs timed validation.
- [x] 2. Calendar UI: `/calendar` route + nav link, month/week toggle, board filter, create/edit/delete item form, ticket chips link to the ticket. **Check:** `pnpm --dir web build` passes; manual: create event + reminder, set ticket due date, all three show on the right day in month and week view.
- [x] 3. Reminder notifications: desktop goroutine fires due reminders with `notified_at IS NULL`, sets `notified_at` in the same transaction. **Check:** Go test: past-due unfired reminder selected once, never twice, events never selected. Manual: reminder 1 min ahead → OS notification appears.
- [ ] 4. MCP calendar tools: the 4 tools over the in-process API as user `ai`. **Check:** in-memory MCP test: create reminder linked to ticket → list_calendar returns it with the ticket's due date in range → delete → gone.
- [ ] 5. Backup: export linked items in board JSON, import remaps ids, old backups without the field still import. **Check:** round-trip test equal calendar item count and links; existing v1 fixture without `calendar_items` imports.
