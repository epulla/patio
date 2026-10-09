import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  api,
  type Connector,
  type ConnectorKind,
  type ConnectorQuota,
  type ConnectorStatus,
} from "./api";
import { ErrorText } from "./App";
import { Icon, IconButton } from "./icons";

const kinds: { kind: ConnectorKind; label: string }[] = [
  { kind: "claude", label: "Claude Code" },
  { kind: "codex", label: "Codex" },
  { kind: "copilot", label: "GitHub Copilot" },
  { kind: "opencode", label: "opencode" },
  { kind: "local", label: "Local AI" },
];
const kindLabel = (kind: ConnectorKind) => kinds.find((k) => k.kind === kind)?.label ?? kind;

const presets = [
  { label: "Ollama", url: "http://localhost:11434/v1" },
  { label: "LM Studio", url: "http://localhost:1234/v1" },
  { label: "llama.cpp", url: "http://127.0.0.1:8080/v1" },
];

export default function Connectors({ refresh }: { refresh: number }) {
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [error, setError] = useState<unknown>();
  const [editing, setEditing] = useState<Connector | "new">();
  const [reload, setReload] = useState(0);

  useEffect(() => {
    api
      .get<Connector[]>("/api/connectors")
      .then((list) => {
        setConnectors(list);
        setError(undefined);
      })
      .catch(setError);
  }, [refresh, reload]);

  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <p className="eyebrow">AI</p>
          <h1>Connectors</h1>
        </div>
        <button className="primary" onClick={() => setEditing("new")}>
          <Icon name="plus" /> Add connector
        </button>
      </div>
      <p className="muted">
        Claude Code, Codex, Copilot, and opencode connectors use the official CLI installed on this
        computer, signed in with your own account. Their providers' terms apply.
      </p>
      <ErrorText error={error} />
      {connectors.length === 0 && !error && (
        <p className="callout">No connectors yet. Add one to chat with Claude, ChatGPT, Copilot, or a local model.</p>
      )}
      <div className="connector-grid">
        {connectors.map((connector) => (
          <ConnectorCard
            key={connector.id}
            connector={connector}
            onEdit={() => setEditing(connector)}
            onDeleted={() => setReload((value) => value + 1)}
          />
        ))}
      </div>
      {editing && (
        <ConnectorDialog
          connector={editing === "new" ? undefined : editing}
          existingLoginKinds={connectors.filter((c) => c.kind !== "local" && !c.config_dir).map((c) => c.kind)}
          onClose={() => setEditing(undefined)}
          onSaved={() => {
            setEditing(undefined);
            setReload((value) => value + 1);
          }}
        />
      )}
    </div>
  );
}

