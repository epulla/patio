# my-tickets

Local, single-user kanban ticket app. One Go binary with an embedded React UI and a SQLite database. Runs on Windows, macOS, and Linux with no install or CGO.

Port of the prod-tickets schema, plus a `ticket_status_transition` table that records every status change for future reporting.

## Features

- Boards: create, rename, change short name (old ticket codes still resolve), delete.
- Kanban: drag tickets within and between columns, reorder columns (drag or left/right buttons), add columns, temporary columns (tickets hide after 1 day), search by title or code.
- Tickets: rich-text description, ticket types, due date, status, history ("Status changed from X to Y"), status transitions.
- Export: board as Excel, CSV, or JSON backup; ticket as JSON or Markdown.
- Import: a board JSON backup imports as a new board, e.g. on another computer. A colliding short name gets a suffix (`NB` -> `NB2`).
- Print: `/print/tickets/<code>` shows a clean printable page with history and transitions.
- Hidden: comments and assignee work in the API; the UI shows them only with `-show-hidden`.

## Run

```sh
make build
./bin/my-tickets            # opens http://127.0.0.1:<random port> in the browser
```

Flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `-port` | `0` (random) | HTTP port, bound to 127.0.0.1 only |
| `-no-browser` | `false` | Do not open the browser |
| `-show-hidden` | `false` | Show comments and assignee in the UI |

Stop it with `pkill -f bin/my-tickets` (macOS/Linux) or Ctrl+C.

## Data

The database is `my-tickets.db` in the OS data directory:

- macOS: `~/Library/Application Support/my-tickets/`
- Linux: `$XDG_DATA_HOME/my-tickets/` or `~/.local/share/my-tickets/`
- Windows: `%LOCALAPPDATA%\my-tickets\`

Back up a board with its JSON export, or copy the `.db` file while the app is stopped.

## Development

```sh
go run . -port 8080 -no-browser   # API on :8080
pnpm --dir web install
pnpm --dir web dev                # Vite dev server, proxies /api to :8080
```

## Build and test

```sh
make test          # go test ./...
make build         # frontend + bin/my-tickets
make web release   # frontend + Windows/macOS/Linux binaries in bin/
```

## Manual smoke test

1. Create a board and add a few tickets.
2. Drag a ticket to another column and back; history reads "Status changed from X to Y".
3. Reorder columns by dragging and with the left/right buttons.
4. Open a ticket, click **Print**, check the print preview.
5. Export the board as JSON, import it from the boards list, check the copy gets a new short name.
