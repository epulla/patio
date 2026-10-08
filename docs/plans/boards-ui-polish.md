# Boards UI polish

## Request

> I don't like the UI at all:
> - this 'Create new board' page looks odd there, maybe just presenting the boards and a '+ Create Board' button [Image 1]
> - I don't like the 'Save' & 'Delete' buttons, both are different, they don't follow the same structure. Maybe just presenting the buttons with icon + tooltip. Also, when I choose the 'temporary' option, the 'days' option shift the input layouts and 'days' label looks odd [Image 2]
> - There's no way in the board or in the 'Create Board' view that explains what a 'temporary' column is. It'd be nice in the 'Create Board' to have a explicit label saying that (also, make the default 'Done' column a temporary one), and in the 'Board' view, let's put a exclamation or '?' tooltip next to the temporary column names so when the user hovers it, a label explaining how a temp column works. Finally, for temp columns, in the 'Board' view, let's update the border to be dashed (make it slightly dashed, not too agressive)
>
> can we also make the export buttons better? [Image 1: board export bar with Excel (.xlsx), CSV, JSON backup links and description] I was thinking of having a export button (icon + tooltip) with a 'arrow-down' so it shows a custom dropdown (not native) that shows the multiple export options, but feel free to opine

## Decisions

1. Boards page shows only board grid; header has "+ Create board" (opens modal reusing `.modal` CSS; fields unchanged: name, short name, columns, template HTML) and "Import" (opens file picker directly).
2. Board details Save/Delete become `IconButton` check ("Save") and trash ("Delete board", red), same size as status-row buttons; delete keeps confirm.
3. Days input: input + "days" suffix as one bordered control; classic rows reserve same-width slot so nothing shifts. Settings and create modal.
4. Create modal callout above column rows: "Temporary columns hide their tickets from the board N days after they enter the column. Tickets are not deleted. Good for Done." Default Done row = Temporary, 1 day.
5. API/MCP default columns (no `statuses`): To Do, In Progress classic; Done temporary, 1 day. Existing boards unchanged.
6. MCP column arg `{name, type?, hide_after_days?}`, type `"classic"|"temporary"` only. type absent + hide_after_days set -> temporary; neither -> classic; temporary days default 1; invalid type -> error.
7. Board view: `Temporary` badge replaced by focusable "?" next to column name, tooltip "Temporary column: tickets hide N day(s) after entering it. They are not deleted."
8. Temp columns get `1px dashed` border, existing border color at reduced opacity; drop-target highlight overrides.
9. Custom tooltips only: extend existing `data-tip` CSS tooltip (`styles.css:346-381`), shown on hover and keyboard focus. Covers Save/Delete, "?", days input (settings + modal), Export. Remove native `title` tooltips added this session.
10. Export: board-header icon button (download + chevron-down, tooltip "Export") replaces export bar; custom menu with 3 items + one-line descriptions ("Excel (.xlsx): spreadsheet of tickets", "CSV: plain table", "JSON backup: import into my-tickets on another computer"). Esc / outside click / selection closes; arrow keys move; focus returns to button.

## Tasks

- [x] API + MCP: default Done temporary (decision 5); MCP `{name, type?, hide_after_days?}` (decision 6). Check: Go tests for default columns and MCP explicit type, inferred type, invalid type; `go vet ./... && go test ./... -count=1`.
- [ ] Settings polish: `data-tip` tooltips on hover+focus for any element (decision 9), icon Save/Delete (decision 2), non-shifting days control (decision 3), remove settings `title`s. Check: `pnpm --dir web build`; smoke: toggling Classic/Temporary doesn't move other controls, tooltips show on Tab focus.
- [ ] Board columns: "?" tooltip replacing badge (decision 7), dashed temp border (decision 8). Check: `pnpm --dir web build`; smoke: hover + Tab on "?" shows tooltip, dashed border visible, drag highlight still works.
- [ ] Export dropdown (decision 10). Check: `pnpm --dir web build`; smoke: each item downloads, Esc/outside click close, arrow keys move, focus returns.
- [ ] Boards page: header buttons, create modal with callout + Done temporary default, Import button (decisions 1, 3, 4). Check: `pnpm --dir web build`; smoke: create board via modal, Done column is temporary 1 day; import JSON backup works.
