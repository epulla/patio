# MCP hot reload + Markdown everywhere

## Request

> /burned-plan work on PER-1 pls. The repo is at projects/patio

> I mean, why we need to restart the whole app?? I'd prefer not restart the whole app whenever I add an MCP

> also look at the ticket description, I would like to support markdown there and in the chat inputs pls

> it'd be nice to have all stated in this session as markdown, the only reason I used Quill was for the "mention" feature, but it'd be to have a markdown thing, but not sure, give an opinion

> It'd be nice have some kind of text tools as quill but of course they will be Markdown related like title (which is `# <title>`) and so on

## Decisions

1. On MCP server create/update/delete/OAuth sign-in, restart idle chat runtimes; busy runtimes get a "restart after this turn" flag. History survives via stored session ID.
2. Applies to all chat kinds (opencode, claude, codex, copilot).
3. Before each send, if the OAuth header for any MCP server differs from the one the runtime started with, restart the runtime.
4. Restart posts a chat note: "MCP servers changed — reconnected."
5. Acceptance: Go test adding an MCP server between two sends asserts the second send starts a new runtime with the new server; plus manual check in app.
6. Delete this plan file in the last task's commit.
7. Store ticket descriptions and comments as Markdown. Replace Quill with a shared `MarkdownEditor` (textarea + Write/Preview tabs + Markdown toolbar: heading select, bold, italic, link, ordered/bullet list, code, Cmd+B/I/K).
8. Startup migration: descriptions/comments containing HTML tags are converted to Markdown; others untouched.
9. Comments get the same treatment as descriptions.
10. Chat: sent user messages render via `<Markdown>`; composer stays a plain textarea.
11. Markdown export writes stored Markdown directly.
12. Create separate Patio tickets for ticket Markdown and chat Markdown on PER-1's board.
13. Mentions out of scope; follow-up ticket. Also a follow-up ticket for Jira-style ticket links.
14. No Markdown toolbar in the chat composer.
15. Tool-call lines in chat are clickable to expand and show full content.

## Tasks

- [x] 1. MCP hot reload (items 1–5). Check: `go test ./internal/api -run 'MCP|Restart'` passes, including new test from item 5.
- [x] 2. Markdown storage: migration (item 8), export (item 11), print/render paths read Markdown. Check: `go test ./...` passes, including a migration test for an HTML row and a Markdown row.
- [ ] 3. `MarkdownEditor` with toolbar replaces Quill in description and comments; remove `react-quill-new`. Check: `npm run build` in `web/` passes; PER-1 description shows headings and lists in the app.
- [ ] 4. Chat user messages render as Markdown. Check: `npm run build` passes; a sent `**bold**` message renders bold.
- [ ] 5. Expandable tool-call and approval lines in chat; delete this plan file. Check: `npm run build` passes; clicking a truncated tool line shows full content, clicking again collapses.
