import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, type McpServer, type McpServerCheck } from "./api";
import { ErrorText, navigate } from "./App";
import { Icon, IconButton } from "./icons";
import Switch from "./Switch";

export default function McpServers({ refresh }: { refresh: number }) {
  const [servers, setServers] = useState<McpServer[]>([]);
  const [error, setError] = useState<unknown>();
  const [editing, setEditing] = useState<McpServer | "new">();
  const [reload, setReload] = useState(0);

  useEffect(() => {
    api
      .get<McpServer[]>("/api/mcp-servers")
      .then((list) => {
        setServers(list);
        setError(undefined);
      })
      .catch(setError);
  }, [refresh, reload]);

  const changed = () => setReload((value) => value + 1);

  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <p className="eyebrow">AI</p>
          <h1>MCP servers</h1>
        </div>
        <button className="primary" onClick={() => setEditing("new")}>
          <Icon name="plus" /> Add server
        </button>
      </div>
      <p className="muted">
        Enabled servers are available in every chat, next to the built-in my-tickets tools (status in{" "}
        <a
          href="/settings"
          onClick={(event) => {
            event.preventDefault();
            navigate("/settings");
          }}
        >
          Settings
        </a>
        ). Claude Code, Copilot, and opencode ask before using their tools; Codex runs them without asking.
      </p>
      <ErrorText error={error} />
      {servers.length === 0 && !error && (
        <p className="callout">No servers yet. Add a remote URL (HTTP) or a local command (stdio).</p>
      )}
      <div className="connector-grid">
        {servers.map((server) => (
          <ServerCard key={server.id} server={server} onEdit={() => setEditing(server)} onChanged={changed} />
        ))}
      </div>
      {editing && (
        <ServerDialog
          server={editing === "new" ? undefined : editing}
          onClose={() => setEditing(undefined)}
          onSaved={() => {
            setEditing(undefined);
            changed();
          }}
        />
      )}
    </div>
  );
}

