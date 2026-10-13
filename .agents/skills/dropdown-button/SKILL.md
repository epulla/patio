---
name: dropdown-button
description: Conventions for dropdown buttons and split buttons in the Patio web UI (web/src); use when adding a button that opens a menu of options, or an action button with a chevron menu of alternatives.
---

# Dropdown and split buttons

Reference: `Menu` in `web/src/Menu.tsx`, `SplitButton` in `web/src/SplitButton.tsx`, and `.menu`, `.menu-list`, `.split-button`, `.split-toggle` rules in `web/src/styles.css`. Examples: row actions in `web/src/Skills.tsx` and split layout in `web/src/Chats.tsx`.

## Dropdown buttons

Use `Menu` when a button opens a list of actions or choices:

```tsx
<Menu label="More actions" buttonClassName="icon ghost" button={<Icon name="more" />}>
  {(close) => (
    <button role="menuitem" onClick={() => (close(), doAction())}>
      <Icon name="pencil" /> Edit
    </button>
  )}
</Menu>
```

`Menu` takes `label`, `button`, optional `buttonClassName`, and `children(close)`. For an icon-only trigger, use `buttonClassName="icon ghost"`; `label` becomes its `aria-label`. Use `chevronDown` for a choice menu or `more` for overflow actions. A text trigger can show its label with a chevron, for example `<button>Sort <Icon name="chevronDown" /></button>`.

Menu contents are buttons with `role="menuitem"` and optional `Icon`. Call `close()` in each item's `onClick`. Use `close(false)` when opening a dialog that takes focus, so closing the menu does not steal focus back to its trigger. `Menu` handles ArrowUp/ArrowDown navigation and Escape; don't reimplement those keys.

## Split buttons

Use `SplitButton` when one option is the common default and alternatives belong behind a chevron:

```tsx
<SplitButton icon="columns" label="Split side by side" menuLabel="Split options" onClick={splitSideBySide}>
  {(close) => (
    <>
      <button role="menuitem" onClick={() => (close(), splitSideBySide())}>Side by side</button>
      <button role="menuitem" onClick={() => (close(), splitStacked())}>Stacked</button>
    </>
  )}
</SplitButton>
```

Props are `icon`, `label`, `onClick`, `menuLabel`, and `children(close)`. Main half runs fixed default action; chevron half opens alternatives. Include default among menu items too. Choosing an item does not change default action.

Split control looks like one control: shared background on hover/focus, divider appears on hover, and each half darkens separately. Chevron half is 24px wide—the one exception to 40×40 icon-button size. Main half is currently icon-only; if text main half becomes necessary, extend `SplitButton` rather than hand-rolling one.

## Avoid

- Don't hand-roll two adjacent buttons to mimic a split button.
- Don't use native `<select>` for actions.
- Don't use `title=` tooltips; use accessible labels and shared tooltip conventions.
- Don't add colored accent stripes (AGENTS.md).

## Checklist

- Uses `Menu` or `SplitButton` for the matching interaction.
- Trigger has a clear accessible `label`; menu has a descriptive `label`/`menuLabel`.
- Menu actions use `role="menuitem"` and close the menu after selection.
- Dialog-opening menu action calls `close(false)` when focus should stay in dialog.
- Split menu includes default action; main default remains fixed after a choice.
- Arrow keys and Escape work through built-in `Menu` behavior.
- No `title=`, native action `<select>`, hand-rolled split, or colored accent stripe.
