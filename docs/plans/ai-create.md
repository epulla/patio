# Create boards and tickets with AI

## Request

> new feature: if the user has a connector already set, then when creating a `+ New Ticket` or `+ New Board` (update label from `+ Create board`), there should be 2 tabs: the manual form and the AI chat which gives the user a simple template for creating a board/ticket. The user can continue the chat there tho until it clicks outside or done. The chat is saved as usual. If the user has only one connector, let's open the chat with it, if there are multiple, let's do the same as when creating a chat (let the user choose with the default connector as the selected one)

Ticket: PROJ-168.

## Decisions

1. Tabs "Form" and "Ask AI" only when at least one connector exists; otherwise the form as today.
2. AI tab starts with a prefilled draft (not sent): a short fill-in template for a board, or for a ticket on the current board (and column, when opened from a column +).
3. Chat is created only when the AI tab is first opened: one connector → created directly with the chat defaults; several → the New chat dialog with the default connector preselected. Closing without having sent a message deletes the chat; otherwise it is kept like any chat.
4. Default tab: Form the first time, then the last tab used (per dialog kind, localStorage).
5. Scope: New board dialog (button relabeled "+ New board"), board header "+ New ticket", column + buttons.
6. Closing: Done, Esc or click outside. While open, anything created since the dialog opened shows as "Created ME-12 · Open" / "Created board Work · Open"; Open closes the dialog and navigates there.
7. Ticket chats get the board as page context, like the side panel does.
8. This plan file is deleted in the last task's commit.

## Tasks

- [x] **1. AI tab component.** Shared tab: lazy chat creation (decision 3), embedded `Conversation` with the prefilled draft, delete-if-unused on close, "Created … · Open" list fed by a `created` prop. Check: `pnpm build`.
- [x] **2. New board.** Tabs in `CreateBoard` (BoardList), label "New board" (en + es), remembered tab, created boards = boards added since the dialog opened. Check: headless browser with a local connector: tabs show, draft prefilled, a board created via API while open appears as "Created board … · Open", closing an unused chat deletes it.
- [x] **3. New ticket.** Same for `NewTicket` (header button + column +), template names board and column, created tickets = tickets on the board created since opening. Check: same as task 2 for tickets, from both entry points.
- [ ] **4. Ship check.** `go test ./...`, `wails build`, screenshots in Chrome and WebKit, user tests with a real connector; delete this plan file. Check: user confirms.
