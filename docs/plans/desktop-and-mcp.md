# desktop-and-mcp

## Request

> I have built a initial app with Go + React. However, there are things that I wanna change. I don't want to access the app via browser (localhost), I want the app to be a desktop app, however I would like to have a server (maybe a MCP, or some API) for giving any AI access to perform any actions like creating tickets, creating boards, search for a ticket, move ticket status, etc

## Decisions

1. **Desktop shell.** Wails v2 (native OS webview, one Go binary, no sidecar). Chosen over Tauri because Tauri would need a Rust shell plus the Go backend as a sidecar process. Wails needs CGO on macOS/Linux, so the AGENTS.md rule becomes "CGO only for the desktop build". Releases are built per OS in a GitHub Actions matrix instead of cross-compiled from one machine. Linux needs `webkit2gtk`.
2. **AI interface.** MCP over stdio as subcommand `my-tickets mcp`. The AI client spawns it, and it opens the same SQLite DB (WAL + busy_timeout handle concurrency). Works whether the desktop app is open or not. No network port.
3. **MCP implementation.** Official `github.com/modelcontextprotocol/go-sdk` (pure Go). Tools call the existing `api` handler in-process, so history/transition/transaction invariants stay in one place.
4. **Tool set.** `list_boards`, `get_board` (statuses, types, tickets), `create_board`, `create_ticket`, `get_ticket` (id or code), `update_ticket`, `move_ticket` (target status by name or id), `search_tickets` (title/code, one board or all), `add_comment`. No delete tools, no status/type CRUD. Calendar tools live in `calendar.md`.
5. **AI acting user.** New migration seeds user `ai`. MCP writes as `ai`, so history and transitions record the actor.
6. **Live refresh.** Desktop process checks `PRAGMA data_version` every 1s on a dedicated connection. On change it sends a Wails event and the UI refetches the current view. No SSE, no UI polling.
7. **Flags and browser.** Remove browser auto-open, `-port`, `-no-browser`. Wails AssetServer serves the existing mux, so no TCP port is opened. Keep `-d`, `-show-hidden`. Print must work in the webview; if `window.print()` fails on a platform, open the print route in the system browser instead.

## Tasks

- [x] 1. Acting user + server search: migration seeds user `ai`; `api.New` takes the acting user id instead of hard-coded `local`; add `GET /api/tickets/search?q=&board_id=` (title/code `LIKE`, optional board, sorted by board then number). **Check:** `go test ./... -count=1` with tests: ticket created/moved by an `ai` server has `reporter_id`/transition `user_id` = `ai`; search finds by partial title and code, scoped and unscoped.
- [x] 2. MCP subcommand: `my-tickets mcp [-d path]` serves the 9 tools over stdio via go-sdk, each calling the `api` handler in-process as user `ai`; tool errors return the API error text. README gets a client config snippet. **Check:** `go test ./... -count=1` with an in-memory MCP transport test: list tools = 9; create_board → create_ticket → move_ticket by status name → get_ticket shows new status, 1 history row, 2 transitions by `ai`; search_tickets finds it.
- [x] 3. Wails shell: replace listener + browser open with `wails.Run`, AssetServer handler = existing mux (`/api/*` + SPA); drop `-port`/`-no-browser`; `mcp` subcommand branches before Wails starts. **Check:** `wails build` on darwin produces an app; smoke with `-d "$(mktemp -d)/test.db"`: create board, add ticket, drag column, print preview opens; `lsof -p <pid> -i` shows no listening TCP port.
- [x] 4. Live refresh: `data_version` watcher goroutine emits `db-changed`; `App.tsx` subscribes and refetches the current view. **Check:** Go test: write via a second `db.Open` connection → watcher callback fires within 2s. Manual: app open, `create_ticket` via `my-tickets mcp` → card appears in under 2s without clicking.
- [ ] 5. Release + docs: Makefile targets for `wails build`, GitHub Actions matrix (macOS arm64/amd64, Windows amd64, Linux amd64 with webkit2gtk), AGENTS.md (CGO rule, layout, verify steps), README (desktop install, MCP setup). **Check:** `go run github.com/rhysd/actionlint/cmd/actionlint@latest` clean; `make` desktop target builds on darwin; `gofmt -l internal` empty.

## Risks

- Task 3: print goes through `POST /api/print` -> `runtime.WindowPrint` (WKWebView ignores `window.print()`). Print dialog and drag were not verified automatically (no screen access); verify manually.

- Windows Wails builds use the GUI subsystem. stdio pipes still work when an MCP client spawns the exe, but verify `my-tickets.exe mcp` on Windows in CI.
- macOS `.app` bundle: the MCP config must point to `My Tickets.app/Contents/MacOS/my-tickets`. Document this in README.
