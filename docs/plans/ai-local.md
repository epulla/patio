# Local AI chats with file tools (phase b2c)

## Request

> Local AI connectors ... Chats: ... it should be able to do commands for searching files or whatever from the computer, maybe ripgrep, find, sed, etc.

> mmmm maybe we should add the file tools (edit, remove, search, duplicate, copy, move, etc) for the local AI without opencode

> keep that as a plan for now (write it in a plan and assumptions that I need to clarify/test/do on my own). For now, let's start c

Builds on `docs/plans/ai-shell.md` (decisions 7, 10, 13) and the chat runtime seam from `docs/plans/ai-chats-clis.md`.

## Status

Approved 2026-10-10: the user accepted all decisions as defaults and said "start b2c". Ollama 0.40.2 with `qwen3:4b` is running at `http://localhost:11434/v1`.

## For the user to do or confirm

1. Install a local server and a small model that supports tool calls, then keep the server running for live checks:
   ```sh
   brew install ollama && ollama serve
   ollama pull qwen3:4b
   ```
   As of 2026-10-09, none of Ollama, LM Studio, or llama.cpp is installed; `rg` is at `/opt/homebrew/bin/rg`.
2. Confirm or change proposed decisions 1–7 below.
3. After implementation, run the smoke test: a local connector chat streams a reply, a `write_file` shows an approval card (allow and deny), `delete` sends the file to the Trash, and a ticket tool call works.

## Decisions

1. Ticket tools: the local driver starts `my-tickets mcp -d <db>` and connects to it as an MCP client (go-sdk client over stdio), giving it the same tools as the CLIs. This avoids the `api` ↔ `mcp` import cycle.
2. History: the server keeps no session, so each turn re-sends the last 40 saved user and assistant messages plus the current turn's tool calls and results. Older tool results aren't re-sent.
3. File tools, all relative to the chat workdir, refusing anything that resolves outside it (`EvalSymlinks` + separator-boundary prefix check):
   - `list_dir`, `read_file` (text only, 256 KB cap), `search` (`rg`, falling back to a Go walk; 200-hit cap).
   - `write_file`, `edit_file` (exact find and replace, which must match exactly once), `move`, `copy`, `duplicate` (`name copy.ext`).
   - `delete` sends to the OS trash: macOS `osascript` Finder delete, Windows PowerShell `Microsoft.VisualBasic.FileIO.FileSystem.DeleteFile(..., SendToRecycleBin)`, Linux `gio trash`. If no trash is available it fails; it never deletes permanently.
4. Approvals: reads run without asking. Write, edit, move, copy, duplicate, and delete go through `requestApproval` (the existing card). Ticket tools run without asking.
5. Transport: `POST {base_url}/chat/completions` with `stream: true` (SSE) and OpenAI `tools`, using the keychain key as a Bearer token. If the server rejects `tools`, retry once without them and add a note: "This model doesn't support tools; chatting without them."
6. Limits: Stop cancels the request context. At most 20 tool calls per turn; tool output sent back to the model is capped at 32 KB.
7. Live checks use the local model only (no paid quota).

## Tasks

- [x] File tool package (`internal/filetools`): sandboxed path resolution, the 9 tools, OS trash. Check: table-driven Go tests including symlink escape, sibling prefix, edit must match once, and trash command argv per OS; `make release`.
- [x] Local driver: OpenAI-compatible streaming client with a tool loop, history rebuild, MCP client to `my-tickets mcp`, approvals for writes, stop, and the no-tools fallback. Check: `httptest` fake server streaming SSE with tool calls; live with Ollama.
- [ ] Chats UI: allow `local` connectors in New chat and show the model from the connector. Check: `pnpm --dir web build`; `wails build`; smoke test (see "For the user to do or confirm", item 3).
