# Enhanced ticket search

## Request

> feat (enhanced ticket search): Create more inputs so the ticket search is more powerful (board selector, time created/update, table-like sorting). The ticket view should be presented as list btw. The view should show the most recent tickets by default, but there should be a custom dropdown to sort alphabetically and most recent

## Decisions

1. Empty query lists tickets: `q` becomes optional; all tickets on non-deleted boards, capped at 200 rows, UI shows "Showing first 200" when the cap is hit.
2. Most recent = `created_at DESC`. Alphabetical = `title` case-insensitive ASC, then `code`. Ties fall back to `rowid`.
3. Table-like sorting: column header row (Code · Title · Board · Status · Created · Updated); clicking toggles asc/desc. The dropdown (Most recent / Alphabetical) and headers share one sort state.
3a. Layout follows the `list-view` conventions (`Skills.tsx`): `.list-toolbar` with count + sort `<Menu>` (`menuitemradio`), one `.panel` `<ul>` of rows, sort persisted in `localStorage` key `ticket-sort`.
4. Sort dropdown reuses `Menu.tsx`, never a native `<select>`.
5. Board selector: single board, "All boards" first.
6. Four date inputs (Created from/to, Updated from/to), whole local days inclusive; UI converts to UTC ISO bounds (`*_from` inclusive, `*_to` exclusive = start of next local day).
7. MCP `search_tickets` exposes the same filters and sort; `query` becomes optional.
8. Plan file deleted in the last task's commit.

Assumed (by me, per 3a "do whatever you think"):
- Row look: a button row with a grid of code (monospace, bold) · title · board · status · created · updated (short local date); the header row is a grid of sort buttons aligned to the same columns with `aria-sort`. On narrow widths board/status/dates wrap under the title.
- Sorting runs on the server (results are capped, so client sort would sort the wrong 200).
- API params: `sort` in `created|updated|title|code|board|status`, `dir` in `asc|desc`; invalid values or unparsable RFC 3339 dates return 400.

## Tasks

- [x] 1. API: `GET /api/tickets/search` accepts optional `q`, `board_id`, `created_from/to`, `updated_from/to`, `sort`, `dir`; default `created_at DESC`, limit 200, 400 on bad input. Check: `go test ./internal/api -run TestSearchTickets -count=1` with new cases (empty q, each filter, each sort, bad sort/date).
- [x] 2. MCP: `search_tickets` gets the same optional args. Check: `go test ./internal/mcp -count=1` with a filtered + sorted call.
- [ ] 3. UI: `Tickets.tsx` list view with board selector, date inputs, sort `<Menu>`, sortable header row, default most recent; README line updated; this plan deleted. Check: `gofmt -l internal` empty, `go vet ./... && go test ./... -count=1`, `pnpm --dir web build`, `wails build`, smoke run with a throwaway DB.
