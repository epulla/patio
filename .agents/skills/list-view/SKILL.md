---
name: list-view
description: Conventions for list pages in the my-tickets web UI (web/src) such as Skills, and any new list of user-managed items (servers, templates, presets). Use when building or changing a page that lists items with enable/disable, edit, remove, sort, or a create flow.
---

# List view conventions

Reference implementation: `web/src/Skills.tsx`, plus the shared `web/src/Menu.tsx` and `web/src/Switch.tsx`. Copy its shape; don't invent a new layout.

## Before building, ask the user

Ask in one message, with defaults, before writing code:

1. **Pagination?** Will this list grow past ~50 items? Default: no pagination; client-side sort only. If yes: server-side `?limit=&offset=` (or a cursor for timestamp-sorted lists), page size 25, "Previous / Next" buttons under the list with "1–25 of N", and the sort applied on the server so pages stay consistent.
2. **Sort options?** Default: Most recent (`created_at` DESC) and Alphabetical (`name`).
3. **Enable/disable?** Only if the item has an on/off state that changes behavior elsewhere.

## Page layout

- Heading row (`.page-heading`): `.eyebrow` section name, `<h1>`, then actions on the right: secondary actions (for example Import) first, then one `.primary` "New <thing>" button.
- One muted sentence under the heading explaining what the items do.
- `ErrorText` for load and action errors.

## Empty state

A centered `.panel` card (see `.skill-empty`, max 640 px) with an icon mark, `<h2>No <things> yet</h2>`, one sentence on what a <thing> is, an optional example, and the same actions as the heading. Never put the empty state in a narrow column.

## The list

- Toolbar above the list (`.list-toolbar`): item count on the left ("3 skills"), the sort dropdown on the right.
- One `.panel` `<ul>` with a row per item; rows are separated by `var(--border)` lines; the whole row highlights on hover.
- Row content is a button that opens the item's page (for example, `/ai/skills/<id>`): the name (monospace, bold) over the description (muted, clamped to 2 lines). Disabled items render faded.
- Right side of each row, in order:
  1. `<Switch>` for enable/disable (`showLabel={false}`, label `Enable <name>`). Never a checkbox for on/off state.
  2. `⋯` menu (`<Menu>` with `buttonClassName="icon ghost"`), items in this order: **Disable <thing>** / **Enable <thing>** (pause/play icon, toggles immediately), **Edit**, a `<hr className="menu-divider" role="separator" />`, then **Remove** (`danger-text`, asks `confirm()` first).
- Sorting happens on the client unless the list is paginated. The choice persists in `localStorage` under a singular-item key such as `skill-sort`. The sort control is a `<Menu>` with `role="menuitemradio"` + `aria-checked` items, never a native `<select>`.

## Item page (create and edit)

- Routes use the section prefix, for example `/ai/skills/new` and `/ai/skills/<id>`, with a "← <Things>" back button (`.back`) and the item name as `<h1>`.
- A `.panel` form. Save is disabled until something changed. An "Enabled" `<Switch>` sits next to Save when the item has that state.
- After **create**, go back to the list. After **save** on an existing item, stay on the page and show "Saved" briefly. After **delete**, go back to the list.
- Autofocus the first field so keyboard focus lands somewhere after navigating from a menu.

## Rules that apply everywhere

- Reuse `Menu`, `Switch`, `IconButton`, `ErrorText`, and existing CSS classes before adding new ones.
- No colored accent stripes on box edges; plain `var(--border)` borders only (AGENTS.md).
- Accessibility: every control has a visible or `sr-only` label; menus support arrow keys, Escape (returns focus to the trigger), and click-outside; respect `prefers-reduced-motion`.
- Rendered Markdown goes through `web/src/Markdown.tsx` (DOMPurify).
- Verify with `pnpm --dir web build` and `wails build`.
