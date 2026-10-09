# AI connectors (phase b1)

## Request

> Connectors: can have multiple Claude & ChatGPT connections and should show the usage quota.

> i would like to connect to Codex and Copilot CLI.

Phase (b1) of `docs/plans/ai-shell.md`; decisions 6–9 and 13 there still apply.

## Decisions

1. Claude terms: build it. The Claude connector is labelled "uses your local Claude Code login", and the README notes that Anthropic's terms apply. The user decides on approval from Anthropic before any public release.
2. Kinds and drivers: `claude` uses `claude -p` with stream-json; `codex` uses `codex app-server` (JSON-RPC over stdio); `opencode` uses `opencode serve`; `copilot` uses `copilot --acp --stdio`; `local` uses an OpenAI-compatible HTTP endpoint. b1 uses only the account, status and quota parts; chats come in b2.
3. Quota:
   - Codex: 5-hour and weekly `usedPercent` and `resetsAt` from app-server `account/rateLimits/read`.
   - Claude: the last rate-limit info seen during chats, stored by b2. Shows "unknown" until then.
   - opencode: token and cost totals from `opencode stats`, no quota.
   - Copilot: PAT connectors show this month's premium-request usage count (no limit) from `GET /users/{username}/settings/billing/premium_request/usage`. Others show "unknown".
   - Local AI: no quota.
4. Accounts: the first connector of each kind may use the existing CLI login (no config dir). Extra connectors:
   - Claude: own `CLAUDE_CONFIG_DIR`.
   - Codex: own `CODEX_HOME` with `-c cli_auth_credentials_store="file"`.
   - opencode: own `XDG_DATA_HOME` and `XDG_CONFIG_HOME`. macOS/Linux only; Windows offers only the existing login.
   - Copilot: a fine-grained PAT with the "Copilot Requests" permission, stored in the keychain and passed as `COPILOT_GITHUB_TOKEN`, plus its own `COPILOT_HOME`.
5. Login:
   - Claude and opencode: "Log in" opens an OS terminal running `claude auth login` or `opencode auth login` with the connector's env set. The terminal is macOS `osascript` Terminal `do script`, Windows `cmd /c start "" cmd /k`, or Linux `x-terminal-emulator -e`.
   - Copilot existing login: the terminal runs `copilot login --device-code`. PAT connectors have no login button; the PAT is entered in the form.
   - Codex: app-server `account/login/start` `{"type":"chatgpt"}` returns an `authUrl`, which opens in the browser; completion arrives via `account/login/completed`.
6. Status:
   - Claude: `claude auth status --json` (logged in, email, `subscriptionType`).
   - Codex: app-server `account/read` (`account.email`, `account.planType`; `account: null` means logged out).
   - opencode: `opencode auth list`.
   - Copilot: PAT connectors check `GET https://api.github.com/user` and show the login name, with plan "unknown". The existing-login connector shows "installed", and login is checked on the first chat (no non-interactive status command exists).
7. Detection: look the CLI up with `$SHELL -ilc 'command -v <cli>'` (3-second timeout), falling back to known dirs:
   - Claude: `~/.local/bin`.
   - Codex: `~/.local/bin`, `$CODEX_HOME/packages/standalone/current/bin`, Windows `%LOCALAPPDATA%\Programs\OpenAI\Codex\bin`.
   - opencode: `~/.opencode/bin`.
   - Copilot: `~/.local/bin`, `/usr/local/bin`.
   - Also the Homebrew dirs and the npm global bin.

   If a CLI is missing, show the official install command to copy plus a docs link. The app never runs installers.
8. Local AI connector: name, base URL with presets (Ollama `http://localhost:11434/v1`, LM Studio `http://localhost:1234/v1`, llama.cpp `http://127.0.0.1:8080/v1`), a model chosen from `GET /v1/models`, and an optional API key. A "Test" button checks the connection.
9. Secrets (local API keys, Copilot PATs) are stored with `github.com/zalando/go-keyring` v0.2.8, which is CGO-free. Neither the DB nor API responses ever contain a secret, only whether one is set. Tests use `keyring.MockInit()`.
10. Codex and Copilot are not installed locally. Their checks use recorded output from the official schemas and docs, and live checks are marked unverified. Remind the user to install one before b1 closes. Options: Codex via `curl -fsSL https://chatgpt.com/codex/install.sh | sh` or `brew install --cask codex`; Copilot via `curl -fsSL https://gh.io/copilot-install | bash` or `brew install --cask copilot-cli`. The macOS/Linux install hints show the official script commands (Codex, Copilot, Claude `curl -fsSL https://claude.ai/install.sh | bash`, opencode `curl -fsSL https://opencode.ai/install | bash`). The Windows hints show `irm https://chatgpt.com/codex/install.ps1 | iex`, `winget install GitHub.Copilot`, and `irm https://claude.ai/install.ps1 | iex`.

## Tasks

- [x] Connector storage and CRUD: migration `0006_connector.sql` (id, kind `claude|codex|opencode|copilot|local`, name, config_dir nullable, base_url, model, created_at) and `/api/connectors` list/create/update/delete. Create allocates an isolated dir for extra CLI connectors (decision 4). Local keys and Copilot PATs go to the keychain (decision 9). Check: `go test ./... -count=1` covering CRUD, the keychain round-trip, and the absence of secrets in responses; `make release`.
- [x] CLI detection and status: `GET /api/connectors/{id}/status` returns installed (path, version), logged-in state, email or login name and plan for each kind, plus install hints when the CLI is missing (decisions 6, 7). Check: Go tests parse recorded `claude auth status --json`, Codex `initialize` + `account/read`, `opencode auth list`, and a fake GitHub `/user`; manual check against the local claude and opencode.
- [x] Login: `POST /api/connectors/{id}/login` uses the terminal launcher for claude, opencode and copilot, and the app-server browser login for codex (decision 5). Check: on macOS, Terminal opens running `claude auth login` with `CLAUDE_CONFIG_DIR` set; a Go test drives a fake codex app-server through `account/login/start` and `completed`; `make release`.
- [ ] Quota and local test: Codex `account/rateLimits/read`, `opencode stats` totals, Copilot premium-request usage for PAT connectors, Claude "unknown" with a stored-last-seen slot for b2, and a local `GET /v1/models` test (decisions 3, 8). Check: Go tests over recorded JSON and stats output, plus `httptest` fakes for GitHub and `/v1/models`.
- [ ] Connectors page `/ai/connectors`: list with status, quota bars, Log in, install hint, add/edit/delete (including the PAT field for extra Copilot connectors), and a local preset with model picker and Test (decisions 1–8). Check: `pnpm --dir web build`; `wails build`; smoke test with a throwaway DB: a Claude connector on the existing login shows email and plan, a local preset reports its Test result, and Codex and Copilot cards show install hints.