function ServerCard({
  server,
  onEdit,
  onChanged,
}: {
  server: McpServer;
  onEdit: () => void;
  onChanged: () => void;
}) {
  const [check, setCheck] = useState<McpServerCheck>();
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState<unknown>();
  const [message, setMessage] = useState("");
  const [run, setRun] = useState(0);
  const poll = useRef<number | undefined>(undefined);
  const needsSignIn = server.auth === "oauth" && !server.has_oauth_token;

  useEffect(() => {
    if (needsSignIn) return setCheck(undefined);
    let active = true;
    setChecking(true);
    api
      .get<McpServerCheck>(`/api/mcp-servers/${server.id}/check`)
      .then((value) => active && setCheck(value))
      .catch((reason) => active && setError(reason))
      .finally(() => active && setChecking(false));
    return () => {
      active = false;
    };
  }, [
    server.id,
    server.has_oauth_token,
    server.has_bearer_token,
    server.url,
    server.command,
    server.args.join("\u0000"),
    server.env_keys.join(","),
    needsSignIn,
    run,
  ]);

  useEffect(() => () => window.clearInterval(poll.current), []);

  async function toggle() {
    try {
      await api.put(`/api/mcp-servers/${server.id}`, { enabled: !server.enabled });
      onChanged();
    } catch (reason) {
      setError(reason);
    }
  }

  async function signIn() {
    setError(undefined);
    try {
      await api.post(`/api/mcp-servers/${server.id}/login`, {});
      setMessage("Finish signing in in your browser.");
      const started = Date.now();
      window.clearInterval(poll.current);
      poll.current = window.setInterval(async () => {
        try {
          const list = await api.get<McpServer[]>("/api/mcp-servers");
          if (list.find((s) => s.id === server.id)?.has_oauth_token || Date.now() - started > 5 * 60_000) {
            window.clearInterval(poll.current);
            setMessage("");
            onChanged();
          }
        } catch {
          window.clearInterval(poll.current);
        }
      }, 2000);
    } catch (reason) {
      setError(reason);
    }
  }

  async function remove() {
    if (!confirm(`Delete MCP server "${server.name}"?`)) return;
    try {
      await api.delete(`/api/mcp-servers/${server.id}`);
      onChanged();
    } catch (reason) {
      setError(reason);
    }
  }

  const state = needsSignIn ? "down" : checking || !check ? "pending" : check.ok ? "up" : "down";
  const label = needsSignIn
    ? "Sign in required"
    : checking || !check
      ? "Checking…"
      : check.ok
        ? `Healthy · ${check.latency_ms} ms · ${check.tools.length} tool${check.tools.length === 1 ? "" : "s"}`
        : "Unreachable";

  return (
    <article className={`panel connector-card ${state}`} aria-labelledby={`mcp-${server.id}`}>
      <header className="connector-head">
        <div>
          <p className="eyebrow">
            {server.kind === "stdio" ? "Local command" : "Remote"}
            {server.auth === "oauth" ? " · OAuth" : server.auth === "bearer" ? " · token" : ""}
          </p>
          <h2 id={`mcp-${server.id}`}>{server.name}</h2>
        </div>
        <div className="connector-actions">
          <button aria-label={`Edit ${server.name}`} onClick={onEdit}>
            Edit
          </button>
          <IconButton className="danger-text" icon="trash" label={`Delete ${server.name}`} onClick={remove} />
        </div>
      </header>

      <p className="connector-state" role="status">
        <span className="state-dot" aria-hidden="true" />
        <span>{label}</span>
      </p>
      <code className="connector-url">
        {server.kind === "stdio" ? [server.command, ...server.args].join(" ") : server.url}
      </code>
      {check && !check.ok && check.error && <p className="muted connector-note">{check.error}</p>}
      {check?.ok && check.tools.length > 0 && (
        <details className="mcp-tool-list">
          <summary>Tools</summary>
          <dl className="mcp-tools">
            {check.tools.map((tool) => (
              <div key={tool.name}>
                <dt>
                  <code>{tool.name}</code>
                </dt>
                <dd>{tool.description}</dd>
              </div>
            ))}
          </dl>
        </details>
      )}
      {message && (
        <p className="muted" role="status">
          {message}
        </p>
      )}
      <ErrorText error={error} />

      <div className="connector-buttons">
        <Switch checked={server.enabled} onChange={toggle} label="Enabled in chats" />
        {server.auth === "oauth" && (
          <button onClick={signIn}>{server.has_oauth_token ? "Sign in again" : "Sign in"}</button>
        )}
        <button disabled={checking || needsSignIn} onClick={() => setRun((value) => value + 1)}>
          Test
        </button>
      </div>
    </article>
  );
}

function parseEnv(text: string): Record<string, string> {
  const env: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const index = trimmed.indexOf("=");
    if (index < 1) throw new Error(`Environment line "${trimmed}" must look like KEY=value`);
    env[trimmed.slice(0, index).trim()] = trimmed.slice(index + 1);
  }
  return env;
}

