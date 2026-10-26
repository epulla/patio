import { confirmDialog } from "./confirm";
import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  api,
  type Connector,
  type ConnectorKind,
  type ConnectorQuota,
  type ConnectorStatus,
} from "./api";
import { ErrorText, navigate } from "./App";
import { Icon, IconButton } from "./icons";
import Picker from "./Picker";
import History from "./History";
import { lang, locale, useText } from "./i18n";

const text = {
  en: {
    localAI: "Local AI",
    noConnectors: "No connectors yet", first: (then: string) => `Add a connector first (Claude Code, Codex, Copilot, opencode, or Local AI), then ${then}.`, go: "Go to Connectors",
    connectors: "Connectors", add: "Add connector", intro: "Claude Code, Codex, Copilot, and opencode connectors use the official CLI installed on this computer, signed in with your own account. Their providers' terms apply.", empty: "No connectors yet. Patio's AI features need one: install and sign in to a CLI (Claude Code, Codex, Copilot, opencode), or use a local model. Then add it here.",
    finishBrowser: "Finish signing in in your browser, then press Refresh.", finishTerminal: "Finish signing in in the Terminal window, then press Refresh.", testing: "Testing…", connected: (n: number, found: boolean, model: string) => `Connected. ${n} model${n === 1 ? "" : "s"} available${found ? "" : `; "${model}" not found`}.`, deleteQuestion: (name: string) => `Delete connector "${name}"?`, noModel: "No model selected", checking: "Checking…", notInstalled: "Not installed", notSignedIn: "Not signed in", signedIn: "Signed in", signInUnknown: "Sign-in unknown", edit: (name: string) => `Edit ${name}`, editText: "Edit", delete: (name: string) => `Delete ${name}`, install: "Install it in a terminal:", installGuide: "Install guide", restart: "Just installed it? Restart Patio.", test: "Test", login: "Log in", refresh: "Refresh", separate: "separate account", existingLogin: "existing login", token: "token",
    agoNow: "just now", agoMin: (n: number) => `${n} min ago`, agoHour: (n: number) => `${n} h ago`, unknown: "unknown", used: "used", resets: "Resets", premium: "premium requests this month", tokensIn: "tokens in", out: "out", updated: "Updated", claudeUsage: "Claude reports usage after the first reply of each chat",
    editTitle: "Edit connector", addTitle: "Add connector", close: "Close", type: "Type", name: "Name", account: "Account", useCLI: "Use the account already signed in to the CLI", githubOther: "Use a GitHub token for another account", separateLogin: "Sign in to a separate account", existingTaken: "Another connector already uses the existing login.", githubToken: "GitHub token", savedReplace: "Saved. Type to replace", tokenPlaceholder: "github_pat_…", tokenHelp: "Fine-grained token with the “Copilot Requests” permission. Stored in your system keychain.", server: "Server", custom: "Custom", baseURL: "Base URL", apiKey: "API key (optional)", loadingModels: "Loading models", reloadModels: "Reload models", noModels: "No models found", modelPlaceholder: "Type a model", loadModelsError: "Couldn't load models: ", cancel: "Cancel", save: "Save",
    model: "Model", notFound: "not found", modelHint: "Small and fast on 8 GB:", and: "or", modelHintEnd: "Both support tools; neither thinks.",
  },
  es: {
    localAI: "IA local",
    noConnectors: "Aún no hay conectores", first: (then: string) => `Primero agrega un conector (Claude Code, Codex, Copilot, opencode o IA local), luego ${then}.`, go: "Ir a Conectores",
    connectors: "Conectores", add: "Agregar conector", intro: "Los conectores de Claude Code, Codex, Copilot y opencode usan la CLI oficial instalada en esta computadora, con tu propia cuenta. Se aplican los términos de sus proveedores.", empty: "Aún no hay conectores. Las funciones de IA de Patio necesitan uno: instala una CLI e inicia sesión (Claude Code, Codex, Copilot, opencode), o usa un modelo local. Luego agrégalo aquí.",
    finishBrowser: "Termina de iniciar sesión en tu navegador y luego presiona Actualizar.", finishTerminal: "Termina de iniciar sesión en la ventana de Terminal y luego presiona Actualizar.", testing: "Probando…", connected: (n: number, found: boolean, model: string) => `Conectado. Hay ${n} modelo${n === 1 ? "" : "s"} disponible${n === 1 ? "" : "s"}${found ? "" : `; no se encontró «${model}»`}.`, deleteQuestion: (name: string) => `¿Eliminar el conector «${name}»?`, noModel: "No hay un modelo seleccionado", checking: "Comprobando…", notInstalled: "No está instalado", notSignedIn: "No has iniciado sesión", signedIn: "Sesión iniciada", signInUnknown: "Estado de sesión desconocido", edit: (name: string) => `Editar ${name}`, editText: "Editar", delete: (name: string) => `Eliminar ${name}`, install: "Instálalo en una terminal:", installGuide: "Guía de instalación", restart: "¿Acabas de instalarlo? Reinicia Patio.", test: "Probar", login: "Iniciar sesión", refresh: "Actualizar", separate: "cuenta separada", existingLogin: "sesión existente", token: "token",
    agoNow: "hace un momento", agoMin: (n: number) => `hace ${n} min`, agoHour: (n: number) => `hace ${n} h`, unknown: "desconocido", used: "usado", resets: "Se restablece", premium: "solicitudes premium este mes", tokensIn: "tokens de entrada", out: "de salida", updated: "Actualizado", claudeUsage: "Claude informa el uso después de la primera respuesta de cada chat",
    editTitle: "Editar conector", addTitle: "Agregar conector", close: "Cerrar", type: "Tipo", name: "Nombre", account: "Cuenta", useCLI: "Usar la cuenta que ya inició sesión en la CLI", githubOther: "Usar un token de GitHub para otra cuenta", separateLogin: "Iniciar sesión con una cuenta separada", existingTaken: "Otro conector ya usa la sesión existente.", githubToken: "Token de GitHub", savedReplace: "Guardado. Escribe para reemplazarlo", tokenPlaceholder: "github_pat_…", tokenHelp: "Token detallado con el permiso “Copilot Requests”. Se guarda en el llavero del sistema.", server: "Servidor", custom: "Personalizado", baseURL: "URL base", apiKey: "Clave de API (opcional)", loadingModels: "Cargando modelos", reloadModels: "Recargar modelos", noModels: "No se encontraron modelos", modelPlaceholder: "Escribe un modelo", loadModelsError: "No se pudieron cargar los modelos: ", cancel: "Cancelar", save: "Guardar",
    model: "Modelo", notFound: "no encontrado", modelHint: "Pequeño y rápido con 8 GB:", and: "o", modelHintEnd: "Ambos admiten herramientas; ninguno piensa.",
  },
};

