import { useEffect, useState, type FormEvent } from "react";
import { api } from "./api";
import { ErrorText } from "./App";
import Switch from "./Switch";
import { useText } from "./i18n";

type TelegramSettings = { enabled: boolean; allowed_user_ids: number[]; has_token: boolean };

const text = {
  en: {
    telegram: "Telegram",
    help: "Chat with Patio from Telegram. Create a bot with @BotFather, paste its token here, then message the bot. Patio must be running; replies use your default connector and bot. Commands: /new starts a fresh chat, /chat shows the current one, /continue picks a recent chat (or /continue <chat link>).",
    enabled: "Answer Telegram messages",
    token: "Bot token",
    tokenSaved: "Saved · type to replace",
    saveToken: "Save token",
    removeToken: "Remove token",
    allowed: "Allowed Telegram user IDs",
    allowedHelp: "Separate with commas. Message the bot once: it replies with your ID.",
    invalidIds: "User IDs must be numbers.",
    test: "Test",
    testing: "Testing…",
    connected: (name: string) => `Connected as @${name}`,
    notConnected: "Not connected",
    checking: "Checking…",
    loading: "Loading…",
  },
  es: {
    telegram: "Telegram",
    help: "Chatea con Patio desde Telegram. Crea un bot con @BotFather, pega aquí su token y escríbele al bot. Patio debe estar abierto; las respuestas usan tu conector y bot por defecto. Comandos: /new inicia un chat nuevo, /chat muestra el actual, /continue elige un chat reciente (o /continue <enlace del chat>).",
    enabled: "Responder mensajes de Telegram",
    token: "Token del bot",
    tokenSaved: "Guardado · escribe para reemplazarlo",
    saveToken: "Guardar token",
    removeToken: "Quitar token",
    allowed: "IDs de usuario de Telegram permitidos",
    allowedHelp: "Sepáralos con comas. Escríbele una vez al bot: te responde con tu ID.",
    invalidIds: "Los IDs de usuario deben ser números.",
    test: "Probar",
    testing: "Probando…",
    connected: (name: string) => `Conectado como @${name}`,
    notConnected: "Sin conectar",
    checking: "Comprobando…",
    loading: "Cargando…",
  },
};

export default function Telegram() {
  const t = useText(text);
  const [settings, setSettings] = useState<TelegramSettings>();
  const [token, setToken] = useState("");
  const [ids, setIds] = useState("");
  const [bot, setBot] = useState<string>();
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState<string>();
  const [error, setError] = useState<unknown>();

  function load(loaded: TelegramSettings) {
    setSettings(loaded);
    setIds(loaded.allowed_user_ids.join(", "));
  }

  useEffect(() => {
    api
      .get<TelegramSettings>("/api/settings/telegram")
      .then((loaded) => {
        load(loaded);
        if (loaded.has_token) void test(null);
      })
      .catch(setError);
  }, []);

  async function save(change: { enabled?: boolean; allowed_user_ids?: number[]; token?: string }) {
    if (!settings) return;
    try {
      const saved = await api.put<TelegramSettings>("/api/settings/telegram", {
        enabled: settings.enabled,
        allowed_user_ids: settings.allowed_user_ids,
        token: null,
        ...change,
      });
      load(saved);
      setError(undefined);
      if (change.token !== undefined) {
        setToken("");
        setBot(undefined);
        setTestError(undefined);
        if (saved.has_token) void test(null);
      }
    } catch (reason) {
      setError(reason);
    }
  }

  function saveToken(event: FormEvent) {
    event.preventDefault();
    if (token.trim()) void save({ token: token.trim() });
  }

  function saveIds() {
    if (!settings) return;
    const parts = ids.split(/[\s,]+/).filter(Boolean);
    if (parts.some((part) => !/^\d+$/.test(part))) return setError(new Error(t.invalidIds));
    const next = parts.map(Number);
    if (next.join() === settings.allowed_user_ids.join()) return;
    void save({ allowed_user_ids: next });
  }

  async function test(candidate: string | null) {
    setTesting(true);
    try {
      const result = await api.post<{ username: string }>("/api/settings/telegram/test", { token: candidate });
      setBot(result.username);
      setTestError(undefined);
    } catch (reason) {
      setBot(undefined);
      setTestError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      setTesting(false);
    }
  }

  const state = bot ? "up" : testError ? "down" : "pending";
  const label = testing ? t.checking : bot ? t.connected(bot) : (testError ?? t.notConnected);

  return (
    <section className="stack">
      <h2>{t.telegram}</h2>
      <p className="muted">{t.help}</p>
      <section className={`panel mcp-status ${state}`}>
        <span className="status-dot" aria-hidden="true" />
        <div className="mcp-status-text" role="status" aria-live="polite">
          <strong>{label}</strong>
        </div>
        <button type="button" disabled={testing || (!token.trim() && !settings?.has_token)} onClick={() => test(token.trim() || null)}>
          {testing ? t.testing : t.test}
        </button>
      </section>
      <section className="panel stack defaults-panel">
        {settings ? (
          <>
            <form className="inline-form" onSubmit={saveToken}>
              <label className="grow">
                {t.token}
                <input
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={settings.has_token ? t.tokenSaved : ""}
                  value={token}
                  onChange={(event) => setToken(event.target.value)}
                />
              </label>
              <button disabled={!token.trim()}>{t.saveToken}</button>
              {settings.has_token && (
                <button type="button" className="danger-text" onClick={() => save({ token: "" })}>
                  {t.removeToken}
                </button>
              )}
            </form>
            <div className="inline-form">
              <label className="grow">
                {t.allowed}
                <input
                  inputMode="numeric"
                  spellCheck={false}
                  value={ids}
                  onChange={(event) => setIds(event.target.value)}
                  onBlur={saveIds}
                  onKeyDown={(event) => event.key === "Enter" && saveIds()}
                />
                <small className="muted">{t.allowedHelp}</small>
              </label>
            </div>
            <Switch label={t.enabled} checked={settings.enabled} onChange={(enabled) => save({ enabled })} />
          </>
        ) : (
          !error && <p className="muted" role="status">{t.loading}</p>
        )}
        <ErrorText error={error} />
      </section>
    </section>
  );
}
