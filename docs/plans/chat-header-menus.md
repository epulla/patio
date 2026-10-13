# Chat composer controls and split button

## Request

> now this (red square): I don't like it, too much noise. I do like we have those options, but it'd be better to some kind of "settings" custom dropdown per chat with those options
>
> also this: it should look as 1 button (there are 2 buttons tho), if the user hovers to the dropdown button, it gets darker or something. Pls create a skill for this dropdown button, there should be a dropdown button and button + dropdown button which has a default values when clicking the button (not the dropdown)
>
> what you think it'd be the best approach? maybe moving model and folder to the chat next to 'Send' button as icon buttons below could work as well

## Decisions

16. Composer row: `[folder icon] [settings gear]  ...  [<connector> · <model> ▾] [Send]`. Folder icon opens existing folder menu (or reveals folder after first message); gear opens Thinking, Show thinking, Auto-allow; model dropdown label includes connector name. Scheduled-task chip stays in header.
17. Settings rows show their explanation as muted text; menu stays open while toggling.
18. Warning dot on gear when Auto-allow is on.
19. Split button: one joined control with a divider; each half darkens on hover separately.
20. Shared `web/src/SplitButton.tsx`; main half runs a fixed default action; choosing a menu option does not change the default. Dropdown-only stays `Menu` with a chevron.
21. Skill `.agents/skills/dropdown-button/SKILL.md` covering dropdown and split buttons.
22. This plan file deleted in the last task.
23. Running indicator: existing `.spinner` in the Stop button for the whole run; "Working…" gets animated dots until first text; both static under `prefers-reduced-motion`.

## Tasks

- [ ] 1. `SplitButton` component + styles; chat split control uses it. Check: `pnpm --dir web build`; smoke: one joined control, halves darken separately, main click splits side by side, menu offers both.
- [ ] 2. Skill `.agents/skills/dropdown-button/SKILL.md` documenting `Menu` dropdown and `SplitButton`. Check: skill references real props/classes in `web/src` (grep).
- [ ] 3. Composer controls: move model, folder, and a settings menu (with Auto-allow dot) next to Send; remove the header meta row except the scheduled chip; running indicator (decision 23). Check: `pnpm --dir web build`; smoke: header is title + actions only, all three switches work from the gear menu, folder menu works before first message, Stop shows a spinner while running.
- [ ] 4. Verify + cleanup: full Verify block from AGENTS.md; delete this plan and `docs/plans/chat-list-paging.md`. Check: all commands pass.
