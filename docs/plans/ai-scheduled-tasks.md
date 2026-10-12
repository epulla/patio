# AI scheduled tasks

## Request

> feat (AI scheduled tasks): I want some tasks that uses one of the CLI connectors it can get get scheduled for any time in the future and also make it a recurrent job. Not sure if date inputs or cron-job text-based inputs would be good for non-technical users. Maybe having both? give any recommendation. The schedules tasks should have a sidebar button below the AI section, they should be configurable, with the 'mode' selector, schedule options, AI prompt as well. Scheduled tasks should appear in the calendar as well.

## Decisions

- Task fields: title, prompt, connector, model, folder, thinking, mode, enabled. Same pickers as the New chat dialog.
- Mode = permission mode: **Ask** (approvals wait in the run's chat with an OS notification and expire on the existing timeout) or **Auto-allow** (same as the chat switch).
- Schedule UI: presets first ("Once at date/time", "Every day at", "Weekdays at", "Weekly on days at", "Monthly on day N at", "Every N hours"), plus "Advanced (cron)" text field with a preview of the next 3 runs.
- Storage: presets compile to a 5-field cron string in local time; one-off runs store a single date/time. Parsing and next-run math use `github.com/robfig/cron/v3` (pure Go, CGO-free).
- Each run creates a new chat titled `<task> · <date time>`. The task page lists recent runs with status and a link to the chat. Chats created by a task link back to it in the Chats view. OS notification when a run finishes or fails.
- Runs only while the app is open. Missed while closed: run once at next startup, skip the rest. Previous run still going: skip.
- Labels:
  - "Ran late". Hover: "The app was closed at the scheduled time, so this ran when it opened again."
  - "Skipped". Hover: "The previous run was still working, so this one didn't start." No chat created.
  - Times missed while closed are not listed. Task page shows "Runs only while My Tickets is open."
- Sidebar: "Scheduled tasks" entry below the AI section.
- Calendar: upcoming runs in the visible range plus past runs, distinct style, not draggable, click opens the task or the run's chat. Hidden when a board filter is active.
- MCP: tools to list, create, update, and delete scheduled tasks.
- This plan file is deleted in the last task.

## Tasks

- [x] 1. Storage and API: migration `0017` (`scheduled_task`, `scheduled_task_run`, `chat.scheduled_task_id`), CRUD at `/api/scheduled-tasks`, next-runs preview endpoint, cron validation. Check: `go test ./internal/api -run ScheduledTask -count=1` covers CRUD, invalid cron, next-run times.
- [x] 2. Runner: background loop started in `OnStartup` next to reminders. Claims due runs in one transaction, creates the chat, sends the prompt with the task's mode, records "Ran late" and "Skipped", notifies on finish or failure. Check: `go test ./internal/... -run Scheduled -count=1` covers on-time, late, skipped, and failed runs with a fake runtime.
- [ ] 3. UI: sidebar entry, list page (list-view conventions), create/edit form with presets, Advanced cron, next-3 preview, run history with labels; Chats list shows the task link. Check: `pnpm --dir web build` passes, and the smoke test creates a task due in 1 minute and the run's chat appears.
- [ ] 4. Calendar and MCP: `/api/calendar` returns upcoming occurrences and past runs in range; `Calendar.tsx` renders them; MCP tools for scheduled tasks. Check: `go test ./... -count=1` covers calendar occurrences and MCP tools; `pnpm --dir web build` passes.
- [ ] 5. README section, then delete this plan. Check: full Verify list in AGENTS.md passes.
