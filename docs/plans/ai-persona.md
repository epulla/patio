# ai-persona

## Request

> so let's update the 'ai' user to have some kinda persona: 'Martin'. Also, whenever Martin's name appear, an AI label/symbol should appear showing that was the AI who did it

## Decisions

1. **AI flag.** Migration `0003` adds `user.is_ai INTEGER NOT NULL DEFAULT 0`, sets user `ai` to name "Martin", last_name NULL, email `martin-ai@localhost`, `is_ai=1`. API returns `user_is_ai` next to `user_name` (history, transitions, comments) and `is_ai` in `/api/users`.
2. **Label.** Small text badge "AI" after the name, `title` and `aria-label` "AI agent". No icon or emoji.
3. **Where.** Ticket history, transitions, comments, assignee options ("Martin (AI)"), print view. Markdown export prints "Martin (AI)". JSON backup adds optional `is_ai` on users; older backups without it still import.
4. **MCP instructions.** Server instructions: "You act as Martin, your AI companion for your everyday and weekly tasks. You should be supportive, give recommendations, have a friendly and funny tone (but also you should be concise, direct, terse, not too verbose), make some jokes sometimes. Your changes are labeled AI in the app". The typo "you AI" is corrected to "your AI".

## Tasks

- [x] 1. Martin persona + AI badge end to end (migration, API fields, UI badge, print, Markdown, backup field, MCP instructions). **Check:** `go test ./... -count=1` (history/transition/comment by `ai` return `user_name` "Martin" + `user_is_ai` true; Markdown export contains "Martin (AI)"; MCP initialize returns the instructions); `pnpm --dir web build` passes.
