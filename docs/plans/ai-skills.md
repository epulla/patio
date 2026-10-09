# Skills (phase c2)

## Request

> Skills: custom skills

Builds on `docs/plans/ai-shell.md` (decision 11) and `docs/plans/ai-mcp.md` (decision 8).

## Decisions

1. Fields follow `SKILL.md`: `name` (lowercase letters, digits, and hyphens; 1–64 characters; unique), `description` (required, 1–1024 characters; what the model sees), and `body` (Markdown, up to 64 KB). Plus `enabled`, `created_at`, and `updated_at`. Migration `0009_skill.sql`.
2. Import: a single `SKILL.md` file through a file picker. Name and description come from its front matter (`---` YAML block with `name:` and `description:`), and the rest is the body. Export downloads `<name>/SKILL.md` content as `SKILL.md`. Folders of scripts or extra resource files are not supported.
3. Delivery: `my-tickets mcp` gets `list_skills` (enabled names and descriptions) and `load_skill(name)` (returns the body). `load_skill`'s description embeds the enabled list, read when each chat's MCP process starts, so changes apply to new chats and to chats restarted after the idle close. No CLI-specific system prompt changes.
4. Approvals: skill tools run without asking (read-only), like the ticket tools.
5. Skills page `/ai/skills`: a list with an enable switch, plus an editor with name, description, and body with a live Markdown preview (the same renderer as chats). New, Import, Export, and Delete.
6. No starter skills ship. The empty page shows a short example.
7. Local AI (b2c) inherits the skill tools through `my-tickets mcp`.
8. Commits use the caveman-commit style.
9. Feedback round (user-approved): Skills is a list page (rows open /ai/skills/<id>), not a two-pane editor; enable/disable uses a shared Switch everywhere (Skills, MCP servers); each row has a ⋯ menu (Enable/Disable, Edit, divider, Remove); list sorts by most recent by default with a custom sort dropdown (Most recent / Alphabetical) persisted in localStorage; creating a skill returns to the list. Pagination skipped for now.

## Tasks

- [x] Storage and API: migration plus `/api/skills` list/get/create/update/delete, with `POST /api/skills/import` (raw `SKILL.md` text → parsed skill) and `GET /api/skills/{id}/export` (`SKILL.md` text). Check: Go tests covering validation (name pattern, length caps, unique), front-matter parse (quoted values, missing fields → 400, CRLF), and an export → import round-trip; `make release`.
- [x] MCP tools: `list_skills` and `load_skill` in `my-tickets mcp`, with the enabled list in `load_skill`'s description. Check: Go test with an in-process MCP server confirming the tool description lists enabled skills only, `load_skill` returns the body, an unknown or disabled name errors, and the existing tool list is otherwise unchanged; one live Claude prompt loads a skill.
- [x] Skills page UI (main agent): list, editor with preview, import/export, delete; the Markdown renderer moves to a shared module used by Chats and Skills. Check: `pnpm --dir web build`; `wails build`; smoke test: import a `SKILL.md`, edit it, and a new chat asked to use it calls `load_skill`.
