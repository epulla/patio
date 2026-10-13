# Show model thinking in chats

## Request

```
is there a way to show reasoning/thinking? like having another switch button for it?
```

## Decisions

- Per-chat switch `show_thinking` (new `chat.show_thinking` column), placed next to the Thinking switch; new chats take the Settings Defaults value `show_thinking`, default off. Switch stays enabled even when the model can't think.
- Save reasoning: new `chat_message.role = 'thinking'` via a new migration that rebuilds `chat_message` (CHECK constraint); update `internal/mcp/schema.md`. Local history replay keeps excluding it (`chat_local.go:504-584` selects only user/assistant).
- Off: drivers neither stream nor save reasoning; UI hides that chat's existing thinking blocks. On again: they show.
- All six connectors: Claude (`thinking_delta`/thinking blocks, `chat_claude.go:332,351`), Codex (reasoning/summary deltas, `chat_codex.go:288`), Copilot (`agent_thought_chunk`, `chat_copilot.go:299`), OpenCode (`reasoning` parts, `chat_opencode.go:663-684`), Ollama (`message.thinking`, `chat_local.go:925`), OpenAI-compatible (`reasoning_content` + `<think>` content, `chat_local.go:1123,1363`). Show whatever the provider sends (may be summaries). Claude gets a new stream fixture test.
- UI: native `<details>` labeled "Thinking", collapsed by default, plain text (no Markdown/HTML).
- One thinking block per contiguous reasoning segment, ordered with tool lines (think → tool → think → answer = two blocks).
- Plan file deleted in last task's commit.

## Tasks

- [x] 1. Storage + shared path: migration (rebuild `chat_message` with `thinking` role, add `chat.show_thinking`), Settings default `show_thinking`, chat create/update/read expose it, shared driver emit+persist of thinking segments ordered with tools, `schema.md` updated. Check: `go test ./internal/... -count=1` incl. migration on a populated DB keeps rows, settings round-trip, a segment persisted only when on.
- [x] 2. Ollama, OpenAI-compatible, and OpenCode drivers emit thinking when on. Check: existing "private reasoning"/"hidden reasoning" tests flipped: stored as `thinking` when on, absent when off.
- [x] 3. Claude, Codex, Copilot drivers emit thinking when on. Check: per-driver fixture tests (new Claude stream-json fixture) show thinking stored when on, absent when off.
- [ ] 4. UI: per-chat Show thinking switch, Settings default switch, live + stored `<details>` blocks, hidden when off. Check: `pnpm --dir web build`; manual run shows block for a qwen3:0.6b chat.
- [ ] 5. Full verify + live smoke on installed connectors; delete this plan. Check: AGENTS.md Verify block passes; per-connector on/off result recorded in final report.