export function ConnectorRequired({ then }: { then: string }) {
  const t = useText(text);
  return (
    <section className="panel skill-empty">
      <span className="skill-empty-mark" aria-hidden="true">
        <Icon name="connector" />
      </span>
      <h2>{t.noConnectors}</h2>
      <p className="muted">
        {t.first(then)}
      </p>
      <button onClick={() => navigate("/ai/connectors")}>{t.go}</button>
    </section>
  );
}

const kinds: { kind: ConnectorKind; label: string }[] = [
  { kind: "claude", label: "Claude Code" },
  { kind: "codex", label: "Codex" },
  { kind: "copilot", label: "GitHub Copilot" },
  { kind: "opencode", label: "opencode" },
  { kind: "local", label: "Local AI" },
];
const kindLabel = (kind: ConnectorKind) => (kind === "local" ? text[lang()].localAI : kinds.find((k) => k.kind === kind)?.label ?? kind);

const presets = [
  { label: "Ollama", url: "http://localhost:11434/v1" },
  { label: "LM Studio", url: "http://localhost:1234/v1" },
  { label: "llama.cpp", url: "http://127.0.0.1:8080/v1" },
];

export default function Connectors({ refresh, embedded }: { refresh: number; embedded?: boolean }) {
  const t = useText(text);
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
      {!embedded && (
        <div className="page-heading">
          <div>
            <p className="eyebrow">AI</p>
            <h1>{t.connectors}</h1>
          </div>
          <button className="primary" onClick={() => setEditing("new")}>
            <Icon name="plus" /> {t.add}
          </button>
        </div>
      )}
      <p className="muted">
        {t.intro}
      </p>
      <ErrorText error={error} />
      {embedded && (
        <button className="connector-add" onClick={() => setEditing("new")}>
          <Icon name="plus" /> {t.add}
        </button>
      )}
      {connectors.length === 0 && !error && !embedded && (
        <p className="callout">{t.empty}</p>
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
  const t = useText(text);
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

  useEffect(() => {
    let fetchedAt = Date.now();
    const onFocus = () => {
      if (document.visibilityState !== "visible" || Date.now() - fetchedAt < 60_000) return;
      fetchedAt = Date.now();
      api
        .get<ConnectorQuota>(`/api/connectors/${connector.id}/quota`)
        .then(setQuota)
        .catch(() => undefined);
    };
    addEventListener("focus", onFocus);
    document.addEventListener("visibilitychange", onFocus);
    return () => {
      removeEventListener("focus", onFocus);
      document.removeEventListener("visibilitychange", onFocus);
    };
  }, [connector.id]);

  async function login() {
    setError(undefined);
    try {
      const result = await api.post<{ auth_url?: string }>(`/api/connectors/${connector.id}/login`, {});
      setMessage(
        result.auth_url
          ? t.finishBrowser
          : t.finishTerminal,
      );
    } catch (reason) {
      setError(reason);
    }
  }

  async function test() {
    setError(undefined);
    setMessage(t.testing);
    try {
      const result = await api.post<{ models: string[] }>("/api/connectors/local-models", {
        id: connector.id,
        base_url: connector.base_url,
      });
      const found = !connector.model || result.models.includes(connector.model);
      setMessage(
        t.connected(result.models.length, found, connector.model ?? ""),
      );
    } catch (reason) {
      setMessage("");
      setError(reason);
    }
  }

  async function remove() {
    if (!(await confirmDialog(text[lang()].deleteQuestion(connector.name)))) return;
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
    ? connector.model || t.noModel
    : !status
      ? t.checking
      : status.installed === false
        ? t.notInstalled
        : status.logged_in === false
          ? t.notSignedIn
          : status.logged_in
            ? [status.account, status.plan].filter(Boolean).join(" · ") || t.signedIn
            : t.signInUnknown;

  return (
    <article className={`panel connector-card ${state}`} aria-labelledby={`connector-${connector.id}`}>
      <header className="connector-head">
        <div>
          <p className="eyebrow">
            {kindLabel(connector.kind)}
            {!local && (connector.config_dir ? (pat ? ` · ${t.token}` : ` · ${t.separate}`) : ` · ${t.existingLogin}`)}
          </p>
          <h2 id={`connector-${connector.id}`}>{connector.name}</h2>
        </div>
        <div className="connector-actions">
          <button aria-label={t.edit(connector.name)} onClick={onEdit}>
            {t.editText}
          </button>
          <IconButton className="danger-text" icon="trash" label={t.delete(connector.name)} onClick={remove} />
        </div>
      </header>

      <p className="connector-state" role="status">
        <span className="state-dot" aria-hidden="true" />
        <span>{stateLabel}</span>
        {status?.version && <small>{status.version}</small>}
      </p>
      {local && <small className="connector-url">{connector.base_url}</small>}
      {local && <ModelHint />}
      {status?.note && <p className="muted connector-note">{status.note}</p>}

      {status?.install && (
        <div className="connector-install">
          <p>{t.install}</p>
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
            {t.installGuide}
          </a>
          {status.installed === false && <p className="muted">{t.restart}</p>}
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
          <button onClick={test}>{t.test}</button>
        ) : (
          !pat &&
          status?.installed !== false && <button onClick={login}>{t.login}</button>
        )}
        <button
          onClick={() => {
            setMessage("");
            setError(undefined);
            setCheck((value) => value + 1);
          }}
        >
          {t.refresh}
        </button>
      </div>
    </article>
  );
}

export function formatAgo(value: string) {
  const minutes = Math.round((Date.now() - new Date(value).getTime()) / 60_000);
  if (minutes < 1) return text[lang()].agoNow;
  if (minutes < 60) return text[lang()].agoMin(minutes);
  const hours = Math.round(minutes / 60);
  if (hours < 24) return text[lang()].agoHour(hours);
  return new Date(value).toLocaleDateString(locale());
}

function Quota({ quota }: { quota: ConnectorQuota }) {
  const t = useText(text);
  const windows = quota.windows ?? [];
  return (
    <div className="quota">
      {windows.map((window) => {
        const used = Math.max(0, Math.min(100, window.used_percent ?? 0));
        return (
          <div className="quota-row" key={window.label}>
            <div className="quota-label">
              <span>{window.label}</span>
              <span>{window.used_percent == null ? t.unknown : `${Math.round(used)}% ${t.used}`}</span>
            </div>
            <div
              className="quota-bar"
              role="progressbar"
              aria-label={`${window.label} ${t.used}`}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={window.used_percent == null ? undefined : Math.round(used)}
            >
              <span style={{ width: `${used}%` }} data-high={used >= 80 || undefined} />
            </div>
            {window.resets_at && <small>{t.resets} {new Date(window.resets_at).toLocaleString(locale())}</small>}
          </div>
        );
      })}
      {quota.requests != null && <p className="muted">{quota.requests} {t.premium}</p>}
      {quota.tokens && (
        <p className="muted">
          {quota.tokens.input.toLocaleString(locale())} {t.tokensIn} · {quota.tokens.output.toLocaleString(locale())} {t.out} · $
          {quota.tokens.cost.toFixed(2)}
        </p>
      )}
      {quota.note && <p className="muted">{quota.note}</p>}
      {quota.updated_at && windows.length > 0 && (
        <small title={new Date(quota.updated_at).toLocaleString(locale())}>
          {t.updated} {formatAgo(quota.updated_at)}
          {quota.kind === "claude" && ` · ${t.claudeUsage}`}
        </small>
      )}
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
  const t = useText(text);
  const [kind, setKind] = useState<ConnectorKind>(connector?.kind ?? "claude");
  const [name, setName] = useState(connector?.name ?? "");
  const [existing, setExisting] = useState(!existingLoginKinds.includes(connector?.kind ?? "claude"));
  const [baseURL, setBaseURL] = useState(connector?.base_url ?? presets[0].url);
  const [model, setModel] = useState(connector?.model ?? "");
  const [models, setModels] = useState<string[]>();
  const [modelsLoading, setModelsLoading] = useState(false);
  const [modelsError, setModelsError] = useState("");
  const modelsRequest = useRef(0);
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
    const request = ++modelsRequest.current;
    setModelsLoading(true);
    setModelsError("");
    try {
      const result = await api.post<{ models: string[] }>("/api/connectors/local-models", {
        base_url: baseURL.trim(),
        ...(secret ? { secret } : {}),
        ...(connector ? { id: connector.id } : {}),
      });
      if (request !== modelsRequest.current) return;
      setModels(result.models);
      setModel((current) => current || result.models[0] || "");
    } catch (reason) {
      if (request !== modelsRequest.current) return;
      setModels(undefined);
      setModelsError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      if (request === modelsRequest.current) setModelsLoading(false);
    }
  }

  useEffect(() => {
    if (!local || !validURL(baseURL.trim())) return;
    const timer = setTimeout(loadModels, 400);
    return () => clearTimeout(timer);
  }, [local, baseURL, secret]);

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
          <h2 id="connector-dialog-title">{editing ? t.editTitle : t.addTitle}</h2>
          <IconButton type="button" className="ghost" icon="x" label={t.close} onClick={() => dialog.current?.close()} />
        </div>
        <form onSubmit={submit}>
          <label>
            {t.type}
            <Picker
              label={t.type}
              options={kinds.map((k) => ({ id: k.kind, name: k.label }))}
              value={kind}
              disabled={editing}
              onChange={(k) => changeKind(k as ConnectorKind)}
            />
          </label>
          <label>
            {t.name}
            <input
              ref={nameInput}
              placeholder={kindLabel(kind)}
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </label>

          {!local && !editing && (
            <fieldset className="create-columns">
              <legend>{t.account}</legend>
              <label className="check-row">
                <input
                  type="radio"
                  name="account"
                  checked={useExisting}
                  disabled={existingTaken}
                  onChange={() => setExisting(true)}
                />
                {t.useCLI}
              </label>
              <label className="check-row">
                <input type="radio" name="account" checked={!useExisting} onChange={() => setExisting(false)} />
                {kind === "copilot" ? t.githubOther : t.separateLogin}
              </label>
              {existingTaken && <p className="muted">{t.existingTaken}</p>}
            </fieldset>
          )}

          {needsToken && (
            <label className="wide">
              {t.githubToken}
              <input
                type="password"
                autoComplete="off"
                required={!editing}
                placeholder={editing && connector.has_secret ? t.savedReplace : t.tokenPlaceholder}
                value={secret}
                onChange={(event) => setSecret(event.target.value)}
              />
              <small>{t.tokenHelp}</small>
            </label>
          )}

          {local && (
            <>
              <label>
                {t.server}
                <Picker
                  label={t.server}
                  options={[...presets.map((p) => ({ id: p.url, name: p.label })), { id: "", name: t.custom }]}
                  value={presets.find((p) => p.url === baseURL)?.url ?? ""}
                  onChange={(url) => {
                    if (url) setBaseURL(url);
                  }}
                />
              </label>
              <label>
                {t.baseURL}
                <input
                  required
                  type="url"
                  value={baseURL}
                  onChange={(event) => setBaseURL(event.target.value)}
                />
              </label>
              <label>
                {t.apiKey}
                <input
                  type="password"
                  autoComplete="off"
                  placeholder={editing && connector.has_secret ? t.savedReplace : ""}
                  value={secret}
                  onChange={(event) => setSecret(event.target.value)}
                />
              </label>
              <div className="field">
                <span className="label-row">
                  <label htmlFor="local-model">{t.model}</label>
                  {modelsLoading ? (
                    <span className="spinner" role="status" aria-label={t.loadingModels} />
                  ) : (
                    <IconButton
                      type="button"
                      className="ghost"
                      icon="refresh"
                      label={t.reloadModels}
                      onClick={loadModels}
                    />
                  )}
                </span>
                {models ? (
                  <Picker
                    id="local-model"
                     label={t.model}
                    empty={models.length === 0 ? t.noModels : undefined}
                    options={[
                      ...models.map((id) => ({ id, name: id })),
                       ...(model && !models.includes(model) ? [{ id: model, name: `${model} (${t.notFound})` }] : []),
                    ]}
                    value={model}
                    onChange={setModel}
                  />
                ) : (
                  <input id="local-model" value={model} placeholder={t.modelPlaceholder} onChange={(event) => setModel(event.target.value)} />
                )}
                {modelsError && <small className="muted">{t.loadModelsError}{modelsError}</small>}
              </div>
              <ModelHint />
            </>
          )}

          <div className="modal-actions">
            <button type="button" onClick={() => dialog.current?.close()}>
              {t.cancel}
            </button>
            <button className="primary">{editing ? t.save : t.add}</button>
          </div>
        </form>
        <ErrorText error={error} />
        {connector && <History itemType="connector" itemId={connector.id} />}
      </section>
    </dialog>
  );
}

function ModelHint() {
  const t = useText(text);
  return (
    <small className="muted wide model-hint">
      {t.modelHint} <code>qwen2.5:3b</code> {text[lang()].and} <code>llama3.2:3b</code> (<code>ollama pull &lt;name&gt;</code>).
      {" "}{t.modelHintEnd}
    </small>
  );
}

function validURL(value: string) {
  try {
    return ["http:", "https:"].includes(new URL(value).protocol);
  } catch {
    return false;
  }
}
