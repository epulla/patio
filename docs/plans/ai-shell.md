# AI shell: sidebar, settings, tickets search

## Request

> I wanna do the following feature: - new feat (AI connectors): Claude, ChatGPT connectors, Local AI connectors and available MCP for those connectors (stdio for now). It should have a settings page (for now, it only shows general (nothing for now, just a hello page, mcp status). The sidebar should have "Boards", "Tickets" (for ticket search), "AI" dropdown: "Connectors", "Chats", "Skills", "MCP". Connectors: can have multiple Claude & ChatGPT connections and should show the usage quota. Chats: simple chats with text as Claude Desktop (minimal MVP) (follow up would be file submission: text-based, image) (it should be able to do commands for searching files or whatever from the computer, maybe ripgrep, find, sed, etc). Skills: custom skills. MCP: external MCP connectors

## Decisions

This plan is phase (a) of 3. Phase (b) is connectors and chats. Phase (c) is skills and external MCP. Each gets its own plan file.

Phase (a):

1. Sidebar order: Boards, Tickets, Calendar, then the AI section (Connectors, Chats, Skills, MCP), then "Your boards".
2. The AI section is a labeled group like "Your boards", always open, not a dropdown. Four items don't need hiding, and a dropdown adds a click and a stored state for no gain. When the sidebar is collapsed, the four icons show behind a divider. (Proposed in reply to "better UI/UX?"; approved together with the task list.)
3. Settings: a gear button pinned to the bottom of the sidebar next to the theme toggle, opening `/settings`. Sections: General (hello placeholder) and MCP status (the current `Mcp.tsx` content). `/mcp` redirects to `/settings`.
4. `/tickets` searches all boards through `GET /api/tickets/search`. Results show code, title, board and status. Clicking a result opens `/tickets/:id`.
5. `/ai/connectors`, `/ai/chats`, `/ai/skills` and `/ai/mcp` are placeholder pages in this phase.

Carried to phases (b) and (c):

6. Claude, ChatGPT and opencode connectors drive the official CLIs as subprocesses (`claude -p`, `codex exec`, `opencode run`) on the user's own subscription login. Each extra account is a separate config dir (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, and opencode's equivalent). No direct OAuth token reuse.
7. Local AI: one OpenAI-compatible base URL, plus a model and an optional key. No quota. No opencode install required. It uses our own Go client with a tool loop, which gives it the ticket tools and the file tools from decision 10.
8. Quota: show whatever usage or limit data each CLI or provider exposes, and "unknown" when there is none. Confirm the real sources during phase (b).
9. Secrets go in the OS keychain via `zalando/go-keyring`. SQLite stores only references.
10. File tools differ by connector type:
   - **CLI connectors (Claude, Codex, opencode):** use each CLI's own built-in tools and permission system. The working directory is an allowed folder. Approvals go through our UI wherever the CLI supports that (Claude `--permission-prompt-tool`, the opencode serve API, the Codex app-server); confirm in phase (b).
   - **Local AI:** uses our own tools: `list_dir`, `read_file`, `search` (`rg` with a Go walk fallback), `write_file`, `edit_file` (find and replace), `move`, `copy`, `duplicate`, `delete` (sends to the OS trash). They work only inside allowed folders, and every call needs approval, with no "always allow".

   All chats get the my-tickets ticket tools (`my-tickets mcp` for CLIs, in-process for local AI). History is stored in SQLite, and replies stream to the UI.
11. Skills use the `SKILL.md` format (name, description, markdown body), stored in the DB and editable in the UI. The system prompt lists each skill's name and description, and the model loads a body through a `load_skill` tool.
12. External MCP supports stdio (command, args, env, enabled) and streamable HTTP with browser OAuth (redirect to a local port, tokens in the keychain). Each server shows its health and tool list. Tools go to Chats behind the same approval as item 10. Anthropic-hosted connector URLs may refuse third-party clients.
13. CLI setup: Settings → Connectors detects whether each CLI (claude, codex, opencode) is installed and shows a single install command or link. A "Log in" button runs that CLI's login flow. We bundle no CLIs. Bundling (opencode MIT, Codex Apache-2.0) was considered and rejected because of app size, per-platform binaries, signing and update burden.

## Tasks

- [x] Sidebar restructure (decisions 1, 2, 5) with placeholder AI pages and the settings gear pinned to the bottom. Check: `pnpm --dir web build`; smoke-test that every nav item routes and that collapsed mode shows the AI icons.
- [ ] `/settings` page with General and MCP status sections, plus the `/mcp` redirect (decision 3). Check: `pnpm --dir web build`; `go vet ./... && go test ./... -count=1`; smoke-test that MCP status shows Healthy.
- [ ] `/tickets` search page across all boards (decision 4). Check: `pnpm --dir web build`; smoke-test that a search returns tickets from 2 boards and a click opens the ticket.
