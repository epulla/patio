# my-tickets

## Request

> I have @prod-tickets-v2.1/ project. However, I would like to build another version of this with the following stack: Go + React + SQlite. This app should be local and should run on Windows, Mac, Linux. I would like to use the same DB schema from the prod-tickets project, however I would like to have some new table(s) for storing the ticket status movements so I can easily get a report for this. For now, the reporting/graphics would stay as a follow-up. The features should be the kanban board, board creation, board movement, board/ticket export, easy print of ticket + ticket history. Since the only user would be the local user + any AI agent (future), let's keep the comment and assign features as they are but hidden

## Decisions

1. **Schema port.** Port everything SQLite can hold: `board`, `status`, `ticket_type`, `ticket`, `ticket_history`, `ticket_comment`, `status_order`, `ticket_order`, `ticket_type_order`, `board_counter`, `old_ticket_code`, `user`. Seed one "local" user; AI agents get `user` rows later. Drop `session`, `account`, `verification`, `board_access`, `board_invitation*`, `notification`, `snippet`. Mappings: uuid → TEXT (generated in Go), enums → TEXT + CHECK, jsonb/`text[]` → JSON TEXT, `serial` → INTEGER PK. Postgres triggers (ticket number/code, `updated_at`, `status_updated_at`, board counter, code rename on board change) → Go service logic inside transactions. `ILIKE` → `LIKE` (case-insensitive by default for ASCII).
2. **Movement.** Same as today: drag ticket between columns, reorder columns, reorder cards within a column. No cross-board ticket move.
3. **New table.** `ticket_status_transition(id, ticket_id, board_id, from_status_id NULL, to_status_id, user_id, moved_at)`. Row on ticket create (from = NULL) and on every status change. Indexes `(board_id, moved_at)`, `(ticket_id, moved_at)`. `ticket_history` still written as today.
4. **Export.** Board → xlsx + CSV + JSON. Ticket → JSON + Markdown. The JSON button has a label/tooltip: "Full board backup — import into my-tickets on another computer."
5. **Print.** Dedicated print view per ticket (fields, description, history, status transitions). Print CSS + `window.print()` (OS dialog gives PDF). Comments hidden.
6. **Packaging.** Single Go binary, React build embedded via `go:embed`, serves on `127.0.0.1:<port>`, auto-opens browser, DB in OS user-config dir (`os.UserConfigDir()`). Pure-Go SQLite `modernc.org/sqlite` (no CGO) → cross-compile win/mac/linux. No Wails/Electron. Repo: `/Users/epulla/Desktop/projects/my-tickets/` (project name: my-tickets).
7. **Feature set.** Keep ticket types/labels, temporary statuses (auto-hide after 1 day), title/code search, rich-text description. Drop auth, SSE, notifications, mentions, snippets, reports (reports = follow-up). Comments + assignee: tables + API kept, UI hidden behind one config flag. No migration of existing Postgres data.
8. **Import.** Built now. Board JSON imports as a new board with fresh IDs; ticket codes regenerated if `short_name` collides; keeps history, transitions, and hidden comments.

## Tasks

- [x] 1. Scaffold: Go module, SQLite migrations for all ported tables + `ticket_status_transition`, seeded local user, data dir, browser auto-open, embedded Vite/React shell. **Check:** `go test ./...` (migration test asserts all tables exist) passes and `GOOS={windows,darwin,linux} go build` succeeds for all three.
- [x] 2. Domain API: board/status/ticket-type/ticket CRUD, ticket numbering + code, column/card ordering, status change writing `ticket_history` + `ticket_status_transition`, temporary-status hiding, search, comments/assignee endpoints. **Check:** `go test ./...` covers numbering per board, status move → 1 transition + 1 history row, reorder persistence.
- [x] 3. React UI: board list/create, kanban with drag between columns, column + card reorder, ticket detail/edit, search; comments/assignee hidden by flag. **Check:** `npm run build` passes; manual run: create board, add ticket, drag to another column → transition row visible via `GET /api/tickets/:id/transitions`.
- [x] 4. Export/import: board xlsx/CSV/JSON, ticket JSON/Markdown, board JSON import, JSON tooltip. **Check:** Go round-trip test: export board JSON → import into fresh DB → equal counts of statuses, tickets, history, transitions, comments; xlsx/CSV open with expected headers.
- [ ] 5. Print view: per-ticket print route with fields, description, history, transitions; print CSS. **Check:** `npm run build` passes; manual print preview shows one clean page without app chrome or comments.
