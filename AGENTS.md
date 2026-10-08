# AGENTS.md

Local single-user kanban app: Go HTTP server + SQLite + React UI embedded into one binary. See README.md for features and usage.

## Rules

- Always use `pnpm`, never `npm` or `npx` (use `pnpm dlx`).
- No CGO: SQLite driver is `modernc.org/sqlite`. Every release target must cross-compile with `CGO_ENABLED=0`.
- No ORM, no router library, no state or CSS framework. `database/sql`, `net/http` ServeMux patterns, React with a tiny pathname router.
- Plans live in `docs/plans/`. One commit per task.

## Layout

- `main.go`: flags (`-port`, `-no-browser`, `-show-hidden`), DB path in `os.UserConfigDir()/my-tickets/`, SPA serving, browser open.
- `internal/db/`: `Open` applies pragmas (foreign_keys, WAL, busy_timeout) via DSN, runs embedded migrations tracked by `PRAGMA user_version`, seeds user `local`.
- `internal/db/migrations/`: numbered `.sql` files. Never edit an applied migration; add a new file.
- `internal/api/`: `server.go` (routes, JSON helpers, types), `boards.go`, `tickets.go`, `transfer.go` (export/import).
- `web/src/`: `App.tsx` (router), `BoardList.tsx`, `BoardView.tsx`, `TicketDetail.tsx`, `PrintTicket.tsx`, `api.ts` (types + fetch).
- `web/embed.go`: `//go:embed all:dist`. `web/public/.gitkeep` keeps `web/dist/` non-empty so `go build` works before a frontend build.

## Domain invariants

- Acting user is always `local`. AI agents will get their own `user` rows later.
- IDs are UUIDv4 TEXT generated in Go. Timestamps are ISO-8601 UTC TEXT.
- Ticket number comes from `board_counter` per board; code is `<short_name>-<number>`. Changing short_name rewrites codes and stores old ones in `old_ticket_code`.
- Ordering lives in JSON arrays: `status_order`, `ticket_order`, `ticket_type_order`. Corrupt order JSON must return an error, never an empty list.
- Every status change writes one `ticket_history` row ("Status changed from X to Y") and one `ticket_status_transition` row. Ticket creation writes a transition with `from_status_id` NULL. Same-column reorder writes neither.
- Multi-row writes run in one transaction. Never ignore DB or JSON errors.
- Lists with timestamps sort by timestamp, then `rowid` (insertion order), not random ids.
- `-show-hidden` only affects the UI (comments, assignee). The API always serves them.
- JSON backup format `my-tickets-board` version 1 must stay backward compatible. Import validates all references and remaps every id.
- Sanitize every rendered HTML description with DOMPurify. CSV/xlsx cells starting with `= + - @` get a leading `'`.

## Verify

```sh
gofmt -l internal                 # must be empty
go vet ./... && go test ./... -count=1
pnpm --dir web build              # runs tsc
make web release                  # cross-compile check
```

Smoke-test with a throwaway data dir: `HOME=$(mktemp -d) ./bin/my-tickets -port 18080 -no-browser` (macOS; on Linux set `XDG_CONFIG_HOME`).
