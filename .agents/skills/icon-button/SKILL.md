---
name: icon-button
description: Conventions for icon-only buttons and hover tooltips in the Patio web UI (web/src). Use when adding or changing any button that shows only an icon (header actions, toolbar buttons, row actions, open-in-window, delete, collapse), or any element that needs a hover/focus tooltip, so it gets the shared `IconButton`, an accessible name, and the custom `data-tip` tooltip instead of a native `title`.
---

# Icon buttons and tooltips

Reference: `IconButton` and `Icon` in `web/src/icons.tsx`; `[data-tip]` rules in `web/src/styles.css`. Examples: the chat header in `web/src/Chats.tsx` (Open in new window, Delete chat) and the sidebar collapse button in `web/src/App.tsx`.

## The component

```tsx
import { IconButton } from "./icons";

<IconButton className="ghost" icon="externalLink" label="Open in new window" onClick={open} />
<IconButton className="danger-text" icon="trash" label="Delete chat" onClick={remove} />
```

`label` becomes both `aria-label` (screen readers) and `data-tip` (the visible tooltip), so one string covers both. Write it as a short action in sentence case: "Delete chat", not "Delete" or "delete-chat". Pass the type explicitly (`type="button"`) inside forms so it doesn't submit.

Don't hand-roll `<button><Icon /></button>` for an icon-only action, and don't use `title=` for tooltips; the native tooltip is slow, unstyled, and duplicates the label for screen readers.

## Icons

Icons are Lucide paths (ISC) in the `paths` map in `icons.tsx`, drawn on a 24×24 stroke grid. To add one, copy the path data from the matching Lucide icon and give it a camelCase key. Check it renders at 16px before shipping; hand-edited paths tend to look broken.

## Styles

- Variant classes go in `className`: `ghost` for quiet header and toolbar actions, `danger-text` for destructive ones. `icon` is added automatically.
- Size: every icon button is a 40×40 square (`.icon` sets `width: 40px` and `flex-shrink: 0`). Don't override width, height, or padding, and don't put it in a flex container that stretches or squashes it (e.g. `flex: 1` on the button in a column). Icon-only nav items in the collapsed sidebar follow the same 40×40 size and the button radius (`--radius-field`).
- Tooltip placement: below and centered by default. Add `tip-up` when the button sits near the bottom of a scroll area, and `tip-wide` for text longer than a few words (wraps at 260px). Inside `.sidebar`, the tooltip appears to the right.
- The tooltip shows after 0.4s hover, and right away on keyboard focus.
- For a tooltip on something that isn't an icon button (a help mark, a chip), set `data-tip="..."` on the element and keep a real accessible name on it.

## Checklist

- Uses `IconButton`, not a custom button.
- Renders as a 40×40 square in every layout, including collapsed sidebars and scrolling columns.
- `label` is a clear action and matches what the button does.
- No `title=` on the same element.
- Reachable by keyboard; tooltip visible on focus.
- No colored accent stripes (AGENTS.md).
