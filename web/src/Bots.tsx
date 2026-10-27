import { confirmDialog } from "./confirm";
import { useEffect, useState, type FormEvent } from "react";
import { api, type Bot, type ChatDefaults, type Connector } from "./api";
import { ErrorText, navigate } from "./App";
import { startModel, useModels } from "./Chats";
import { ConnectorRequired } from "./Connectors";
import ModelPicker from "./ModelPicker";
import Picker from "./Picker";
import History from "./History";
import { forgetMentionUsers } from "./mentions";
import { Icon, IconButton } from "./icons";
import { useText } from "./i18n";

const text = {
  en: {
    bots: "Bots", new: "New bot", help: "Bots are AI personas with their own name, instructions, connector and model. Mention one with @ in a chat; new chats start with the default bot from Settings.",
    empty: "No bots yet", count: (n: number) => `${n} bot${n === 1 ? "" : "s"}`, back: "Bots", gone: "This bot no longer exists.", create: "create a bot",
    name: "Name", nameTitle: "One word, no spaces: it's how you @mention the bot", description: "Role", descriptionPlaceholder: "Developer: writes and tests code",
    persona: "Persona prompt", personaPlaceholder: "You are Billy, a careful developer. Keep changes small and always run the tests.",
    color: "Color", connector: "Connector", model: "Model", loadingModels: "Loading models", noConnector: "No connector",
    save: "Save", saved: "Saved", createBot: "Create bot", delete: (name: string) => `Delete ${name}`, confirmDelete: (name: string) => `Delete bot "${name}"?`, protected: "Martin is the built-in bot; you can edit him but not delete him.",
  },
  es: {
    bots: "Bots", new: "Nuevo bot", help: "Los bots son personalidades de IA con su propio nombre, instrucciones, conector y modelo. Menciona uno con @ en un chat; los chats nuevos empiezan con el bot predeterminado de Ajustes.",
    empty: "Aún no hay bots", count: (n: number) => `${n} bot${n === 1 ? "" : "s"}`, back: "Bots", gone: "Este bot ya no existe.", create: "crear un bot",
    name: "Nombre", nameTitle: "Una palabra, sin espacios: así lo mencionas con @", description: "Rol", descriptionPlaceholder: "Desarrollador: escribe y prueba código",
    persona: "Instrucciones de personalidad", personaPlaceholder: "Eres Billy, un desarrollador cuidadoso. Haz cambios pequeños y siempre ejecuta las pruebas.",
    color: "Color", connector: "Conector", model: "Modelo", loadingModels: "Cargando modelos", noConnector: "Sin conector",
    save: "Guardar", saved: "Guardado", createBot: "Crear bot", delete: (name: string) => `Eliminar ${name}`, confirmDelete: (name: string) => `¿Eliminar el bot "${name}"?`, protected: "Martin es el bot incluido; puedes editarlo, pero no eliminarlo.",
  },
};

