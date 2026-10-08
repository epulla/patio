# Settings buttons, export label, column reorder

## Request

> - I don't like the 'Circle' button [Image 1: round bordered check button next to red trash icon]
> - let's add a label saying 'Export' to the export button and remove tooltip
> - there should be an option for moving statuses/columns in the board settings as well (maybe a column preview with arrows at the bottom?)
> -

## Decisions

1. `IconButton` becomes a rounded square (input corner radius, 40x40) with border and background like other buttons, app-wide. Check: normal color. Trash (`danger-text`): red icon, red-tinted border, not ghost.
2. Ghost stays where it reads better (assumed): board column move arrows, dialog/modal Close X and new-column Cancel X buttons, sidebar collapse + theme toggle, calendar day add button. Calendar prev/next arrows stay bordered because they sit in a joined group with "Today". Everything else bordered.
3. Export button: download icon + "Export" text + chevron, no tooltip.
4. Settings Columns section gets a preview strip at the bottom: small column cards (name, dashed border if temporary) with left/right buttons under each; first left / last right disabled; click saves via `PUT /api/boards/{id}/status-order`.
5. After a reorder, settings column rows reset to saved values (unsaved edits discarded).
6. Request has only these 3 items; the empty 4th bullet is nothing.

## Tasks

- [x] Rounded-square `IconButton` app-wide, ghost per decision 2, trash bordered red. Check: `pnpm --dir web build`; code review: no `border-radius: 999px` on `.icon`, ghost list matches decision 2.
- [x] Export button label, no tooltip. Check: `pnpm --dir web build`; menu still opens/closes with keyboard.
- [x] Settings column preview strip with move buttons; rows reset after move. Check: `pnpm --dir web build`; `go test ./... -count=1`; code review confirms order saved via status-order endpoint and rows remount after move.
