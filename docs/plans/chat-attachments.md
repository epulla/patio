# Chat attachments

## Request

> plan attachments

Answers to the questions: 1 default, 2 "yes, pls create a custom icon for this (same approach as sidebar icons)", 3 yes, 4 default, 5 "default, remember this has to work for Windows + Linux as well", 6 ok (live checks approved). Builds on `docs/plans/ai-chats-clis.md`, `docs/plans/ai-local.md`, `docs/plans/chat-auto-allow.md`.

## Decisions

1. Allowed: text/code files up to 1 MB (valid UTF-8, no NUL bytes) and images PNG, JPEG, GIF, WebP up to 10 MB (type detected from content, not the extension). At most 10 files per message. PDFs and Office files are rejected with a clear error. Limits are enforced by the server, and the UI checks them first.
2. Attach with a paperclip button next to Send, drag and drop onto the chat, or paste. The paperclip is a new `paperclip` entry in `web/src/icons.tsx` (Lucide path, same as the sidebar icons), with no emoji. Pending files show as chips above the message box, each with a remove button. Pasted images without a name become `pasted-<local time>.png`.
3. Files are uploaded as bytes (`multipart/form-data`, `POST /api/chats/{id}/attachments`). The HTML5 File API is used, never native OS paths, so drop and paste behave the same in WKWebView (macOS), WebView2 (Windows) and WebKitGTK (Linux). Dropping a file anywhere else in the window is prevented, so the webview never navigates to it.
4. Storage: the file is copied into `<chat folder>/attachments/` (scratch or allowed folder). The message stores only the list of names, types and sizes in a new `chat_message.attachments` JSON column (new migration; empty means none). File contents never go into the database. Names are sanitized for all three OSes: path separators and `<>:"|?*` removed, control chars removed, trailing dots and spaces trimmed, Windows reserved names (`CON`, `NUL`, `COM1`, …) prefixed with `_`, and length capped. Name clashes get ` (2)`, ` (3)`, and so on. Every path is joined with `filepath` and checked to stay inside `attachments/`.
5. Uploads that are never sent (chip removed, chat closed) are deleted when they're removed, and any unsent uploads are cleaned up when the next message is sent. Deleting a chat already deletes its scratch folder; attachments in an allowed folder stay there, since they're the user's files.
6. How the AI gets them:
   - Text files: the message gets a line per file with its absolute OS-native path (for example "Attached file: C:\…\attachments\notes.md"), and the AI reads it with its own tools.
   - Images: Claude gets stream-json `image` content blocks (base64); Codex gets `turn/start` `localImage` input items; Copilot gets ACP `image` content blocks when the agent says it supports images; opencode gets `file` parts. The exact shapes are confirmed from each CLI's schema or docs during the task. If a CLI can't take images, the image is sent as a path line like text files, and the chat shows a note.
   - Local AI: Ollama models that list the `vision` capability (`/api/show`) get the images (`images` base64 on the native API); other Ollama models get only the text, and the chat shows "This model can't see images". On non-Ollama servers the image is sent as an OpenAI `image_url` data URL, and if the server rejects it, the turn is retried once without it and the same note is shown.
7. In the chat, user messages show attachment chips (icon, name, size). Clicking a chip uses the existing reveal helper: Finder on macOS, Explorer `/select,` on Windows, and the containing folder via `xdg-open` on Linux. Images also show a small preview, served by `GET /api/chats/{id}/attachments/{name}` (correct `Content-Type`, `nosniff`, name checked to stay inside `attachments/`). No `file://` URLs are used, because they don't load in the webviews.
8. Copying a chat or exporting a board doesn't include attachments (chats aren't part of the board backup format).
9. Live checks (approved): 4 tiny prompts, one at a time, each with one small generated image of a red square: "What color is this square? Reply with one word." One each for Claude, Codex, Copilot, and opencode with a free model. Local AI is tested only with fake servers. No vision model is downloaded.
10. Commits use the caveman-commit style.

## Tasks

- [x] Backend storage: migration for `chat_message.attachments`, upload/delete/serve endpoints, cross-OS name sanitizing, limits and type detection, cleanup of unsent uploads, and attachments on `POST /api/chats/{id}/messages`. Check: Go tests for limits, types, sanitizing (Windows reserved names, separators, clashes), path escapes, serve headers, cleanup; race; `make release` (Windows compile).
- [ ] Backend delivery: per-CLI image and text delivery (decision 6), the local vision check and fallback, and the "can't see images" note. Check: Go tests for the exact payload built per runtime with fake processes or servers; race; `make release`; the 4 live prompts from decision 9.
- [ ] UI: `paperclip` icon, the attach button, drag and drop, paste, pending chips with remove, sent-message chips with reveal and image previews, and the "can't see images" note. Check: `pnpm --dir web build`; `wails build`; user smoke test (attach a `.md` and a screenshot in a Claude chat).
