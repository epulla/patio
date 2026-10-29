import { startTour } from "./Tour";
import { useEffect, useState, type FormEvent, type MouseEvent } from "react";
import { ShortcutList } from "./Shortcuts";
import {
  api,
  type AllowedFolder,
  type Bot,
  type ChatDefaults,
  type Connector,
  type McpStatus,
  type SettingsAbout,
  type SettingsRevealTarget,
} from "./api";
import { ErrorText } from "./App";
import { Icon, IconButton } from "./icons";
import Switch from "./Switch";
import { palettes, setThemePrefs, useThemePrefs, type Mode, type SidebarStyle } from "./theme";
import Picker from "./Picker";
import ModelPicker from "./ModelPicker";
import { useModels } from "./Chats";
import { openNotificationSettings, useNotificationsBlocked } from "./Notifications";
import { locale, setLanguage, useLanguage, useText, type Language } from "./i18n";

const text = {
  en: {
    allowedFolders: "Allowed folders",
    allowedFoldersHelp: "Chats can work inside these folders. A chat without a folder gets an empty folder of its own.",
    remove: (path: string) => `Remove ${path}`,
    noFolders: "No folders yet.",
    folderPath: "Folder path",
    addFolder: "Add folder",
    general: "General",
    desktopNotifications: "Desktop notifications", off: "Off", openSettings: "Open Settings",
    keyboardShortcuts: "Keyboard shortcuts",
    version: "Version",
    checkUpdates: "Check for updates", upToDate: "You're up to date.", devBuild: "Development build; updates aren't checked.", updateAvailable: (v: string) => `Patio ${v} is available.`, download: "Download", whatsNew: "What's new",
    database: "Database",
    logs: "Logs",
    showIn: (manager: string) => `Show in ${manager}`,
    fileManager: "file manager",
    copied: "Copied",
    copyPath: "Copy path",
    dataFolder: "Data folder",
    about: "about ",
    open: "Open",
    contents: "Contents",
    counts: {
      boards: "boards",
      tickets: "tickets",
      chats: "chats",
      connectors: "connectors",
      mcp_servers: "MCP servers",
      skills: "skills",
    },
    schema: "Schema",
    schemaVersion: (version: number) => `Version ${version}`,
    hiddenFields: "Hidden fields",
    shown: "Shown (-show-hidden)",
    hidden: "Hidden",
    secrets: "Secrets",
    secretsIn: (store: string) => `Stored in ${store}, not in the database or backups.`,
    source: "Source",
    dedication: "Dedication",
    dedicationText: "For Dolores, Domenica, Gary and José Martín.",
    loading: "Loading…",
    appearance: "Appearance",
    language: "Language",
    system: "System",
    mode: "Mode",
    light: "Light",
    dark: "Dark",
    sidebarStyle: "Sidebar style",
    flat: "Flat",
    gradient: "Gradient",
    palette: "Palette",
    defaults: "Defaults",
    defaultsHelp: "New chats start with these. Each chat can change them afterwards.",
    connector: "Connector",
    firstConnector: "First connector",
    model: "Model",
    loadingModels: "Loading models",
    folder: "Folder",
    scratchFolder: "Scratch folder (no files shared)",
    autoAllow: "Auto-allow file changes and commands",
    thinking: "Thinking",
    showThinking: "Show thinking",
    defaultBot: "Bot",
    noBot: "None (plain assistant)",
    showTour: "Show tour again",
    chatCleanup: "Chat cleanup",
    chatCleanupHelp:
      "Deletes chats whose last message is older than the set number of days, at startup and once a day. Pinned and running chats are kept. Ticket links to deleted chats stop working.",
    deleteOldChats: "Delete old chats automatically",
    deleteOlderThan: "Delete chats older than",
    days: "days",
    deleteWarning: "⚠️ This permanently deletes chats. Pin any chat you want to keep.",
    settings: "Settings",
    healthy: "Healthy",
    unhealthy: "Unhealthy",
    checking: "Checking…",
    backendDown: "Could not reach the app backend.",
    mcpOk: (ms: number, tools: number) => `MCP server responded in ${ms} ms with ${tools} tools. Everything is fine.`,
    mcpStatus: "MCP status",
    mcpHelp: "AI clients launch this app as an MCP server over stdio. It shares this app's database and acts as Martin.",
    lastChecked: (when: string) => `Last checked ${when} · rechecks every 30 s`,
    never: "never",
    checkAgain: "Check again",
    connectClient: "Connect an AI client",
    connectHelp: "Add this to your client's MCP config (Claude Desktop, Cursor, Claude Code…).",
    copy: "Copy",
    tools: "Tools",
  },
  es: {
    allowedFolders: "Carpetas permitidas",
    allowedFoldersHelp:
      "Los chats pueden trabajar dentro de estas carpetas. Un chat sin carpeta recibe una carpeta vacía propia.",
    remove: (path: string) => `Quitar ${path}`,
    noFolders: "Aún no hay carpetas.",
    folderPath: "Ruta de la carpeta",
    addFolder: "Agregar carpeta",
    general: "General",
    desktopNotifications: "Notificaciones de escritorio", off: "Desactivadas", openSettings: "Abrir Ajustes",
    keyboardShortcuts: "Atajos de teclado",
    version: "Versión",
    checkUpdates: "Buscar actualizaciones", upToDate: "Tienes la última versión.", devBuild: "Versión de desarrollo; no se buscan actualizaciones.", updateAvailable: (v: string) => `Patio ${v} está disponible.`, download: "Descargar", whatsNew: "Novedades",
    database: "Base de datos",
    logs: "Registros",
    showIn: (manager: string) => `Mostrar en ${manager}`,
    fileManager: "el explorador de archivos",
    copied: "Copiado",
    copyPath: "Copiar ruta",
    dataFolder: "Carpeta de datos",
    about: "aprox. ",
    open: "Abrir",
    contents: "Contenido",
    counts: {
      boards: "tableros",
      tickets: "tickets",
      chats: "chats",
      connectors: "conectores",
      mcp_servers: "servidores MCP",
      skills: "skills",
    },
    schema: "Esquema",
    schemaVersion: (version: number) => `Versión ${version}`,
    hiddenFields: "Campos ocultos",
    shown: "Visibles (-show-hidden)",
    hidden: "Ocultos",
    secrets: "Secretos",
    secretsIn: (store: string) => `Guardados en ${store}, no en la base de datos ni en los respaldos.`,
    source: "Código fuente",
    dedication: "Dedicatoria",
    dedicationText: "Para Dolores, Domenica, Gary y José Martín.",
    loading: "Cargando…",
    appearance: "Apariencia",
    language: "Idioma",
    system: "Sistema",
    mode: "Modo",
    light: "Claro",
    dark: "Oscuro",
    sidebarStyle: "Estilo de la barra lateral",
    flat: "Plano",
    gradient: "Degradado",
    palette: "Paleta",
    defaults: "Valores predeterminados",
    defaultsHelp: "Los chats nuevos empiezan con estos valores. Cada chat puede cambiarlos después.",
    connector: "Conector",
    firstConnector: "Primer conector",
    model: "Modelo",
    loadingModels: "Cargando modelos",
    folder: "Carpeta",
    scratchFolder: "Carpeta temporal (sin archivos compartidos)",
    autoAllow: "Permitir cambios de archivos y comandos automáticamente",
    thinking: "Razonamiento",
    showThinking: "Mostrar razonamiento",
    defaultBot: "Bot",
    noBot: "Ninguno (asistente simple)",
    showTour: "Ver el recorrido otra vez",
    chatCleanup: "Limpieza de chats",
    chatCleanupHelp:
      "Elimina los chats cuyo último mensaje es más antiguo que la cantidad de días indicada, al iniciar y una vez al día. Los chats fijados y en ejecución se conservan. Los enlaces de tickets a chats eliminados dejan de funcionar.",
    deleteOldChats: "Eliminar chats antiguos automáticamente",
    deleteOlderThan: "Eliminar chats con más de",
    days: "días",
    deleteWarning: "⚠️ Esto elimina chats de forma permanente. Fija los chats que quieras conservar.",
    settings: "Ajustes",
    healthy: "Funcionando",
    unhealthy: "Con problemas",
    checking: "Comprobando…",
    backendDown: "No se pudo conectar con el backend de la app.",
    mcpOk: (ms: number, tools: number) =>
      `El servidor MCP respondió en ${ms} ms con ${tools} herramientas. Todo está bien.`,
    mcpStatus: "Estado de MCP",
    mcpHelp:
      "Los clientes de IA ejecutan esta app como servidor MCP por stdio. Comparte la base de datos de esta app y actúa como Martin.",
    lastChecked: (when: string) => `Última comprobación: ${when} · se revisa cada 30 s`,
    never: "nunca",
    checkAgain: "Comprobar de nuevo",
    connectClient: "Conecta un cliente de IA",
    connectHelp: "Agrega esto a la configuración MCP de tu cliente (Claude Desktop, Cursor, Claude Code…).",
    copy: "Copiar",
    tools: "Herramientas",
  },
};

