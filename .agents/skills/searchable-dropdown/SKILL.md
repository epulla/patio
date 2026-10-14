---
name: searchable-dropdown
description: Conventions for the custom searchable dropdown in the Patio web UI (web/src), currently `ModelPicker`. Use when adding or changing a model selector, or any single-choice picker whose list is long enough to need search (more than ~10 options, provider-prefixed ids, descriptions), instead of a native `<select>` or a `Menu`.
---

# Searchable dropdown

Reference: `ModelPicker` and `ModelOption` in `web/src/ModelPicker.tsx`; `.model-picker`, `.model-trigger`, `.model-pop` rules in `web/src/styles.css`. Examples: chat composer and New chat dialog in `web/src/Chats.tsx`, scheduled task form in `web/src/ScheduledTasks.tsx`.

## Which control

- Short fixed list (under ~10, no search needed): native `<select>` in forms.
- Actions or a few choices behind a button: `Menu` (see `dropdown-button` skill).
- Long or growing list of choices, model ids, anything the user would want to type to find: `ModelPicker`.

## Usage

```tsx
const { models, loading } = useModels(connectorID);

<div className="field">
  <span className="label-row">
    Model {loading && <span className="spinner" role="status" aria-label="Loading models" />}
  </span>
  <ModelPicker models={models} value={model} loading={loading} onChange={setModel} />
</div>
```

Props: `models: ModelOption[]` (`{ id, label, description? }`), `value`, `loading`, optional `disabled`, optional `up` (open upward and right-aligned; use at the bottom of the screen, e.g. the composer), `onChange(id)`. `onChange` only fires when the choice actually changes.

Always get models from `useModels` (`web/src/Chats.tsx`): it caches per connector and falls back to `Default`. A `value` missing from the list is still shown as an option, so saved models never disappear.

## Behavior (built in, don't reimplement)

- Trigger shows the current label without the `provider/` prefix; the full id is its `data-tip` tooltip. Loading shows "Loading models…" and disables it.
- Opening focuses a search box; matching is case-insensitive substring on id, label, and description.
- Ids shaped `provider/model` are grouped under a small provider header; ids without `/` form one ungrouped list.
- ↑/↓ move the active option (wraps), Enter picks, Esc closes and returns focus to the trigger, hover sets the active option, outside click or tabbing away closes.
- ARIA: trigger `aria-haspopup="listbox"`; search input `role="combobox"` with `aria-controls` and `aria-activedescendant`; options `role="option"` with `aria-selected`.
- The popup keeps focus in the search box on mouse down (except on the input itself).

## Layout rules

- Never wrap the picker in a `<label>`: clicking anywhere in the popup would re-trigger the button. Use `<div className="field">` (styled for `.create` and `.skill-editor` forms) with a text span.
- Context-specific sizing goes through a parent selector, e.g. `.composer-model .model-trigger { height: 32px; max-width: 220px; }`.
- Buttons default to font-weight 600; the trigger and popup reset to 400. Keep option rows normal weight.

## Extending

- A second kind of searchable list (not models): generalize `ModelPicker` (rename, make grouping optional) instead of copying it. One component, two callers.
- Don't add a dependency (no UI library in `web/package.json`).

## Checklist

- Uses `ModelPicker`, not a native `<select>` or `Menu`, for long model lists.
- Wrapped in `.field` div, not `<label>`; loading spinner in the label row.
- `up` set when the picker sits near the bottom of the viewport.
- No `title=` tooltips; no colored accent stripes (AGENTS.md).
- `pnpm build` passes; check search, keyboard pick, mouse pick, and disabled state in the app.
