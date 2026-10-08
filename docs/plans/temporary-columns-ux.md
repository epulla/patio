# Temporary columns UX

## Request

> I would like to improve UI and have some label about what 'temp' columns mean: [Image 1: board settings Columns section, "Done" row with "Temporary" select and unlabeled "1" number input]. Also when creating a board, how can I set a temp column from there? Currently I need to create a board first, then go to settings for updating that to be a temp one

## Decisions

1. Settings "Columns" section gets one muted help line: "Temporary: tickets auto-hide from the board N days after entering the column. They are not deleted."
2. Days input gets visible "days" suffix and `title="Hide tickets after N days"`.
3. Board view `Temporary` badge gets `title` tooltip, e.g. "Tickets hide after 1 day" (plural aware).
4. Create-board form: comma input replaced by editable row list (name, Classic/Temporary select, days when temporary, remove button, "Add column" button). Prefilled To Do / In Progress / Done, all Classic.
5. Default columns stay Classic.
6. `POST /api/boards` `statuses` accepts strings (old) and objects `{name, type, hide_after_days}`, mixed allowed. Validation matches status create: type `classic|temporary` (default `classic`), `hide_after_days >= 1` (default 1). Bad entry -> 400.
7. MCP `create_board` can create temporary columns.
8. MCP arg shape: `statuses` becomes list of objects `{name, hide_after_days?}`. `hide_after_days` present -> temporary; absent -> classic. Schema description says so explicitly. MCP maps to explicit `type` before calling API.

## Tasks

- [x] API: `createBoard` accepts string or object statuses, writes type + `hide_after_days`. Check: new Go test creates board with mixed statuses, board data shows temporary column with days; invalid type/days -> 400; `go test ./... -count=1`.
- [x] MCP: `create_board` statuses as `{name, hide_after_days?}`. Check: `internal/mcp/server_test.go` creates one classic and one temporary (`hide_after_days: 2`) column and verifies type + days; `go test ./internal/mcp -count=1`.
- [x] Settings + board badge labels (decisions 1-3). Check: `pnpm --dir web build`; smoke-test settings and badge tooltip.
- [ ] Create-board column row editor (decision 4). Check: `pnpm --dir web build`; smoke-test creating board with a temporary "Done" (2 days), confirm in settings.