function ConnectorCard({
  connector,
  onEdit,
  onDeleted,
}: {
  connector: Connector;
  onEdit: () => void;
  onDeleted: () => void;
}) {
  const [status, setStatus] = useState<ConnectorStatus>();
  const [quota, setQuota] = useState<ConnectorQuota>();
  const [error, setError] = useState<unknown>();
  const [message, setMessage] = useState("");
  const [check, setCheck] = useState(0);
  const local = connector.kind === "local";
  const pat = connector.kind === "copilot" && (!!connector.config_dir || connector.has_secret);

  useEffect(() => {
    let active = true;
    setStatus(undefined);
    setQuota(undefined);
    api
      .get<ConnectorStatus>(`/api/connectors/${connector.id}/status`)
      .then((value) => active && setStatus(value))
      .catch((reason) => active && setError(reason));
    api
      .get<ConnectorQuota>(`/api/connectors/${connector.id}/quota`)
      .then((value) => active && setQuota(value))
      .catch((reason) => active && setError(reason));
    return () => {
      active = false;
    };
  }, [connector.id, connector.config_dir, connector.has_secret, check]);

  async function login() {
    setError(undefined);
    try {
      const result = await api.post<{ auth_url?: string }>(`/api/connectors/${connector.id}/login`, {});
      setMessage(
        result.auth_url
          ? "Finish signing in in your browser, then press Refresh."
          : "Finish signing in in the Terminal window, then press Refresh.",
      );
    } catch (reason) {
      setError(reason);
    }
  }

  async function test() {
    setError(undefined);
    setMessage("Testing…");
    try {
      const result = await api.post<{ models: string[] }>("/api/connectors/local-models", {
        id: connector.id,
        base_url: connector.base_url,
      });
      const found = !connector.model || result.models.includes(connector.model);
      setMessage(
        `Connected. ${result.models.length} model${result.models.length === 1 ? "" : "s"} available${found ? "" : `; "${connector.model}" not found`}.`,
      );
    } catch (reason) {
      setMessage("");
      setError(reason);
    }
  }

  async function remove() {
    if (!confirm(`Delete connector "${connector.name}"?`)) return;
    try {
      await api.delete(`/api/connectors/${connector.id}`);
      onDeleted();
    } catch (reason) {
      setError(reason);
    }
  }

  const state = local
    ? "neutral"
    : !status
      ? "pending"
      : status.installed === false || status.logged_in === false
        ? "down"
        : status.logged_in
          ? "up"
          : "neutral";
  const stateLabel = local
    ? connector.model || "No model selected"
    : !status
      ? "Checking…"
      : status.installed === false
        ? "Not installed"
        : status.logged_in === false
          ? "Not signed in"
          : status.logged_in
            ? [status.account, status.plan].filter(Boolean).join(" · ") || "Signed in"
            : "Sign-in unknown";

  return (
    <article className={`panel connector-card ${state}`} aria-labelledby={`connector-${connector.id}`}>
      <header className="connector-head">
        <div>
          <p className="eyebrow">
            {kindLabel(connector.kind)}
            {!local && (connector.config_dir ? (pat ? " · token" : " · separate account") : " · existing login")}
          </p>
          <h2 id={`connector-${connector.id}`}>{connector.name}</h2>
        </div>
        <div className="connector-actions">
          <button aria-label={`Edit ${connector.name}`} onClick={onEdit}>
            Edit
          </button>
          <IconButton className="danger-text" icon="trash" label={`Delete ${connector.name}`} onClick={remove} />
        </div>
      </header>

      <p className="connector-state" role="status">
        <span className="state-dot" aria-hidden="true" />
        <span>{stateLabel}</span>
        {status?.version && <small>{status.version}</small>}
      </p>
      {local && <small className="connector-url">{connector.base_url}</small>}
      {status?.note && <p className="muted connector-note">{status.note}</p>}

      {status?.install && (
        <div className="connector-install">
          <p>Install it in a terminal:</p>
          <pre className="mcp-config">
            <code>{status.install.command}</code>
          </pre>
          <a
            href={status.install.url}
            target="_blank"
            rel="noreferrer"
            onClick={(event) => {
              if (!window.runtime) return;
              event.preventDefault();
              window.runtime.BrowserOpenURL(status.install!.url);
            }}
          >
            Install guide
          </a>
        </div>
      )}

      {quota && !local && <Quota quota={quota} />}

      {message && (
        <p className="muted" role="status">
          {message}
        </p>
      )}
      <ErrorText error={error} />

      <div className="connector-buttons">
        {local ? (
          <button onClick={test}>Test</button>
        ) : (
          !pat &&
          status?.installed !== false && <button onClick={login}>Log in</button>
        )}
        <button
          onClick={() => {
            setMessage("");
            setError(undefined);
            setCheck((value) => value + 1);
          }}
        >
          Refresh
        </button>
      </div>
    </article>
  );
}

function Quota({ quota }: { quota: ConnectorQuota }) {
  const windows = quota.windows ?? [];
  return (
    <div className="quota">
      {windows.map((window) => {
        const used = Math.max(0, Math.min(100, window.used_percent ?? 0));
        return (
          <div className="quota-row" key={window.label}>
            <div className="quota-label">
              <span>{window.label}</span>
              <span>{window.used_percent == null ? "unknown" : `${Math.round(used)}% used`}</span>
            </div>
            <div
              className="quota-bar"
              role="progressbar"
              aria-label={`${window.label} usage`}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={window.used_percent == null ? undefined : Math.round(used)}
            >
              <span style={{ width: `${used}%` }} data-high={used >= 80 || undefined} />
            </div>
            {window.resets_at && <small>Resets {new Date(window.resets_at).toLocaleString()}</small>}
          </div>
        );
      })}
      {quota.requests != null && <p className="muted">{quota.requests} premium requests this month</p>}
      {quota.tokens && (
        <p className="muted">
          {quota.tokens.input.toLocaleString()} tokens in · {quota.tokens.output.toLocaleString()} out · $
          {quota.tokens.cost.toFixed(2)}
        </p>
      )}
      {quota.note && <p className="muted">{quota.note}</p>}
    </div>
  );
}

