import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, type Board, type Connector } from "./api";
import { ErrorText, navigate } from "./App";
import Connectors from "./Connectors";
import { AllowedFolders, Appearance, Defaults } from "./Settings";
import { useText } from "./i18n";

const text = {
  en: {
    welcome: "Welcome to Patio", welcomeHelp: "A few quick choices and you're ready to work.",
    boardTitle: "Your first board", boardHelp: "Boards hold your tickets in columns. Pick a starting point; you can change columns later.",
    personal: "Personal", work: "Work", blank: "Blank", name: "Name", code: "Code", codeHelp: "Prefix for ticket codes, like ABC-1",
    connectorHelp: "Optional. Patio's AI chats run through a CLI you install and sign in to (Claude Code, Codex, Copilot, opencode) or a local model. You can add one later.",
    defaultsTitle: "Chat defaults", defaultsHelp: "How new chats start. Add a project folder to let chats work in it.",
    next: "Next", create: "Create board", skip: "Skip", skipAll: "Skip setup", finish: "Finish", step: (n: number, total: number) => `Step ${n} of ${total}`,
  },
  es: {
    welcome: "Bienvenido a Patio", welcomeHelp: "Unas elecciones rápidas y listo para trabajar.",
    boardTitle: "Tu primer tablero", boardHelp: "Los tableros guardan tus tickets en columnas. Elige un punto de partida; puedes cambiar las columnas después.",
    personal: "Personal", work: "Trabajo", blank: "En blanco", name: "Nombre", code: "Código", codeHelp: "Prefijo de los códigos de ticket, como ABC-1",
    connectorHelp: "Opcional. Los chats de IA de Patio funcionan con una CLI que instalas e inicias sesión (Claude Code, Codex, Copilot, opencode) o con un modelo local. Puedes agregarla después.",
    defaultsTitle: "Valores de chat", defaultsHelp: "Cómo empiezan los chats nuevos. Agrega una carpeta de proyecto para que los chats trabajen en ella.",
    next: "Siguiente", create: "Crear tablero", skip: "Omitir", skipAll: "Omitir configuración", finish: "Terminar", step: (n: number, total: number) => `Paso ${n} de ${total}`,
  },
};

type Preset = "personal" | "work" | "blank";
const done = { type: "temporary", hide_after_days: 1 };
const todo = { type: "classic", hide_after_days: 1 };
// burnedout: assumed preset columns (plan decision 4)
const presets: Record<Preset, { code: string; columns: { name: string; type: string; hide_after_days: number }[] }> = {
  personal: { code: "ME", columns: [{ name: "To Do", ...todo }, { name: "Doing", ...todo }, { name: "Done", ...done }] },
  work: {
    code: "WORK",
    columns: [{ name: "Backlog", ...todo }, { name: "To Do", ...todo }, { name: "In Progress", ...todo }, { name: "Review", ...todo }, { name: "Done", ...done }],
  },
  blank: { code: "BRD", columns: [{ name: "To Do", ...todo }, { name: "Done", ...done }] },
};

export default function Onboarding({ onDone }: { onDone: () => void }) {
  const t = useText(text);
  const dialog = useRef<HTMLDialogElement>(null);
  const [step, setStep] = useState(0);
  const [board, setBoard] = useState<Board>();
  const [foldersVersion, setFoldersVersion] = useState(0);
  const steps = ["welcome", "board", "connector", "defaults"] as const;

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
  }, []);

  async function finish() {
    await api.put("/api/onboarding", {}).catch(() => undefined);
    dialog.current?.close();
    if (board) navigate(`/boards/${board.id}`);
    onDone();
  }

  const next = () => (step + 1 < steps.length ? setStep(step + 1) : finish());

  async function afterConnector() {
    const connectors = await api.get<Connector[]>("/api/connectors").catch(() => []);
    if (connectors.length) next();
    else void finish();
  }

  return (
    <dialog ref={dialog} className="create-dialog" aria-labelledby="onboarding-title" onCancel={(event) => event.preventDefault()}>
      <section className="panel create create-modal stack">
        <div className="page-heading">
          <p className="eyebrow">{t.step(step + 1, steps.length)}</p>
          <button type="button" className="ghost" onClick={finish}>
            {t.skipAll}
          </button>
        </div>
        {steps[step] === "welcome" && (
          <>
            <div>
              <h1 id="onboarding-title">{t.welcome}</h1>
              <p className="muted">{t.welcomeHelp}</p>
            </div>
            <Appearance />
            <div className="modal-actions">
              <button className="primary" onClick={next}>
                {t.next}
              </button>
            </div>
          </>
        )}
        {steps[step] === "board" && (
          <FirstBoard
            onCreated={(created) => {
              setBoard(created);
              next();
            }}
            onSkip={next}
          />
        )}
        {steps[step] === "connector" && (
          <>
            <p className="muted" id="onboarding-title">{t.connectorHelp}</p>
            <Connectors refresh={0} />
            <div className="modal-actions">
              <button type="button" onClick={finish}>
                {t.skip}
              </button>
              <button className="primary" onClick={afterConnector}>
                {t.next}
              </button>
            </div>
          </>
        )}
        {steps[step] === "defaults" && (
          <>
            <div>
              <h1 id="onboarding-title">{t.defaultsTitle}</h1>
              <p className="muted">{t.defaultsHelp}</p>
            </div>
            <Defaults foldersVersion={foldersVersion} />
            <AllowedFolders onChange={() => setFoldersVersion((value) => value + 1)} />
            <div className="modal-actions">
              <button className="primary" onClick={finish}>
                {t.finish}
              </button>
            </div>
          </>
        )}
      </section>
    </dialog>
  );
}

function FirstBoard({ onCreated, onSkip }: { onCreated: (board: Board) => void; onSkip: () => void }) {
  const t = useText(text);
  const [preset, setPreset] = useState<Preset>("personal");
  const [name, setName] = useState(t.personal);
  const [code, setCode] = useState(presets.personal.code);
  const [error, setError] = useState<unknown>();

  function pick(next: Preset) {
    setPreset(next);
    setName(t[next]);
    setCode(presets[next].code);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      onCreated(
        await api.post<Board>("/api/boards", {
          name: name.trim(),
          short_name: code.trim().toUpperCase(),
          template: "",
          statuses: presets[preset].columns,
        }),
      );
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <form className="stack" onSubmit={submit}>
      <div>
        <h1 id="onboarding-title">{t.boardTitle}</h1>
        <p className="muted">{t.boardHelp}</p>
      </div>
      <fieldset className="palette-picker">
        {(Object.keys(presets) as Preset[]).map((id) => (
          <label key={id} className="palette-option">
            <input type="radio" name="preset" value={id} checked={preset === id} onChange={() => pick(id)} />
            {t[id]} <span className="muted">· {presets[id].columns.map((column) => column.name).join(" / ")}</span>
          </label>
        ))}
      </fieldset>
      <div className="defaults-fields">
        <label>
          {t.name}
          <input required value={name} onChange={(event) => setName(event.target.value)} />
        </label>
        <label>
          {t.code}
          <input required value={code} title={t.codeHelp} onChange={(event) => setCode(event.target.value)} />
        </label>
      </div>
      <ErrorText error={error} />
      <div className="modal-actions">
        <button type="button" onClick={onSkip}>
          {t.skip}
        </button>
        <button className="primary">{t.create}</button>
      </div>
    </form>
  );
}
