# Faster local AI replies

## Request

> 1. it'd be nice to have it turned off but let the user to turn it on (a switch in the chat view) just in case
> 2. sounds good
> 3. yeah that would be awesome

The answers refer to three ideas: (1) turn qwen3 thinking off, (2) a bigger context, (3) suggest faster models. Idea 2 was dropped after a 16k context test ran an 8 GB Mac out of memory. Builds on `docs/plans/ai-local.md`.

## Findings (2026-10-10, Apple M2 8 GB, Ollama 0.40.2, `qwen3:4b`, 100% GPU, context 4096)

- "Say hi in 3 words" takes about 23s, because about 600 hidden thinking tokens come first (about 25 tokens/s).
- The OpenAI-compatible `/v1/chat/completions` endpoint ignored every off switch tried: `think:false`, `options.num_ctx`, and `/no_think` in the system prompt. `/no_think` in the user message got it down to 12s but it still thought. `reasoning_effort:"none"` moved the rambling into the reply (177s).
- Ollama's native `/api/chat` documents `think:false` and `options.num_ctx`. Not tested yet: the test was interrupted by the out-of-memory crash.
- The user then removed `qwen3:4b` and pulled `qwen2.5:3b` (1.9 GB) and `llama3.2:3b` (2.0 GB). Neither thinks, so decisions 1–2 matter for thinking models (qwen3, deepseek-r1) other users may pick. Context fit (decision 4) matters for all.
- Each local request sends about 24 tool schemas (9 file tools, 13 ticket tools, 2 skill tools) plus up to 40 history messages. That likely overflows 4096 tokens, and Ollama then silently drops the oldest part, including the system prompt and tools.
- Live (2026-10-11): `qwen2.5:3b` completed hello; 13.79s elapsed through response and next safety check (prompt-only timing not captured); free memory fell to 25% before write, so stopped and unloaded; no write or `llama3.2:3b` run.

## Decisions

1. Thinking is off by default. Each local chat gets a "Thinking" switch (the shared `Switch`) in the chat header next to the model picker, shown only for local chats and disabled while a reply runs. It's stored per chat in a new migration (`chat.thinking INTEGER NOT NULL DEFAULT 0`) and changed with `PUT /api/chats/{id}` `{thinking}`. When it's on, thinking text stays hidden and "Thinking…" shows as today.
2. Ollama detection: on runtime start, `GET {base_url without /v1}/api/version`. If it answers, the driver uses the native `/api/chat` (NDJSON streaming, `think` flag, `tools`, tool calls with object arguments and no ids, `tool` role results). Otherwise it keeps `/v1/chat/completions` and adds `/no_think` to the user message when thinking is off (best effort, for other qwen3 servers).
3. No bigger context: the driver never sets `num_ctx`, so memory stays at the server default.
4. Fit the default 4096 context:
   - Tool descriptions become one short line, and parameter descriptions are dropped where the name says it.
   - History is trimmed by a budget (about 4 characters per token): the system prompt, tools and the current turn come first, then the newest messages that fit in about 1,500 tokens, at most 40.
   - A test asserts the tool JSON stays under about 2,000 tokens. If it doesn't fit after trimming, local chats drop the calendar tools and `duplicate` first. That doesn't affect the CLIs.
   - On Ollama, if the response's `prompt_eval_count` is at least the context size, the app adds one note: "Conversation too long for this model's context; older messages were left out."
5. Faster model hint: the Local AI connector form and card show a short hint: "Small and fast on 8 GB: `qwen2.5:3b` or `llama3.2:3b` (`ollama pull <name>`). Both support tools; neither thinks." Text only, no pulling from the app.
6. Safety for live checks: no local model runs without the user's OK in the conversation. Runs go one at a time, with at most 4 short prompts in total, split across `qwen2.5:3b` and `llama3.2:3b` to compare speed and tool use. Thinking on/off is covered by fake-server tests only, since no thinking model is installed. Before each run, check `memory_pressure` shows at least 30% free, and unload the model afterwards (`ollama stop <model>`).
7. Commits use the caveman-commit style.

## Tasks

- [x] Driver: Ollama detection and native `/api/chat` transport, `think` per chat (migration + API field), `/no_think` fallback, tool and description trimming, history budget, truncation note. Check: Go tests with a fake Ollama (NDJSON, tool call loop, think on/off sent, approvals unchanged, truncation note) and a fake OpenAI server (unchanged behavior + `/no_think`); a size test on the tool JSON; `go test -race ./internal/api/...`; `make release`. Live (decision 6): per model, "Say hi in 3 words" (time it) and one file write with Allow; report which picks tools better.
- [x] UI: "Thinking" switch in the chat header for local chats, and the faster-model hint on the Local AI connector form and card. Check: `pnpm --dir web build`; `wails build`; smoke test: toggle the switch, send a message, check the reply is fast with thinking off.
- [x] Small-model behavior (user test 2026-10-11 with `llama3.2:3b`: "Hi!" got the reply `{}`, every greeting triggered `list_dir`, and an empty folder read as a failed tool call followed by an invented `work_folder`): a system prompt that says to reply normally and use tools only for files, the folder or tickets, plus today's date and the folder name; clear tool results (an empty folder returns "(empty folder)", errors start with "Error:"); a reply that is only a JSON object becomes a real tool call when it names a known tool, otherwise that step retries once without tools, and `{}` is never stored as a reply; tool results stored capped at 2 KB and hidden in the UI. Check: Go tests (fake Ollama: text JSON tool call, `{}` retry, greeting with no tool call in the fake, empty folder result); live with at most 2 prompts on `llama3.2:3b` ("Hi!", "how are you?"), no tool calls expected. Live first prompt timed out before completing; stopped model and did not retry.
