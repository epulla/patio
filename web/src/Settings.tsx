import { useEffect, useState, type FormEvent, type MouseEvent } from "react";
import {
  api,
  type AllowedFolder,
  type ChatDefaults,
  type Connector,
  type McpStatus,
  type SettingsAbout,
  type SettingsRevealTarget,
} from "./api";
import { ErrorText } from "./App";
import { Icon, IconButton } from "./icons";
import Switch from "./Switch";

function AllowedFolders({ onChange }: { onChange: () => void }) {
  const [folders, setFolders] = useState<AllowedFolder[]>([]);
  const [path, setPath] = useState("");
  const [error, setError] = useState<unknown>();

  const load = () =>
    api
      .get<AllowedFolder[]>("/api/settings/folders")
      .then(setFolders)
      .catch(setError);

  useEffect(() => {
    void load();
  }, []);

  async function add(event: FormEvent) {
    event.preventDefault();
    try {
      await api.post("/api/settings/folders", { path: path.trim() });
      setPath("");
      setError(undefined);
      await load();
      onChange();
    } catch (reason) {
      setError(reason);
    }
  }

  async function remove(folder: AllowedFolder) {
    try {
      await api.delete(`/api/settings/folders/${folder.id}`);
      setError(undefined);
      await load();
      onChange();
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <section className="stack">
      <h2>Allowed folders</h2>
      <p className="muted">
        Chats can work inside these folders. A chat without a folder gets an empty folder of its own.
      </p>
      <section className="panel stack folder-panel">
        {folders.length > 0 ? (
          <ul className="folder-list">
            {folders.map((folder) => (
              <li key={folder.id}>
                <code>{folder.path}</code>
                <IconButton
                  className="danger-text"
                  icon="trash"
                  label={`Remove ${folder.path}`}
                  onClick={() => remove(folder)}
                />
              </li>
            ))}
          </ul>
        ) : (
          <p className="muted">No folders yet.</p>
        )}
        <form className="inline-form" onSubmit={add}>
          <label className="grow">
            Folder path
            <input
              required
              placeholder="/Users/you/projects/app"
              value={path}
              onChange={(event) => setPath(event.target.value)}
            />
          </label>
          <button>
            <Icon name="plus" /> Add folder
          </button>
        </form>
        <ErrorText error={error} />
      </section>
    </section>
  );
}

const fileManager: Record<string, string> = { darwin: "Finder", windows: "Explorer" };

export function formatBytes(bytes: number) {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(unit === 0 || value >= 10 ? 0 : 1)} ${units[unit]}`;
}

function General() {
  const [about, setAbout] = useState<SettingsAbout>();
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    api.get<SettingsAbout>("/api/settings/about").then(setAbout).catch(setError);
  }, []);

  async function reveal(target: SettingsRevealTarget) {
    try {
      await api.post("/api/settings/reveal", { target });
      setError(undefined);
    } catch (reason) {
      setError(reason);
    }
  }

  async function copyPath() {
    if (!about) return;
    try {
      await navigator.clipboard.writeText(about.db_path);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch (reason) {
      setError(reason);
    }
  }

  function openRepo(event: MouseEvent<HTMLAnchorElement>) {
    if (!window.runtime || !about) return;
    event.preventDefault();
    window.runtime.BrowserOpenURL(about.repo_url);
  }

  const manager = fileManager[about?.os ?? ""] ?? "file manager";
  const counts = about
    ? [
        ["boards", about.counts.boards],
        ["tickets", about.counts.tickets],
        ["chats", about.counts.chats],
        ["connectors", about.counts.connectors],
        ["MCP servers", about.counts.mcp_servers],
        ["skills", about.counts.skills],
      ]
    : [];

  return (
    <section className="stack">
      <h2>General</h2>
      <section className="panel">
        {about ? (
          <dl className="about-list">
            <dt>Version</dt>
            <dd>
              <code>{about.version}</code> · {about.os}/{about.arch}
            </dd>
            <dt>Database</dt>
            <dd>
              <code className="about-path">{about.db_path}</code>
              <span className="about-actions">
                <button type="button" onClick={() => reveal("database")}>
                  Show in {manager}
                </button>
                <button type="button" onClick={copyPath}>
                  {copied ? "Copied" : "Copy path"}
                </button>
              </span>
            </dd>
            <dt>Data folder</dt>
            <dd>
              <code className="about-path">{about.data_dir}</code>
              <span className="muted">
                {about.approximate ? "about " : ""}
                {formatBytes(about.data_dir_bytes)}
              </span>
              <span className="about-actions">
                <button type="button" onClick={() => reveal("data_dir")}>
                  Open
                </button>
              </span>
            </dd>
            <dt>Contents</dt>
            <dd>{counts.map(([label, count]) => `${count} ${label}`).join(" · ")}</dd>
            <dt>Schema</dt>
            <dd>Version {about.schema_version}</dd>
            <dt>Hidden fields</dt>
            <dd>{about.show_hidden ? "Shown (-show-hidden)" : "Hidden"}</dd>
            <dt>Secrets</dt>
            <dd>Stored in {about.secrets_store}, not in the database or backups.</dd>
            <dt>Source</dt>
            <dd>
              <a href={about.repo_url} target="_blank" rel="noreferrer" onClick={openRepo}>
                {about.repo_url.replace(/^https:\/\//, "")}
              </a>
            </dd>
          </dl>
        ) : (
          !error && <p className="muted" role="status">Loading…</p>
        )}
        <ErrorText error={error} />
      </section>
    </section>
  );
}

function Defaults({ foldersVersion }: { foldersVersion: number }) {
  const [defaults, setDefaults] = useState<ChatDefaults>();
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [folders, setFolders] = useState<AllowedFolder[]>([]);
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    Promise.all([
      api.get<ChatDefaults>("/api/settings/defaults"),
      api.get<Connector[]>("/api/connectors"),
      api.get<AllowedFolder[]>("/api/settings/folders"),
    ])
      .then(([nextDefaults, nextConnectors, nextFolders]) => {
        setDefaults(nextDefaults);
        setConnectors(nextConnectors);
        setFolders(nextFolders);
      })
      .catch(setError);
  }, [foldersVersion]);

  async function save(change: Partial<ChatDefaults>) {
    if (!defaults) return;
    const next = { ...defaults, ...change };
    try {
      setDefaults(await api.put<ChatDefaults>("/api/settings/defaults", next));
      setError(undefined);
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <section className="stack">
      <h2>Defaults</h2>
      <p className="muted">New chats start with these. Each chat can change them afterwards.</p>
      <section className="panel stack defaults-panel">
        {defaults ? (
          <>
            <div className="defaults-fields">
              <label>
                Connector
                <select
                  value={defaults.connector_id ?? ""}
                  onChange={(event) => save({ connector_id: event.target.value || null })}
                >
                  <option value="">First connector</option>
                  {connectors.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name} · {c.kind}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Folder
                <select value={defaults.folder ?? ""} onChange={(event) => save({ folder: event.target.value || null })}>
                  <option value="">Scratch folder (no files shared)</option>
                  {folders.map((f) => (
                    <option key={f.id} value={f.path}>
                      {f.path}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            <Switch
              className="warning"
              label="Auto-allow file changes and commands"
              checked={defaults.auto_allow}
              onChange={(auto_allow) => save({ auto_allow })}
            />
            <Switch
              label="Thinking"
              checked={defaults.thinking}
              onChange={(thinking) => save({ thinking })}
            />
          </>
        ) : (
          !error && <p className="muted" role="status">Loading…</p>
        )}
        <ErrorText error={error} />
      </section>
    </section>
  );
}

export default function Settings() {
  const [foldersVersion, setFoldersVersion] = useState(0);
  const [status, setStatus] = useState<McpStatus>();
  const [error, setError] = useState<unknown>();
  const [copyError, setCopyError] = useState<unknown>();
  const [checked, setChecked] = useState<Date>();
  const [refresh, setRefresh] = useState(0);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let active = true;
    const check = async () => {
      try {
        const result = await api.get<McpStatus>("/api/mcp");
        if (active) {
          setStatus(result);
          setError(undefined);
        }
      } catch (reason) {
        if (active) setError(reason);
      } finally {
        if (active) setChecked(new Date());
      }
    };
    void check();
    const timer = window.setInterval(check, 30000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [refresh]);

  const config = status
    ? JSON.stringify(
        { mcpServers: { patio: { command: status.command, args: status.args } } },
        null,
        2,
      )
    : "Loading…";

  async function copyConfig() {
    try {
      await navigator.clipboard.writeText(config);
      setCopyError(undefined);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch (reason) {
      setCopyError(reason);
    }
  }

  const state = error || status?.ok === false ? "down" : status ? "up" : "pending";
  const label = { up: "Healthy", down: "Unhealthy", pending: "Checking…" }[state];
  const detail = error
    ? "Could not reach the app backend."
    : status?.ok
      ? `MCP server responded in ${status.latency_ms} ms with ${status.tools.length} tools. Everything is fine.`
      : status?.error;

  return (
    <div className="stack mcp-page">
      <div className="page-heading">
        <div>
          <h1>Settings</h1>
        </div>
      </div>

      <General />

      <Defaults foldersVersion={foldersVersion} />

      <AllowedFolders onChange={() => setFoldersVersion((value) => value + 1)} />

      <section className="stack">
        <h2>MCP status</h2>
        <p className="muted">
          AI clients launch this app as an MCP server over stdio. It shares this app's database and acts as Martin.
        </p>
        <section className={`panel mcp-status ${state}`}>
          <span className="status-dot" aria-hidden="true" />
          <div className="mcp-status-text" role="status" aria-live="polite">
            <strong>{label}</strong>
            {detail && <span className="muted">{detail}</span>}
            <small>Last checked {checked ? checked.toLocaleTimeString() : "never"} · rechecks every 30 s</small>
          </div>
          <button type="button" onClick={() => setRefresh((value) => value + 1)}>
            Check again
          </button>
        </section>
        <ErrorText error={error} />

        <section className="panel">
          <div className="mcp-section-head">
            <div>
              <h3>Connect an AI client</h3>
              <p className="muted">Add this to your client's MCP config (Claude Desktop, Cursor, Claude Code…).</p>
            </div>
            <button type="button" onClick={copyConfig} disabled={!status}>
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
          <pre className="mcp-config">
            <code>{config}</code>
          </pre>
          <ErrorText error={copyError} />
        </section>

        {status?.ok && (
          <section className="panel">
            <h3>
              Tools <small>{status.tools.length}</small>
            </h3>
            <dl className="mcp-tools">
              {status.tools.map((tool) => (
                <div key={tool.name}>
                  <dt>
                    <code>{tool.name}</code>
                  </dt>
                  <dd>{tool.description}</dd>
                </div>
              ))}
            </dl>
          </section>
        )}
      </section>
    </div>
  );
}
