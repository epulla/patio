# PROJ-36: New chat from the ticket view

## Request

Add a **New chat** button to the ticket view that creates and opens a fresh chat for that ticket. Prefill a draft with the ticket code, title, and link so the user can review before sending. Link the new chat back to the ticket without replacing existing chat links.

## Decisions

1. Button in the ticket header (`.ticket-links`, `web/src/TicketDetail.tsx` ~L118). One connector: create the chat directly with saved defaults. Several: open the existing `NewChatDialog`, exported from `web/src/Chats.tsx`.
2. Chat title `<CODE>: <title>` (sent as `title` to `POST /api/chats`). Draft `[<CODE>: <title>](/tickets/<id>)` + blank line, written to localStorage `chat-draft:<chat id>` before navigating. Nothing is sent.
3. Link back: append `**Chat:** [<chat title>](/ai/chats/<chat id>)` to the description's first links line, same rules as the `ticket-chat-link` skill (add after the first link, append with `, ` if a Chat entry exists, create the line if missing). Rest of the description byte-for-byte.
4. Repeated clicks create one chat (in-flight guard). Cancel/failure stays on the ticket and shows the error.
5. This plan file is deleted in the last task's commit.
6. Follow-up after review: the ticket goes in as a context chip (same as the dock's page chip), not as draft text. Chip `🎫 <CODE>`, toggleable; line `Working on ticket [<CODE>: <title>](/tickets/<id>)`; sent with the first message, then the chip is gone; message box starts empty; stored in localStorage `chat-context:<chat id>`; wins over the dock's page chip until the first send.
7. Follow-up: the new chat opens pinned in the side panel (appended to the pins, dock opened and focused), staying on the ticket. All 9 pin slots taken: open the chat view as before.

## Tasks

- [x] 1. Shared chat creation: export `NewChatDialog`, `createChat` (with optional `title`) and a `newChatWithDefaults` helper from `Chats.tsx`; Chats page keeps working. *Check:* `pnpm build`; Chats `+` still creates a chat.
- [x] 2. Description link helper `addChatLink(description, title, chatID)` in a small module, applying Decision 3. *Check:* `pnpm build` + the cases (no links line, links line without Chat, with Chat, already linked) verified with a quick `node` script run against the built function.
- [x] 3. Button in `TicketDetail.tsx`: create (direct or via dialog), save draft, update the ticket description, navigate to `/ai/chats/<id>`; in-flight guard; errors via `ErrorText`. *Check:* `pnpm build`.
- [x] 4. Ticket context chip instead of draft text (Decision 6). *Check:* `pnpm build`.
- [x] 5. Open pinned in the side panel (Decision 7). *Check:* `pnpm build`.
- [ ] 6. `wails build`; user checks: one click opens, pinned in the side panel (chat view when pins are full), a chat titled `<CODE>: <title>` with the ticket chip and an empty message box, the ticket shows the new Chat link next to existing ones, double click makes one chat, Cancel in the dialog stays on the ticket. Delete this plan file.
