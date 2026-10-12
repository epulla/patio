# Chat in a separate window

## Request

> Also, I would like to be able to open chats in a separate window, maybe with a button with icon in case the user wants to have multiple chats at the same time

## Decisions

- Wails v2 supports one window per process and has no external HTTP port. Approach: a second app process, `my-tickets -chat <id>`, opens a window showing only that chat.
- The second process shares the database; `db.Watch` keeps windows in sync.
- The second process does not run the scheduler or reminders.
- A reply shows up live only in the window whose process runs that chat's turn. Other windows update when the turn finishes.
- Done after `ai-scheduled-tasks.md`. This plan file is deleted in the last task.

## Open questions

Answer these before `go`:

1. Button: icon-only "Open in new window" in the chat header, next to Delete? default: yes
2. Same chat opened twice: allow a second window, or block with "Already open in another window"? default: allow
3. Same chat sending from two windows at once: second sender gets "A reply is already running in another window"? default: yes, enforced by a DB lock row
4. Window size: 900×800, title = chat title? default: yes
5. Closing the main app: leave chat windows open, or close them too? default: leave open
6. Chat window shows only the conversation (no sidebar), with a "Open in main window" link? default: yes
7. Supported on macOS, Linux, and Windows from day one? default: yes

## Tasks

- [ ] 1. `-chat <id>` flag: single-chat window mode, no sidebar, no scheduler or reminders. Check: `go test ./... -count=1` covers flag parsing; smoke test opens a chat-only window.
- [ ] 2. Header button and `POST /api/chats/{id}/window` that starts the second process with the same `-d` path. Check: smoke test opens two chats in two windows and sends in both.
- [ ] 3. Cross-process busy guard per open question 3. Check: test that a second process sending to a busy chat gets the error.
- [ ] 4. README section, then delete this plan. Check: full Verify list in AGENTS.md passes.
