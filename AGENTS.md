# AGENTS.md

AI-first desktop app for agents, chats, scheduled tasks, and kanban tickets: Go + SQLite + Wails v2 + React UI. `patio mcp` exposes AI access over stdio. See README.md for features and usage.

## Rules

- Always use `pnpm`, never `npm` or `npx` (use `pnpm dlx`).
- No CGO except the Wails desktop build on macOS/Linux; Windows and `go test` stay CGO-free. SQLite driver is `modernc.org/sqlite`.
- No ORM, no router library, no state or CSS framework. `database/sql`, `net/http` ServeMux patterns, React with a tiny pathname router.
- Plans live in `docs/plans/`.
- UI: no colored accent stripes on box edges (e.g. a primary `border-left` on callouts or a state-colored top border on cards). Use a plain `var(--border)` border.
- New migration → update `internal/mcp/schema.md`.

## Layout

- `main.go`: flags (`-d`, `-show-hidden`), `mcp` subcommand, Wails app, OS-specific DB path in `dbPath`.
- `internal/db/`: `Open` applies pragmas (foreign_keys, WAL, busy_timeout) via DSN, runs embedded migrations tracked by `PRAGMA user_version`, seeds users, and `Watch` detects external writes.
- `internal/db/migrations/`: numbered `.sql` files. Never edit an applied migration; add a new file.
- `internal/api/`: `server.go` (routes, JSON helpers, types), `boards.go`, `tickets.go`, `transfer.go` (export/import), `calendar.go` (calendar items + due tickets).
- `internal/mcp/`: MCP stdio server; requests act as user `ai`.
- `internal/reminder/`: claims due reminders once (`notified_at`) and fires OS notifications from the desktop app.
- `web/src/`: `App.tsx` (router), `AIBadge.tsx`, `BoardList.tsx`, `BoardView.tsx`, `TicketDetail.tsx`, `PrintTicket.tsx`, `Calendar.tsx`, `api.ts` (types + fetch).
- `web/embed.go`: `//go:embed all:dist`. `web/public/.gitkeep` keeps `web/dist/` non-empty so `go build` works before a frontend build.
- `wails.json`: Wails app and pnpm frontend build configuration.

## Domain invariants

- UI acting user is `local`; MCP acts as `ai` (Martin, `is_ai=1`).
- `internal/db.Watch` polls `PRAGMA data_version`; external writes emit Wails event `db-changed` for UI refresh.
- IDs are UUIDv4 TEXT generated in Go. Timestamps are ISO-8601 UTC TEXT.
- Ticket number comes from `board_counter` per board; code is `<short_name>-<number>`. Changing short_name rewrites codes and stores old ones in `old_ticket_code`.
- Ordering lives in JSON arrays: `status_order`, `ticket_order`, `ticket_type_order`. Corrupt order JSON must return an error, never an empty list.
- Every status change writes one `ticket_history` row ("Status changed from X to Y") and one `ticket_status_transition` row. Ticket creation writes a transition with `from_status_id` NULL. Same-column reorder writes neither.
- Multi-row writes run in one transaction. Never ignore DB or JSON errors.
- Lists with timestamps sort by timestamp, then `rowid` (insertion order), not random ids.
- `-show-hidden` only affects the UI (comments, assignee). The API always serves them.
- JSON backup format `patio-board` version 1 must stay backward compatible. Import validates all references and remaps every id.
- Sanitize every rendered HTML description with DOMPurify. CSV/xlsx cells starting with `= + - @` get a leading `'`.

## Verify

```sh
gofmt -l internal                 # must be empty
go vet ./... && go test ./... -count=1
pnpm --dir web build              # runs tsc
wails build
make release                      # CGO-free Windows compile check
```

Install Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`.

Smoke-test with a throwaway data dir: `build/bin/patio.app/Contents/MacOS/patio -d "$(mktemp -d)/test.db"`.