export function AllowedFolders({ onChange }: { onChange: () => void }) {
  const t = useText(text);
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
      <h2>{t.allowedFolders}</h2>
      <p className="muted">{t.allowedFoldersHelp}</p>
      <section className="panel stack folder-panel">
        {folders.length > 0 ? (
          <ul className="folder-list">
            {folders.map((folder) => (
              <li key={folder.id}>
                <code>{folder.path}</code>
                <IconButton
                  className="danger-text"
                  icon="trash"
                  label={t.remove(folder.path)}
                  onClick={() => remove(folder)}
                />
              </li>
            ))}
          </ul>
        ) : (
          <p className="muted">{t.noFolders}</p>
        )}
        <form className="inline-form" onSubmit={add}>
          <label className="grow">
            {t.folderPath}
            <input
              required
              placeholder="/Users/you/projects/app"
              value={path}
              onChange={(event) => setPath(event.target.value)}
            />
          </label>
          <button>
            <Icon name="plus" /> {t.addFolder}
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

type UpdateInfo = { current: string; latest: string; url: string; download: string; available: boolean };

function General() {
  const t = useText(text);
  const [about, setAbout] = useState<SettingsAbout>();
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState<unknown>();
  const [update, setUpdate] = useState<UpdateInfo | "checking">();
  const [updateError, setUpdateError] = useState<unknown>();
  const notificationsBlocked = useNotificationsBlocked();

  async function checkUpdates() {
    setUpdate("checking");
    setUpdateError(undefined);
    try {
      setUpdate(await api.get<UpdateInfo>("/api/update"));
    } catch (reason) {
      setUpdate(undefined);
      setUpdateError(reason);
    }
  }

  function openUrl(event: MouseEvent<HTMLAnchorElement>, url: string) {
    if (!window.runtime) return;
    event.preventDefault();
    window.runtime.BrowserOpenURL(url);
  }

  useEffect(() => {
    api.get<SettingsAbout>("/api/settings/about").then(setAbout).catch(setError);
    api.get<UpdateInfo>("/api/update").then(setUpdate).catch(() => undefined);
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

  const manager = fileManager[about?.os ?? ""] ?? t.fileManager;
  const counts = about
    ? (Object.keys(t.counts) as (keyof typeof t.counts)[]).map((key) => [t.counts[key], about.counts[key]])
    : [];

  return (
    <section className="stack">
      {update && update !== "checking" && update.available && (
        <p className="callout update-banner" role="status">
          {t.updateAvailable(update.latest)}{" "}
          <a href={update.download} target="_blank" rel="noreferrer" onClick={(event) => openUrl(event, update.download)}>
            {t.download}
          </a>{" · "}
          <a href={update.url} target="_blank" rel="noreferrer" onClick={(event) => openUrl(event, update.url)}>
            {t.whatsNew}
          </a>
        </p>
      )}
      <h2>{t.general}</h2>
      <section className="panel">
        {about ? (
          <dl className="about-list">
            <dt>{t.version}</dt>
            <dd>
              <code>{about.version}</code> · {about.os}/{about.arch}
              <span className="about-actions">
                <button type="button" onClick={checkUpdates} disabled={update === "checking"}>
                  {update === "checking" ? t.checking : t.checkUpdates}
                </button>
              </span>
              {update && update !== "checking" && !update.available && (
                <span className="muted" role="status">
                  {about.version === "dev" ? t.devBuild : t.upToDate}
                </span>
              )}
              <ErrorText error={updateError} />
            </dd>
            {notificationsBlocked && (
              <>
                <dt>{t.desktopNotifications}</dt>
                <dd>
                  {t.off}
                  <span className="about-actions">
                    <button type="button" onClick={openNotificationSettings}>
                      {t.openSettings}
                    </button>
                  </span>
                </dd>
              </>
            )}
            <dt>{t.database}</dt>
            <dd>
              <code className="about-path">{about.db_path}</code>
              <span className="about-actions">
                <button type="button" onClick={() => reveal("database")}>
                  {t.showIn(manager)}
                </button>
                <button type="button" onClick={copyPath}>
                  {copied ? t.copied : t.copyPath}
                </button>
              </span>
            </dd>
            {about.log_path && (
              <>
                <dt>{t.logs}</dt>
                <dd>
                  <code className="about-path">{about.log_path}</code>
                  <span className="about-actions">
                    <button type="button" onClick={() => reveal("logs")}>
                      {t.showIn(manager)}
                    </button>
                  </span>
                </dd>
              </>
            )}
            <dt>{t.dataFolder}</dt>
            <dd>
              <code className="about-path">{about.data_dir}</code>
              <span className="muted">
                {about.approximate ? t.about : ""}
                {formatBytes(about.data_dir_bytes)}
              </span>
              <span className="about-actions">
                <button type="button" onClick={() => reveal("data_dir")}>
                  {t.open}
                </button>
              </span>
            </dd>
            <dt>{t.contents}</dt>
            <dd>{counts.map(([label, count]) => `${count} ${label}`).join(" · ")}</dd>
            <dt>{t.schema}</dt>
            <dd>{t.schemaVersion(about.schema_version)}</dd>
            <dt>{t.hiddenFields}</dt>
            <dd>{about.show_hidden ? t.shown : t.hidden}</dd>
            <dt>{t.secrets}</dt>
            <dd>{t.secretsIn(about.secrets_store)}</dd>
            <dt>{t.source}</dt>
            <dd>
              <a href={about.repo_url} target="_blank" rel="noreferrer" onClick={openRepo}>
                {about.repo_url.replace(/^https:\/\//, "")}
              </a>
            </dd>
            <dt>{t.dedication}</dt>
            <dd>{t.dedicationText}</dd>
          </dl>
        ) : (
          !error && <p className="muted" role="status">{t.loading}</p>
        )}
        <ErrorText error={error} />
      </section>
    </section>
  );
}

export function Appearance() {
  const t = useText(text);
  const prefs = useThemePrefs();
  const language = useLanguage();
  const [error, setError] = useState<unknown>();

  async function saveLanguage(next: Language) {
    try {
      const saved = await api.put<ChatDefaults>("/api/settings/defaults", { language: next });
      setLanguage(saved.language);
      setError(undefined);
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <section className="stack">
      <h2>{t.appearance}</h2>
      <section className="panel stack">
        <div className="defaults-fields">
          <label>
            {t.language}
            <Picker
              label={t.language}
              options={[
                { id: "system", name: t.system },
                { id: "en", name: "English" },
                { id: "es", name: "Español" },
              ]}
              value={language}
              onChange={(next) => saveLanguage(next as Language)}
            />
          </label>
          <label>
            {t.mode}
            <Picker
              label={t.mode}
              options={[
                { id: "system", name: t.system },
                { id: "light", name: t.light },
                { id: "dark", name: t.dark },
              ]}
              value={prefs.mode}
              onChange={(mode) => setThemePrefs({ mode: mode as Mode })}
            />
          </label>
          <label>
            {t.sidebarStyle}
            <Picker
              label={t.sidebarStyle}
              options={[
                { id: "flat", name: t.flat },
                { id: "gradient", name: t.gradient },
              ]}
              value={prefs.style}
              onChange={(style) => setThemePrefs({ style: style as SidebarStyle })}
            />
          </label>
        </div>
        <fieldset className="palette-picker">
          <legend>{t.palette}</legend>
          {palettes.map((palette) => (
            <label key={palette.id} className="palette-option" data-palette={palette.id}>
              <input
                type="radio"
                name="palette"
                value={palette.id}
                checked={prefs.palette === palette.id}
                onChange={() => setThemePrefs({ palette: palette.id })}
              />
              <span className="palette-swatch" aria-hidden="true" />
              {palette.name}
            </label>
          ))}
        </fieldset>
        <ErrorText error={error} />
      </section>
    </section>
  );
}

export function Defaults({ foldersVersion }: { foldersVersion: number }) {
  const t = useText(text);
  const [defaults, setDefaults] = useState<ChatDefaults>();
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [folders, setFolders] = useState<AllowedFolder[]>([]);
  const [bots, setBots] = useState<Bot[]>([]);
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    Promise.all([
      api.get<ChatDefaults>("/api/settings/defaults"),
      api.get<Connector[]>("/api/connectors"),
      api.get<AllowedFolder[]>("/api/settings/folders"),
      api.get<Bot[]>("/api/bots"),
    ])
      .then(([nextDefaults, nextConnectors, nextFolders, nextBots]) => {
        setDefaults(nextDefaults);
        setConnectors(nextConnectors);
        setFolders(nextFolders);
        setBots(nextBots);
      })
      .catch(setError);
  }, [foldersVersion]);

  const { models, loading: modelsLoading } = useModels(defaults?.connector_id);

  async function save(change: Partial<ChatDefaults>) {
    try {
      setDefaults(await api.put<ChatDefaults>("/api/settings/defaults", change));
      setError(undefined);
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <section className="stack">
      <h2>{t.defaults}</h2>
      <p className="muted">{t.defaultsHelp}</p>
      <section className="panel stack defaults-panel">
        {defaults ? (
          <>
            <div className="defaults-fields">
              <label>
                {t.defaultBot}
                <Picker
                  label={t.defaultBot}
                  options={[{ id: "none", name: t.noBot }, ...bots]}
                  value={defaults.default_bot_id}
                  onChange={(default_bot_id) => save({ default_bot_id })}
                />
              </label>
              <label>
                {t.connector}
                <Picker
                  label={t.connector}
                  empty={t.firstConnector}
                  options={connectors.map((c) => ({ id: c.id, name: `${c.name} · ${c.kind}` }))}
                  value={defaults.connector_id ?? ""}
                  onChange={(id) => save({ connector_id: id || null, model: "" })}
                />
              </label>
              {defaults.connector_id && (
                <div className="field">
                  <span className="label-row">
                    {t.model} {modelsLoading && <span className="spinner" role="status" aria-label={t.loadingModels} />}
                  </span>
                  <ModelPicker
                    models={models}
                    value={defaults.model}
                    loading={modelsLoading}
                    pinKind={connectors.find((c) => c.id === defaults.connector_id)?.kind}
                    onChange={(model) => save({ model })}
                  />
                </div>
              )}
              <label>
                {t.folder}
                <Picker
                  label={t.folder}
                  empty={t.scratchFolder}
                  options={folders.map((f) => ({ id: f.path, name: f.path }))}
                  value={defaults.folder ?? ""}
                  onChange={(folder) => save({ folder: folder || null })}
                />
              </label>
            </div>
            <Switch
              className="warning"
              label={t.autoAllow}
              checked={defaults.auto_allow}
              onChange={(auto_allow) => save({ auto_allow })}
            />
            <Switch
              label={t.thinking}
              checked={defaults.thinking}
              onChange={(thinking) => save({ thinking })}
            />
            <Switch
              label={t.showThinking}
              checked={defaults.show_thinking}
              onChange={(show_thinking) => save({ show_thinking })}
            />
          </>
        ) : (
          !error && <p className="muted" role="status">{t.loading}</p>
        )}
        <ErrorText error={error} />
      </section>
    </section>
  );
}

type ChatCleanupSettings = { enabled: boolean; days: number };

export function ChatCleanup() {
  const t = useText(text);
  const [settings, setSettings] = useState<ChatCleanupSettings>();
  const [days, setDays] = useState("");
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    api
      .get<ChatCleanupSettings>("/api/settings/chat-cleanup")
      .then((loaded) => {
        setSettings(loaded);
        setDays(String(loaded.days));
      })
      .catch(setError);
  }, []);

  async function save(change: Partial<ChatCleanupSettings>) {
    if (!settings) return;
    try {
      const saved = await api.put<ChatCleanupSettings>("/api/settings/chat-cleanup", { ...settings, ...change });
      setSettings(saved);
      setDays(String(saved.days));
      setError(undefined);
    } catch (reason) {
      setError(reason);
    }
  }

  function saveDays() {
    const value = Number(days);
    if (!settings || value === settings.days) return;
    if (!Number.isInteger(value) || value < 1 || value > 3650) return setDays(String(settings.days));
    void save({ days: value });
  }

  return (
    <section className="stack">
      <h2>{t.chatCleanup}</h2>
      <p className="muted">{t.chatCleanupHelp}</p>
      <section className="panel stack defaults-panel">
        {settings ? (
          <>
            <Switch
              className="warning"
              label={t.deleteOldChats}
              checked={settings.enabled}
              onChange={(enabled) => save({ enabled })}
            />
            <label className="days-field">
              <input
                type="number"
                min={1}
                max={3650}
                step={1}
                aria-label={t.deleteOlderThan}
                value={days}
                disabled={!settings.enabled}
                onChange={(event) => setDays(event.target.value)}
                onBlur={saveDays}
                onKeyDown={(event) => event.key === "Enter" && saveDays()}
              />
              <span>{t.days}</span>
            </label>
            {settings.enabled && (
              <p className="callout warning" role="note">
                {t.deleteWarning}
              </p>
            )}
          </>
        ) : (
          !error && <p className="muted" role="status">{t.loading}</p>
        )}
        <ErrorText error={error} />
      </section>
    </section>
  );
}

export default function Settings() {
  const t = useText(text);
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
    : t.loading;

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
  const label = { up: t.healthy, down: t.unhealthy, pending: t.checking }[state];
  const detail = error ? t.backendDown : status?.ok ? t.mcpOk(status.latency_ms, status.tools.length) : status?.error;

  return (
    <div className="stack mcp-page">
      <div className="page-heading">
        <div>
          <h1>{t.settings}</h1>
        </div>
      </div>

      <General />

      <Appearance />

      <Defaults foldersVersion={foldersVersion} />
      <ChatCleanup />

      <AllowedFolders onChange={() => setFoldersVersion((value) => value + 1)} />

      <section className="stack">
        <h2>{t.keyboardShortcuts}</h2>
        <section className="panel">
          <ShortcutList />
          <button type="button" onClick={startTour}>
            {t.showTour}
          </button>
        </section>
      </section>

      <section className="stack">
        <h2>{t.mcpStatus}</h2>
        <p className="muted">{t.mcpHelp}</p>
        <section className={`panel mcp-status ${state}`}>
          <span className="status-dot" aria-hidden="true" />
          <div className="mcp-status-text" role="status" aria-live="polite">
            <strong>{label}</strong>
            {detail && <span className="muted">{detail}</span>}
            <small>{t.lastChecked(checked ? checked.toLocaleTimeString(locale()) : t.never)}</small>
          </div>
          <button type="button" onClick={() => setRefresh((value) => value + 1)}>
            {t.checkAgain}
          </button>
        </section>
        <ErrorText error={error} />

        <section className="panel">
          <div className="mcp-section-head">
            <div>
              <h3>{t.connectClient}</h3>
              <p className="muted">{t.connectHelp}</p>
            </div>
            <button type="button" onClick={copyConfig} disabled={!status}>
              {copied ? t.copied : t.copy}
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
              {t.tools} <small>{status.tools.length}</small>
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
