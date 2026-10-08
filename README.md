# my-tickets

Local kanban ticket desktop app built with Go, Wails, React, and SQLite. MCP stdio subcommand gives AI clients access to same database.

Port of the prod-tickets schema, plus a `ticket_status_transition` table that records every status change for future reporting.

## Features

- Boards: new boards default to To Do, In Progress (classic), and Done (temporary; tickets hide after 1 day); create, rename, change short name (old ticket codes still resolve), delete.
- Kanban: drag tickets within and between columns, reorder columns (drag or left/right buttons), add columns, temporary columns (tickets hide after 1 day), search by title or code.
- Tickets: rich-text description, ticket types, due date, status, history ("Status changed from X to Y"), status transitions.
- Export: board as Excel, CSV, or JSON backup; ticket as JSON or Markdown.
- Import: a board JSON backup imports as a new board, e.g. on another computer. A colliding short name gets a suffix (`NB` -> `NB2`).
- Print: clean printable page with history and transitions.
- Hidden: comments and assignee work in the API; the UI shows them only with `-show-hidden`.

## Install and run

Download app for your OS from [Releases](https://github.com/epulla/my-tickets/releases) and open it. Linux requires GTK 3 and WebKit2GTK runtime libraries (for Ubuntu 22.04, `libgtk-3-0` and `libwebkit2gtk-4.0-37`).

On Linux, make downloaded binary executable with `chmod +x my-tickets-linux-amd64` before running it.

macOS release is unsigned. On first launch, right-click app and choose **Open**, or remove quarantine:

```sh
xattr -dr com.apple.quarantine /Applications/my-tickets.app
```

Optional flags: `-d path/to/file.db` selects database file; `-show-hidden` shows comments and assignee.

## Data

The database is `my-tickets.db` in the OS data directory:

Override with `-d path/to/file.db`.

- macOS: `~/Library/Application Support/my-tickets/`
- Linux: `$XDG_DATA_HOME/my-tickets/` or `~/.local/share/my-tickets/`
- Windows: `%LOCALAPPDATA%\my-tickets\`

Back up a board with its JSON export, or copy the `.db` file while the app is stopped.

## Build from source

Install Go and pnpm. Wails CLI version must match app version:

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
make build
```

On macOS, install Xcode command line tools. Linux also needs GTK 3 and WebKit2GTK development packages. Build output is under `build/bin/`.

## AI access (MCP)

MCP runs as `my-tickets mcp` over stdio and shares database with desktop app. It acts as Martin, the AI persona shown with an AI badge in app. Use `-d path` after `mcp` if database is not at default location.

- `create_board` accepts optional `statuses`: `[{"name":"...","type?":"classic"|"temporary","hide_after_days?":1}]`. Setting `hide_after_days` without `type` implies `temporary`; omitting both implies `classic`.

Configure client with installed executable path:

| OS | `command` | `args` |
| --- | --- | --- |
| macOS | `/Applications/my-tickets.app/Contents/MacOS/my-tickets` | `["mcp"]` |
| Windows | `C:\\path\\to\\my-tickets-windows-amd64.exe` | `["mcp"]` |
| Linux | `/path/to/my-tickets-linux-amd64` | `["mcp"]` |

Example client config:

```json
{"mcpServers":{"my-tickets":{"command":"/Applications/my-tickets.app/Contents/MacOS/my-tickets","args":["mcp"]}}}
```

For a separate database, append `"-d", "path/to/file.db"` to `args`.

App's MCP page (sidebar "MCP") shows this config with running executable path and self-tests server every 30 seconds.

## Development and tests

```sh
wails dev
make test
```

## Manual smoke test

1. Create a board and add a few tickets.
2. Drag a ticket to another column and back; history reads "Status changed from X to Y".
3. Reorder columns by dragging and with the left/right buttons.
4. Open a ticket, click **Print**, check the print preview.
5. Export the board as JSON, import it from the boards list, check the copy gets a new short name.