function ConnectorDialog({
  connector,
  existingLoginKinds,
  onClose,
  onSaved,
}: {
  connector?: Connector;
  existingLoginKinds: ConnectorKind[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [kind, setKind] = useState<ConnectorKind>(connector?.kind ?? "claude");
  const [name, setName] = useState(connector?.name ?? "");
  const [existing, setExisting] = useState(!existingLoginKinds.includes(connector?.kind ?? "claude"));
  const [baseURL, setBaseURL] = useState(connector?.base_url ?? presets[0].url);
  const [model, setModel] = useState(connector?.model ?? "");
  const [models, setModels] = useState<string[]>();
  const [secret, setSecret] = useState("");
  const [error, setError] = useState<unknown>();
  const dialog = useRef<HTMLDialogElement>(null);
  const nameInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
    nameInput.current?.focus();
  }, []);

  const editing = !!connector;
  const local = kind === "local";
  const existingTaken = existingLoginKinds.includes(kind);
  const useExisting = editing ? !connector.config_dir : existing && !existingTaken;
  const needsToken = kind === "copilot" && !useExisting;

  function changeKind(next: ConnectorKind) {
    setKind(next);
    setExisting(!existingLoginKinds.includes(next));
  }

  async function loadModels() {
    setError(undefined);
    try {
      const result = await api.post<{ models: string[] }>("/api/connectors/local-models", {
        base_url: baseURL.trim(),
        ...(secret ? { secret } : {}),
        ...(connector ? { id: connector.id } : {}),
      });
      setModels(result.models);
      if (!model && result.models[0]) setModel(result.models[0]);
    } catch (reason) {
      setError(reason);
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const fields = {
        name: name.trim() || kindLabel(kind),
        ...(local ? { base_url: baseURL.trim(), model: model.trim() } : {}),
        ...(secret ? { secret } : {}),
      };
      if (connector) await api.put(`/api/connectors/${connector.id}`, fields);
      else await api.post("/api/connectors", { ...fields, kind, use_existing_login: !local && useExisting });
      onSaved();
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <dialog ref={dialog} className="create-dialog" aria-labelledby="connector-dialog-title" onClose={onClose}>
      <section className="panel create create-modal">
        <div className="page-heading">
          <h2 id="connector-dialog-title">{editing ? "Edit connector" : "Add connector"}</h2>
          <IconButton type="button" className="ghost" icon="x" label="Close" onClick={() => dialog.current?.close()} />
        </div>
        <form onSubmit={submit}>
          <label>
            Type
            <select value={kind} disabled={editing} onChange={(event) => changeKind(event.target.value as ConnectorKind)}>
              {kinds.map((k) => (
                <option key={k.kind} value={k.kind}>
                  {k.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            Name
            <input
              ref={nameInput}
              placeholder={kindLabel(kind)}
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </label>

          {!local && !editing && (
            <fieldset className="create-columns">
              <legend>Account</legend>
              <label className="check-row">
                <input
                  type="radio"
                  name="account"
                  checked={useExisting}
                  disabled={existingTaken}
                  onChange={() => setExisting(true)}
                />
                Use the account already signed in to the CLI
              </label>
              <label className="check-row">
                <input type="radio" name="account" checked={!useExisting} onChange={() => setExisting(false)} />
                {kind === "copilot" ? "Use a GitHub token for another account" : "Sign in to a separate account"}
              </label>
              {existingTaken && <p className="muted">Another connector already uses the existing login.</p>}
            </fieldset>
          )}

          {needsToken && (
            <label className="wide">
              GitHub token
              <input
                type="password"
                autoComplete="off"
                required={!editing}
                placeholder={editing && connector.has_secret ? "Saved. Type to replace" : "github_pat_…"}
                value={secret}
                onChange={(event) => setSecret(event.target.value)}
              />
              <small>Fine-grained token with the “Copilot Requests” permission. Stored in your system keychain.</small>
            </label>
          )}

          {local && (
            <>
              <label>
                Server
                <select
                  value={presets.find((p) => p.url === baseURL)?.url ?? ""}
                  onChange={(event) => {
                    if (event.target.value) setBaseURL(event.target.value);
                    setModels(undefined);
                  }}
                >
                  {presets.map((p) => (
                    <option key={p.url} value={p.url}>
                      {p.label}
                    </option>
                  ))}
                  <option value="">Custom</option>
                </select>
              </label>
              <label>
                Base URL
                <input
                  required
                  type="url"
                  value={baseURL}
                  onChange={(event) => {
                    setBaseURL(event.target.value);
                    setModels(undefined);
                  }}
                />
              </label>
              <label>
                API key (optional)
                <input
                  type="password"
                  autoComplete="off"
                  placeholder={editing && connector.has_secret ? "Saved. Type to replace" : ""}
                  value={secret}
                  onChange={(event) => setSecret(event.target.value)}
                />
              </label>
              <label>
                Model
                {models ? (
                  <select value={model} onChange={(event) => setModel(event.target.value)}>
                    {models.length === 0 && <option value="">No models found</option>}
                    {models.map((id) => (
                      <option key={id} value={id}>
                        {id}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input value={model} placeholder="Load or type a model" onChange={(event) => setModel(event.target.value)} />
                )}
              </label>
              <button type="button" onClick={loadModels}>
                Load models
              </button>
            </>
          )}

          <div className="modal-actions">
            <button type="button" onClick={() => dialog.current?.close()}>
              Cancel
            </button>
            <button className="primary">{editing ? "Save" : "Add connector"}</button>
          </div>
        </form>
        <ErrorText error={error} />
      </section>
    </dialog>
  );
}
