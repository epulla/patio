# my-tickets

Local kanban ticket desktop app built with Go, Wails, React, and SQLite. Chat with AI coding CLIs you already use (Claude Code, Codex, GitHub Copilot, opencode) from inside the app, and give any AI client access to your tickets through an MCP stdio subcommand.

Port of the prod-tickets schema, plus a `ticket_status_transition` table that records every status change for future reporting.

## Features

- Boards: new boards default to To Do, In Progress (classic), and Done (temporary; tickets hide after 1 day); create, rename, change short name (old ticket codes still resolve), delete.
- Kanban: drag tickets within and between columns, reorder columns (drag or left/right buttons), add columns, temporary columns (tickets hide after 1 day), search by title or code.
- Tickets: rich-text description, ticket types, due date, status, history ("Status changed from X to Y"), status transitions.
- Export: board as Excel, CSV, or JSON backup; ticket as JSON or Markdown.
- Import: a board JSON backup imports as a new board, e.g. on another computer. A colliding short name gets a suffix (`NB` -> `NB2`).
- Print: clean printable page with history and transitions.
- Ticket search: find tickets by title or code across all boards.
- Calendar: calendar items plus tickets with due dates.
- Hidden: comments and assignee work in the API; the UI shows them only with `-show-hidden`.

## AI

The **AI** section in the sidebar has four pages. Settings (gear at the bottom of the sidebar) has General (version, database and data folder, counts, where secrets live), Defaults for new chats, allowed folders, and MCP status.

- **Connectors**: connect the official CLIs installed on your computer, signed in with your own account: Claude Code (`claude`), Codex (`codex`), GitHub Copilot (`copilot`), and opencode (v1 and v2). CLI cards show sign-in status, plan, and usage quota where the CLI or provider reports it, a **Log in** button, and an install command when the CLI is missing. Add more than one account per CLI through a separate config directory (not supported for opencode on Windows); extra Copilot accounts use a fine-grained GitHub token with the "Copilot Requests" permission. Their providers' terms apply. A Local AI connector points at any OpenAI-compatible server (Ollama, LM Studio, llama.cpp); its chats get file tools inside the chat folder (list, read, search, write, edit, move, copy, duplicate, delete to Trash) plus the ticket tools, and every file change asks first unless Auto-allow is on. Small models that fit 8 GB of RAM, such as `qwen2.5:3b`, work; Ollama is used through its native API.
- **Chats**: streaming chats through any connector, with a model picker and saved history. Replies render as Markdown; tickets and boards the AI touches are linked, and file links open in Finder/Explorer. Every write or command shows an approval card (Allow once / Deny) unless the chat's **Auto-allow** switch is on; auto-allowed actions are recorded and shown in the chat. Codex runs external MCP tools without asking. A **Thinking** switch turns extended reasoning on or off per chat (Claude, Codex, Copilot, Local AI; opencode picks reasoning through model variants). Settings → Defaults sets the connector, folder, Auto-allow (on) and Thinking (on) for new chats. A chat works inside an allowed folder you pick, or an empty scratch folder of its own. Idle chat processes close after 10 minutes.
- **MCP**: add external MCP servers, as a local command (stdio) or a remote URL (HTTP, with a token or OAuth sign-in). Each card shows health and tools; enabled servers are offered to every chat.
- **Skills**: write or import `SKILL.md` skills (name, description, Markdown instructions). Enabled skills reach every chat through the `list_skills` and `load_skill` MCP tools.

Secrets (API keys, tokens, OAuth tokens, MCP env values) are stored in the OS keychain, never in the database.

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

Chat scratch folders and extra connector accounts live in `chats/` and `connectors/` next to the database file.

Back up a board with its JSON export, or copy the `.db` file while the app is stopped. Secrets in the keychain are not part of the backup.

## Build from source

Install Go and pnpm. Wails CLI version must match app version:

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
make build
```

On macOS, install Xcode command line tools. Linux also needs GTK 3 and WebKit2GTK development packages. Build output is under `build/bin/`.

## AI access (MCP)

MCP runs as `my-tickets mcp` over stdio and shares database with desktop app. It acts as Martin, the AI persona shown with an AI badge in app. Use `-d path` after `mcp` if database is not at default location.

- Tools cover boards, tickets, comments, calendar items, and skills (`list_skills`, `load_skill`).
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

Settings page's MCP status section shows this config with running executable path and self-tests server every 30 seconds.

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
6. Connectors: add a Claude Code (or other CLI) connector on the existing login; the card shows account and plan.
7. Chats: start a chat, ask for a reply, then ask it to create a file; allow once, then deny on a second try.
8. MCP: add a server, check it turns healthy; Skills: create a skill, start a new chat, check it is used.
