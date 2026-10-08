# UI polish

## Request

> i want you to explore the UI, the look and feel. I want you to propose fixes/adjustments so the UI is better. I prefer buttons with icon + tooltip (opine here). Finally, there's a 'Delete ticket' button that I don't like (remove it)

## Decisions

1. Delete ticket: remove the button (`TicketDetail.tsx:242`) and its `remove()` handler. Keep `DELETE /api/tickets/{id}`. Temporary statuses handle crowding; see `temp-status-ttl.md`.
2. Icons: hand-written inline SVG components in `web/src/icons.tsx` (Lucide-style paths, MIT). No new dependency.
3. Icon-only + tooltip: sidebar collapse, theme toggle, column move/add, add ticket, modal close, calendar prev/next/add, status/type row save/delete. Icon + text: export links (all share the download icon, so icon-only can't tell them apart), sidebar nav items (keep native `title`; nav `overflow-y: auto` clips CSS tooltips), Create board, Create ticket, Save board, Delete board, Add comment, Print, Back.
4. Tooltip: CSS-only, from a `data-tip` attribute that mirrors `aria-label`. Shown on `:hover` and `:focus-visible`.
5. Native dialogs: replace `prompt("Column name")` with an inline input. Keep `confirm()` for destructive actions and add it to status/type delete.
6. Polish fixes:
   - define the missing `.sidebar-foot`, `.search` and `.print-loading` styles
   - one danger style: solid for whole-entity deletes, text for row deletes
   - keyboard-accessible ticket cards
   - NewTicket modal gets `role="dialog"`, `aria-modal` and closes on Escape
   - raise muted contrast and set an 11px minimum text size
   - one h2 size
   - calendar "+" faintly visible instead of hidden
7. `PrintTicket.tsx:68` `res.ok || window.print()` is intentional: WKWebView ignores `window.print()`, so Go prints natively (`main.go:73-77`). The browser fallback runs only when the request fails. Out of scope.

## Tasks

- [x] 1. Remove the Delete ticket button and `remove()` from `TicketDetail.tsx`. Check: `rg "Delete ticket|confirm\(\"Delete this ticket" web/src` is empty; `pnpm --dir web build` passes.
- [x] 2. Add `icons.tsx` and the `data-tip` tooltip CSS; convert the buttons listed in decision 3. Check: `pnpm --dir web build` passes; `rg "[＋×«»☀☾]" web/src/*.tsx` is empty; every icon-only button has `aria-label` and `data-tip`.
- [x] 3. Replace the add-column `prompt()` with an inline input; add `confirm()` to status/type delete. Check: `rg "prompt\(" web/src` is empty; build passes; smoke-test add column and delete status/type in a throwaway DB.
- [ ] 4. Apply the polish fixes in decision 6. Check: build passes; `rg "font-size: ?(9|10)px" web/src/styles.css` is empty; smoke-test Tab into a ticket card, Enter opens it, Escape closes the NewTicket modal, both themes look right.