export default function Bots({ id, refresh }: { id?: string; refresh: number }) {
  const t = useText(text);
  const [bots, setBots] = useState<Bot[]>();
  const [connectors, setConnectors] = useState<Connector[]>();
  const [error, setError] = useState<unknown>();
  const [reload, setReload] = useState(0);

  useEffect(() => {
    Promise.all([api.get<Bot[]>("/api/bots"), api.get<Connector[]>("/api/connectors")])
      .then(([list, connectorList]) => {
        setBots(list);
        setConnectors(connectorList);
        setError(undefined);
      })
      .catch(setError);
  }, [refresh, reload]);

  if (id) {
    const bot = bots?.find((b) => b.id === id);
    if (id !== "new" && !bot) {
      return bots ? (
        <div className="stack">
          <BackLink />
          <p className="muted">{t.gone}</p>
        </div>
      ) : null;
    }
    return (
      <BotEditor
        key={id}
        bot={bot}
        connectors={connectors}
        refresh={refresh}
        onSaved={(saved) => {
          forgetMentionUsers();
          setReload((value) => value + 1);
          if (!bot) navigate(`/ai/bots/${saved.id}`);
        }}
        onDeleted={() => {
          forgetMentionUsers();
          setReload((value) => value + 1);
          navigate("/ai/bots");
        }}
      />
    );
  }

  const noConnectors = connectors?.length === 0;
  const newButton = !noConnectors && (
    <button className="primary" onClick={() => navigate("/ai/bots/new")}>
      <Icon name="plus" /> {t.new}
    </button>
  );

  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <p className="eyebrow">AI</p>
          <h1>{t.bots}</h1>
        </div>
        <div className="actions">{newButton}</div>
      </div>
      <p className="muted">{t.help}</p>
      <ErrorText error={error} />
      {noConnectors && <ConnectorRequired then={t.create} />}
      {!!bots?.length && (
        <>
          <div className="list-toolbar">
            <span className="muted">{t.count(bots.length)}</span>
          </div>
          <ul className="panel skill-list" aria-label={t.bots}>
            {bots.map((bot) => (
              <li key={bot.id}>
                <button className="skill-item" onClick={() => navigate(`/ai/bots/${bot.id}`)}>
                  <span className="skill-name">
                    <span className="bot-dot" style={{ background: bot.color || "var(--muted)" }} aria-hidden="true" /> {bot.name}
                  </span>
                  <span className="skill-description">
                    {bot.description}
                    {bot.description && connectors && " · "}
                    {connectors?.find((c) => c.id === bot.connector_id)?.name ?? ""}
                    {bot.model && ` · ${bot.model}`}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
      {bots?.length === 0 && !noConnectors && <p className="muted">{t.empty}</p>}
    </div>
  );
}

function BackLink() {
  const t = useText(text);
  return (
    <button type="button" className="back" onClick={() => navigate("/ai/bots")}>
      <Icon name="arrowLeft" /> {t.back}
    </button>
  );
}

function BotEditor({
  bot,
  connectors,
  refresh,
  onSaved,
  onDeleted,
}: {
  bot?: Bot;
  connectors?: Connector[];
  refresh: number;
  onSaved: (bot: Bot) => void;
  onDeleted: () => void;
}) {
  const t = useText(text);
  const [name, setName] = useState(bot?.name ?? "");
  const [description, setDescription] = useState(bot?.description ?? "");
  const [persona, setPersona] = useState(bot?.persona_prompt ?? "");
  const [color, setColor] = useState(bot?.color || "#3b82f6");
  const [connectorID, setConnectorID] = useState(bot?.connector_id ?? "");
  const [model, setModel] = useState(bot?.model ?? "");
  const [defaults, setDefaults] = useState<ChatDefaults>();
  const [error, setError] = useState<unknown>();
  const [saved, setSaved] = useState(false);
  const { models, loading: modelsLoading } = useModels(connectorID || null);

  useEffect(() => {
    api
      .get<ChatDefaults>("/api/settings/defaults")
      .then(setDefaults)
      .catch(setError);
  }, []);

  // Bots without a connector (e.g. a freshly seeded Martin) start from the Settings default.
  useEffect(() => {
    if (!defaults || !connectors || connectorID) return;
    const id = defaults.connector_id && connectors.some((c) => c.id === defaults.connector_id) ? defaults.connector_id : connectors[0]?.id ?? "";
    setConnectorID(id);
    setModel(startModel(connectors, defaults, id));
  }, [defaults, connectors]);

  const fields = { name: name.trim(), description, persona_prompt: persona, color, connector_id: connectorID || null, model };
  const dirty = !bot || Object.entries(fields).some(([key, value]) => value !== bot[key as keyof Bot]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const result = bot ? await api.patch<Bot>(`/api/bots/${bot.id}`, fields) : await api.post<Bot>("/api/bots", fields);
      setError(undefined);
      setSaved(true);
      window.setTimeout(() => setSaved(false), 1500);
      onSaved(result);
    } catch (reason) {
      setError(reason);
    }
  }

  async function remove() {
    if (!bot || !(await confirmDialog(t.confirmDelete(bot.name)))) return;
    try {
      await api.delete(`/api/bots/${bot.id}`);
      onDeleted();
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <div className="stack">
      <BackLink />
      <div className="page-heading">
        <h1>{bot ? bot.name : t.new}</h1>
      </div>
      <form className="panel skill-editor" onSubmit={submit} aria-label={bot ? bot.name : t.new}>
        <div className="skill-fields">
          <label>
            {t.name}
            <input autoFocus={!bot} required pattern="\S+" title={t.nameTitle} placeholder="Billy" value={name} onChange={(event) => setName(event.target.value)} />
          </label>
          <label className="grow">
            {t.description}
            <input placeholder={t.descriptionPlaceholder} value={description} onChange={(event) => setDescription(event.target.value)} />
          </label>
          <label>
            {t.color}
            <input type="color" value={color} onChange={(event) => setColor(event.target.value)} />
          </label>
        </div>
        <div className="skill-fields">
          <label>
            {t.connector}
            <Picker
              label={t.connector}
              empty={connectors?.length === 0 || !connectorID ? t.noConnector : undefined}
              options={(connectors ?? []).map((c) => ({ id: c.id, name: `${c.name} · ${c.kind}` }))}
              value={connectorID}
              onChange={(id) => {
                setConnectorID(id);
                setModel(startModel(connectors ?? [], undefined, id));
              }}
            />
          </label>
          <div className="field">
            <span className="label-row">
              {t.model} {modelsLoading && <span className="spinner" role="status" aria-label={t.loadingModels} />}
            </span>
            <ModelPicker models={models} value={model} loading={modelsLoading} pinKind={connectors?.find((c) => c.id === connectorID)?.kind} onChange={setModel} />
          </div>
        </div>
        <label>
          {t.persona}
          <textarea rows={8} placeholder={t.personaPlaceholder} value={persona} onChange={(event) => setPersona(event.target.value)} />
        </label>
        {bot?.id === "ai" && <p className="muted">{t.protected}</p>}
        <ErrorText error={error} />
        <div className="connector-buttons">
          <button className="primary" disabled={!dirty || !connectorID}>
            {saved ? t.saved : bot ? t.save : t.createBot}
          </button>
          {bot && bot.id !== "ai" && <IconButton type="button" className="danger-text" icon="trash" label={t.delete(bot.name)} onClick={remove} />}
        </div>
      </form>
      {bot && <History itemType="bot" itemId={bot.id} refresh={refresh} />}
    </div>
  );
}
