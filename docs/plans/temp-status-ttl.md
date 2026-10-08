# Temporary status: load and correctness

## Request

> there's 'temp' columns/status that make the ticket to dissappear after specific time (like N days), I did that in the other repo in order to avoid crowding the UI and make the DB to suffer by querying lots of tickets

(Raised during the `ui-polish.md` interview. my-tickets already has `status.type = 'temporary'` with a hardcoded 24h hide, `internal/api/boards.go:312,352`.)

## Decisions

8. Configurable N days per temporary status: yes, as a crowding knob; DB load is cut by task 2. Stored as `hide_after_days` (integer, 1 or more, default 1) in `status.additional_fields`, so no migration is needed. Exposed in the status API and the status settings row. The JSON backup gets an optional `hide_after_days` per status; a missing value means 1, so format version 1 stays backward compatible.
9. Board data loads in one query per board with the temporary-status cutoff applied in SQL, replacing the per-ticket `SELECT` loop (`boards.go:302-303`). Order stays by `ticket_order` in Go. New migration adds index `ticket(status_id, status_updated_at)`.
10. When `recent()` can't parse a timestamp, return a 500 instead of silently hiding the ticket.
11. This work lives in a separate plan from `ui-polish.md`.

## Tasks

- [x] 1. Return a 500 when a temporary-status timestamp fails to parse. Check: `go test ./internal/api -count=1` with a new test passes.
- [ ] 2. Load board data with a single query, filter temporary statuses in SQL, and add migration `0005` with the index. Check: new test covers an old temp ticket hidden, a fresh temp ticket shown, classic tickets shown regardless of age, `ticket_order` preserved, and `q` search still works; `go vet ./... && go test ./... -count=1` passes.
- [ ] 3. Add per-status `hide_after_days` across the API create/update/read, the board-data cutoff, export/import, and an input in the `BoardView.tsx` status row shown only for temporary statuses. Check: tests cover a ticket hidden at N=3 after 4 days and shown after 2, a value below 1 rejected with 400, and import of an old backup without the field defaulting to 1; `go test ./... -count=1` and `pnpm --dir web build` pass.
