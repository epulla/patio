import { confirmDialog } from "./confirm";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, type McpServer, type McpServerCheck } from "./api";
import { ErrorText, navigate } from "./App";
import { Icon, IconButton } from "./icons";
import Switch from "./Switch";
import Picker from "./Picker";
import { lang, useText } from "./i18n";

const text = {
  en: {
    addServer: "Add server", title: "MCP servers", settings: "Settings", intro: "Enabled servers are available in every chat, next to the built-in Patio tools (status in ",
    introEnd: "). Claude Code, Copilot, and opencode ask before using their tools; Codex runs them without asking.",
    empty: "No servers yet. Add a remote URL (HTTP) or a local command (stdio).", finishLogin: "Finish signing in in your browser.",
    deleteQuestion: (name: string) => `Delete MCP server "${name}"?`, signInRequired: "Sign in required", checking: "Checking…", healthy: (latency: number, n: number) => `Healthy · ${latency} ms · ${n} tool${n === 1 ? "" : "s"}`, unreachable: "Unreachable",
    localCommand: "Local command", remote: "Remote", edit: (name: string) => `Edit ${name}`, editText: "Edit", delete: (name: string) => `Delete ${name}`, tools: "Tools", enabled: "Enabled in chats", signInAgain: "Sign in again", signIn: "Sign in", test: "Test",
    editTitle: "Edit MCP server", addTitle: "Add MCP server", close: "Close", name: "Name", type: "Type", remoteURL: "Remote URL (HTTP)", localStdio: "Local command (stdio)", command: "Command", commandPlaceholder: "npx or /usr/local/bin/my-server", args: "Arguments (one per line)", environment: "Environment (KEY=value, one per line)", savedKeep: (keys: string) => `Saved: ${keys}. Leave empty to keep.`, apiKey: "API_KEY=…", envNote: "Values are stored in your system keychain. Entering any lines replaces all saved values.", removeValues: "Remove saved values", url: "URL", signInLabel: "Sign-in", none: "None", accessToken: "Access token", oauth: "OAuth (sign in with browser)", tokenReplace: "Saved. Type to replace", afterSave: "After saving, press Sign in on the server card.", cancel: "Cancel", save: "Save",
    clientId: "OAuth client ID (optional)", clientSecret: "OAuth client secret", scopes: "Scopes (space-separated)", clientNote: "Only for providers without automatic client registration, like Google. Leave empty otherwise.",
  },
  es: {
    addServer: "Agregar servidor", title: "Servidores MCP", settings: "Configuración", intro: "Los servidores habilitados están disponibles en todos los chats, junto a las herramientas integradas de Patio (estado en ",
    introEnd: "). Claude Code, Copilot y opencode preguntan antes de usar sus herramientas; Codex las ejecuta sin preguntar.",
    empty: "Aún no hay servidores. Agrega una URL remota (HTTP) o un comando local (stdio).", finishLogin: "Termina de iniciar sesión en tu navegador.",
    deleteQuestion: (name: string) => `¿Eliminar el servidor MCP «${name}»?`, signInRequired: "Debes iniciar sesión", checking: "Comprobando…", healthy: (latency: number, n: number) => `Disponible · ${latency} ms · ${n} herramienta${n === 1 ? "" : "s"}`, unreachable: "No disponible",
    localCommand: "Comando local", remote: "Remoto", edit: (name: string) => `Editar ${name}`, editText: "Editar", delete: (name: string) => `Eliminar ${name}`, tools: "Herramientas", enabled: "Habilitado en chats", signInAgain: "Iniciar sesión de nuevo", signIn: "Iniciar sesión", test: "Probar",
    editTitle: "Editar servidor MCP", addTitle: "Agregar servidor MCP", close: "Cerrar", name: "Nombre", type: "Tipo", remoteURL: "URL remota (HTTP)", localStdio: "Comando local (stdio)", command: "Comando", commandPlaceholder: "npx o /usr/local/bin/my-server", args: "Argumentos (uno por línea)", environment: "Entorno (KEY=value, uno por línea)", savedKeep: (keys: string) => `Guardadas: ${keys}. Déjalo vacío para conservarlas.`, apiKey: "API_KEY=…", envNote: "Los valores se guardan en el llavero del sistema. Si ingresas líneas, se reemplazan todos los valores guardados.", removeValues: "Eliminar valores guardados", url: "URL", signInLabel: "Inicio de sesión", none: "Ninguno", accessToken: "Token de acceso", oauth: "OAuth (inicia sesión con el navegador)", tokenReplace: "Guardado. Escribe para reemplazarlo", afterSave: "Después de guardar, presiona Iniciar sesión en la tarjeta del servidor.", cancel: "Cancelar", save: "Guardar",
    clientId: "ID de cliente OAuth (opcional)", clientSecret: "Secreto de cliente OAuth", scopes: "Scopes (separados por espacios)", clientNote: "Solo para proveedores sin registro automático de clientes, como Google. Si no, déjalo vacío.",
  },
};

