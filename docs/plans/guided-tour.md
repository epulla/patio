# Guided tour

## Request

> regarding the Tour, I like it, but not sure if it's very simple, maybe more steps for newbies?

Ticket: PROJ-155.

## Decisions

1. Depth: guided walk across pages (board → new ticket → ticket detail → chats → skills → settings), ~19 stops.
2. Topics: create a ticket, ticket detail, chat basics, pins + dock, Patio as MCP, skills & memories.
3. No connector: same tour; AI stops say a connector is needed and offer Go to Connectors.
4. Sample ticket: if the first board has no tickets, the tour creates "Welcome to Patio" there (short checklist in the description) and walks through it. No board (assumed): board/ticket stops become one centered card that points to Create board.
5. Chat stops never create a chat: on Chats, spotlight the open chat's composer if one exists, else New chat; card text covers composer, @mentions, model picker, approvals. Pins/dock explained the same way.
6. The tour navigates by itself and returns to the starting page when it ends or is skipped.
7. Chapters as a label only: Basics · AI · Power tools in the card header; one Skip tour.
8. Frontend only; existing APIs (`POST /api/boards/{id}/tickets`, `GET /api/boards/{id}/data`).
9. This plan file is deleted in the last task's commit.

## Stops (assumed order)

- Basics: intro card · Boards nav · board columns · New ticket button · ticket card (drag between columns) · ticket due date · links · comments · New chat from ticket · Tickets nav · Calendar nav
- AI: Connectors nav · Chats (composer, @mentions, model, approvals) · pins + dock (⌘⇧N, ⌘J, groups) · Skills/Memory tabs · MCP nav
- Power tools: Monitors · Scheduled tasks · Settings → Patio as MCP · sidebar foot (settings, notifications, ⌘/, replay tour)

## Tasks

- [x] **1. Engine.** Stops get optional `path` + `chapter`; the tour navigates, waits up to ~1.5 s for the target, falls back to a centered card, and returns to the start page on close. Check: `pnpm build`; the current 10 stops still work.
- [x] **2. Anchors + sample ticket.** `data-tour` on board columns, New ticket, ticket card, ticket due/links/comments/new chat, Chats new chat/composer, pin button, Skills tabs, Settings MCP status; create the sample ticket when needed. Check: `pnpm build`; fresh DB with a board gets exactly one sample ticket even when the tour is replayed.
- [x] **3. Content.** All ~19 stops in en + es, chapter labels, connector nudge. Check: screenshots of every stop on a fresh DB (no connector) and with a connector; no stop off screen at 760 px width.
- [ ] **4. Ship check.** `go test ./...`, `wails build`, user runs the tour; delete this plan file. Check: user confirms.
