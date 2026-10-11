# Thinking switch on every chat, Tools switch removed

## Request

> this switch should go for every chat, it doesn't matter the connector

The user confirmed this means both Tools and Thinking, then on review: "3. wait, this doesn't make sense, maybe we can remove the tool switch, what do you think?" and "7. maybe Tool can be skiped but I need your opine here". The agreed outcome: Thinking goes on every chat, and the Tools switch is removed everywhere. Builds on `docs/plans/local-ai-speed.md` and `docs/plans/chat-auto-allow.md`.

## Findings (read-only research: claude 2.1.296, codex 0.162.0, copilot 1.0.95, opencode 2.0.26)

| CLI | Thinking | Applies |
|---|---|---|
| Claude | `MAX_THINKING_TOKENS=0` env; some models always think | at process start |
| Codex | `turn/start` `effort`; unknown values error | per turn |
| Copilot | `--reasoning-effort <level>` | at process start |
| opencode v2 | model `variant` (model-specific names) | per session |
| opencode v1 | none | none |

Tools off can't be done cleanly on the CLIs: Codex can't remove its file-edit tool, and opencode only blocks calls, so the model still sees its tools. The Tools switch was added for `llama3.2:3b`, which called tools on every greeting. `qwen2.5:3b` doesn't, and the improved system prompt plus Auto-allow already cover the need.

## Decisions

1. The Tools switch is removed everywhere: chat header, Settings → Defaults, API (`tools` on chats and defaults), and the local driver (tools are always offered, except the existing automatic fallback for models without tool support). A new migration drops `chat.tools`, and the `tools` key is deleted from `app_setting`.
2. The Thinking switch shows on every chat next to Auto-allow, disabled while a reply runs. Settings → Defaults drops "(Local AI chats)" from its label.
3. When Thinking changes, the next message restarts that chat's CLI and resumes the same CLI session (Claude `--resume`, Copilot `session/load`), like a model change does today. Codex applies it per message with no restart.
4. Thinking per CLI:
   - Claude: off sets `MAX_THINKING_TOKENS=0`; on leaves the default. The tooltip notes that some models always think.
   - Codex: off sends the lowest effort the model lists (from the existing models call); on sends nothing (the model default).
   - Copilot: off starts with `--reasoning-effort low`; on leaves the default.
   - opencode: the switch is disabled with a tooltip ("opencode picks reasoning with model variants").
   - Local AI: unchanged.
5. Thinking's default becomes **on**, so CLI chats keep today's full reasoning. A migration sets `thinking=1` on existing non-local chats; existing local chats keep their value. Local models without thinking (`qwen2.5:3b`, `llama3.2:3b`) aren't affected.
6. Unsupported combinations never fail a chat. They show a disabled switch with the reason, from a `capabilities.thinking` field (boolean + reason) per connector kind.
7. Live checks: 2 tiny prompts ("Reply OK") with Thinking off, one on Codex and one on Copilot, to confirm each CLI accepts the new effort setting. A wrong value there fails the chat; Go tests can only check the arguments we send, not that the CLI accepts them. Claude's env var can't break startup and opencode is disabled, so they're skipped.
8. Commits use the caveman-commit style.

## Tasks

- [ ] Backend: drop `chat.tools` and the `tools` default; Thinking default on plus the migration for existing non-local chats; Claude env, Copilot flag and Codex per-turn effort; restart-on-change with resume for Claude and Copilot; `capabilities.thinking` per connector kind. Check: Go tests per driver for the exact env, args and effort sent for each value, restart on change with resume, migration; race; `make release`; live per decision 7.
- [ ] UI: remove the Tools switches; Thinking on every chat, disabled with the reason when unsupported; Defaults label. Check: `pnpm --dir web build`; `wails build`; user smoke test on a Claude chat with Thinking off.