function ServerDialog({
  server,
  onClose,
  onSaved,
}: {
  server?: McpServer;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [name, setName] = useState(server?.name ?? "");
  const [kind, setKind] = useState<McpServer["kind"]>(server?.kind ?? "http");
  const [command, setCommand] = useState(server?.command ?? "");
  const [args, setArgs] = useState(server?.args.join("\n") ?? "");
  const [env, setEnv] = useState("");
  const [clearEnv, setClearEnv] = useState(false);
  const [url, setURL] = useState(server?.url ?? "");
  const [auth, setAuth] = useState<McpServer["auth"]>(server?.auth ?? "none");
  const [token, setToken] = useState("");
  const [error, setError] = useState<unknown>();
  const dialog = useRef<HTMLDialogElement>(null);
  const nameInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
    nameInput.current?.focus();
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const body: Record<string, unknown> = { name: name.trim(), kind };
      if (kind === "stdio") {
        body.command = command.trim();
        body.args = args.split("\n").map((line) => line.trim()).filter(Boolean);
        body.auth = "none";
        if (env.trim() || !server || clearEnv) body.env = parseEnv(env);
      } else {
        body.url = url.trim();
        body.auth = auth;
        if (auth === "bearer" && token) body.bearer_token = token;
      }
      if (server) await api.put(`/api/mcp-servers/${server.id}`, body);
      else await api.post("/api/mcp-servers", { ...body, enabled: true });
      onSaved();
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <dialog ref={dialog} className="create-dialog" aria-labelledby="mcp-dialog-title" onClose={onClose}>
      <section className="panel create create-modal">
        <div className="page-heading">
          <h2 id="mcp-dialog-title">{server ? "Edit MCP server" : "Add MCP server"}</h2>
          <IconButton type="button" className="ghost" icon="x" label="Close" onClick={() => dialog.current?.close()} />
        </div>
        <form onSubmit={submit}>
          <label>
            Name
            <input ref={nameInput} required maxLength={64} value={name} onChange={(event) => setName(event.target.value)} />
          </label>
          <label>
            Type
            <select value={kind} disabled={!!server} onChange={(event) => setKind(event.target.value as McpServer["kind"])}>
              <option value="http">Remote URL (HTTP)</option>
              <option value="stdio">Local command (stdio)</option>
            </select>
          </label>

          {kind === "stdio" ? (
            <>
              <label className="wide">
                Command
                <input
                  required
                  placeholder="npx or /usr/local/bin/my-server"
                  value={command}
                  onChange={(event) => setCommand(event.target.value)}
                />
              </label>
              <label className="wide">
                Arguments (one per line)
                <textarea rows={3} value={args} onChange={(event) => setArgs(event.target.value)} />
              </label>
              <label className="wide">
                Environment (KEY=value, one per line)
                <textarea
                  rows={3}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={server?.env_keys.length ? `Saved: ${server.env_keys.join(", ")}. Leave empty to keep.` : "API_KEY=…"}
                  value={env}
                  onChange={(event) => setEnv(event.target.value)}
                />
                <small>Values are stored in your system keychain. Entering any lines replaces all saved values.</small>
              </label>
              {!!server?.env_keys.length && (
                <label className="check-row wide">
                  <input type="checkbox" checked={clearEnv} onChange={(event) => setClearEnv(event.target.checked)} />
                  Remove saved values
                </label>
              )}
            </>
          ) : (
            <>
              <label className="wide">
                URL
                <input
                  required
                  type="url"
                  placeholder="https://example.com/mcp"
                  value={url}
                  onChange={(event) => setURL(event.target.value)}
                />
              </label>
              <label>
                Sign-in
                <select value={auth} onChange={(event) => setAuth(event.target.value as McpServer["auth"])}>
                  <option value="none">None</option>
                  <option value="bearer">Access token</option>
                  <option value="oauth">OAuth (sign in with browser)</option>
                </select>
              </label>
              {auth === "bearer" && (
                <label>
                  Access token
                  <input
                    type="password"
                    autoComplete="off"
                    required={!server?.has_bearer_token}
                    placeholder={server?.has_bearer_token ? "Saved. Type to replace" : ""}
                    value={token}
                    onChange={(event) => setToken(event.target.value)}
                  />
                </label>
              )}
              {auth === "oauth" && (
                <p className="muted wide">After saving, press Sign in on the server card.</p>
              )}
            </>
          )}

          <div className="modal-actions">
            <button type="button" onClick={() => dialog.current?.close()}>
              Cancel
            </button>
            <button className="primary">{server ? "Save" : "Add server"}</button>
          </div>
        </form>
        <ErrorText error={error} />
      </section>
    </dialog>
  );
}