export default function McpServers({ refresh }: { refresh: number }) {
  const t = useText(text);
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
          <h1>{t.title}</h1>
        </div>
        <button className="primary" onClick={() => setEditing("new")}>
          <Icon name="plus" /> {t.addServer}
        </button>
      </div>
      <p className="muted">
        {t.intro}{" "}
        <a
          href="/settings"
          onClick={(event) => {
            event.preventDefault();
            navigate("/settings");
          }}
        >
          {t.settings}
        </a>
        {t.introEnd}
      </p>
      <ErrorText error={error} />
      {servers.length === 0 && !error && (
        <p className="callout">{t.empty}</p>
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
  const t = useText(text);
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
      setMessage(t.finishLogin);
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
    if (!(await confirmDialog(text[lang()].deleteQuestion(server.name)))) return;
    try {
      await api.delete(`/api/mcp-servers/${server.id}`);
      onChanged();
    } catch (reason) {
      setError(reason);
    }
  }

  const state = needsSignIn ? "down" : checking || !check ? "pending" : check.ok ? "up" : "down";
  const label = needsSignIn
    ? t.signInRequired
    : checking || !check
      ? t.checking
      : check.ok
        ? t.healthy(check.latency_ms, check.tools.length)
        : t.unreachable;

  return (
    <article className={`panel connector-card ${state}`} aria-labelledby={`mcp-${server.id}`}>
      <header className="connector-head">
        <div>
          <p className="eyebrow">
            {server.kind === "stdio" ? t.localCommand : t.remote}
            {server.auth === "oauth" ? " · OAuth" : server.auth === "bearer" ? " · token" : ""}
          </p>
          <h2 id={`mcp-${server.id}`}>{server.name}</h2>
        </div>
        <div className="connector-actions">
          <button aria-label={t.edit(server.name)} onClick={onEdit}>
            {t.editText}
          </button>
          <IconButton className="danger-text" icon="trash" label={t.delete(server.name)} onClick={remove} />
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
          <summary>{t.tools}</summary>
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
        <Switch checked={server.enabled} onChange={toggle} label={t.enabled} />
        {server.auth === "oauth" && (
          <button onClick={signIn}>{server.has_oauth_token ? t.signInAgain : t.signIn}</button>
        )}
        <button disabled={checking || needsSignIn} onClick={() => setRun((value) => value + 1)}>
          {t.test}
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
  const t = useText(text);
  const [name, setName] = useState(server?.name ?? "");
  const [kind, setKind] = useState<McpServer["kind"]>(server?.kind ?? "http");
  const [command, setCommand] = useState(server?.command ?? "");
  const [args, setArgs] = useState(server?.args.join("\n") ?? "");
  const [env, setEnv] = useState("");
  const [clearEnv, setClearEnv] = useState(false);
  const [url, setURL] = useState(server?.url ?? "");
  const [auth, setAuth] = useState<McpServer["auth"]>(server?.auth ?? "none");
  const [token, setToken] = useState("");
  const [clientId, setClientId] = useState(server?.oauth_client_id ?? "");
  const [clientSecret, setClientSecret] = useState("");
  const [scopes, setScopes] = useState(server?.oauth_scopes.join(" ") ?? "");
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
        if (auth === "oauth") {
          body.oauth_client_id = clientId.trim();
          body.oauth_scopes = clientId.trim() ? scopes.split(/\s+/).filter(Boolean) : [];
          if (clientId.trim() && clientSecret) body.oauth_client_secret = clientSecret;
        }
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
          <h2 id="mcp-dialog-title">{server ? t.editTitle : t.addTitle}</h2>
          <IconButton type="button" className="ghost" icon="x" label={t.close} onClick={() => dialog.current?.close()} />
        </div>
        <form onSubmit={submit}>
          <label>
            {t.name}
            <input ref={nameInput} required maxLength={64} value={name} onChange={(event) => setName(event.target.value)} />
          </label>
          <label>
            {t.type}
            <Picker
              label={t.type}
              options={[
                { id: "http", name: t.remoteURL },
                { id: "stdio", name: t.localStdio },
              ]}
              value={kind}
              disabled={!!server}
              onChange={(k) => setKind(k as McpServer["kind"])}
            />
          </label>

          {kind === "stdio" ? (
            <>
              <label className="wide">
                {t.command}
                <input
                  required
                  placeholder={t.commandPlaceholder}
                  value={command}
                  onChange={(event) => setCommand(event.target.value)}
                />
              </label>
              <label className="wide">
                {t.args}
                <textarea rows={3} value={args} onChange={(event) => setArgs(event.target.value)} />
              </label>
              <label className="wide">
                {t.environment}
                <textarea
                  rows={3}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={server?.env_keys.length ? t.savedKeep(server.env_keys.join(", ")) : t.apiKey}
                  value={env}
                  onChange={(event) => setEnv(event.target.value)}
                />
                <small>{t.envNote}</small>
              </label>
              {!!server?.env_keys.length && (
                <label className="check-row wide">
                  <input type="checkbox" checked={clearEnv} onChange={(event) => setClearEnv(event.target.checked)} />
                  {t.removeValues}
                </label>
              )}
            </>
          ) : (
            <>
              <label className="wide">
                {t.url}
                <input
                  required
                  type="url"
                  placeholder="https://example.com/mcp"
                  value={url}
                  onChange={(event) => setURL(event.target.value)}
                />
              </label>
              <label>
                {t.signInLabel}
                <Picker
                  label={t.signInLabel}
                  options={[
                    { id: "none", name: t.none },
                    { id: "bearer", name: t.accessToken },
                    { id: "oauth", name: t.oauth },
                  ]}
                  value={auth}
                  onChange={(a) => setAuth(a as McpServer["auth"])}
                />
              </label>
              {auth === "bearer" && (
                <label>
                  {t.accessToken}
                  <input
                    type="password"
                    autoComplete="off"
                    required={!server?.has_bearer_token}
                    placeholder={server?.has_bearer_token ? t.tokenReplace : ""}
                    value={token}
                    onChange={(event) => setToken(event.target.value)}
                  />
                </label>
              )}
              {auth === "oauth" && (
                <>
                  <label className="wide">
                    {t.clientId}
                    <input autoComplete="off" spellCheck={false} value={clientId} onChange={(event) => setClientId(event.target.value)} />
                    <small>{t.clientNote}</small>
                  </label>
                  {clientId.trim() && (
                    <>
                      <label>
                        {t.clientSecret}
                        <input
                          type="password"
                          autoComplete="off"
                          placeholder={server?.has_oauth_client_secret ? t.tokenReplace : ""}
                          value={clientSecret}
                          onChange={(event) => setClientSecret(event.target.value)}
                        />
                      </label>
                      <label>
                        {t.scopes}
                        <input autoComplete="off" spellCheck={false} value={scopes} onChange={(event) => setScopes(event.target.value)} />
                      </label>
                    </>
                  )}
                  <p className="muted wide">{t.afterSave}</p>
                </>
              )}
            </>
          )}

          <div className="modal-actions">
            <button type="button" onClick={() => dialog.current?.close()}>
              {t.cancel}
            </button>
            <button className="primary">{server ? t.save : t.addServer}</button>
          </div>
        </form>
        <ErrorText error={error} />
      </section>
    </dialog>
  );
}
