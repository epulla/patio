import {
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
} from "react";
import { api, type Board } from "./api";
import { AskAI, CreateTabs, useAiCreate, useCreated } from "./AiCreate";
import { ErrorText, navigate } from "./App";
import { DaysField } from "./BoardView";
import { Icon, IconButton } from "./icons";
import Picker from "./Picker";
import { lang, useText } from "./i18n";

const text = {
  en: {
    workspace: "Workspace", boards: "Boards", importBackup: "Import a JSON backup", importing: "Importing...",
    import: "Import", createBoard: "Create board", newBoard: "New board", boardLabel: (name: string) => `board ${name}`, empty: "No boards yet. Create one to get started.",
    close: "Close", name: "Name", shortName: "Short name", columns: "Columns",
    temporaryHelp: "Temporary columns hide their tickets from the board N days after they enter the column. Tickets are not deleted. Good for Done.",
    columnName: (n: number) => `Column ${n} name`, columnType: (n: number) => `Column ${n} type`,
    classic: "Classic", temporary: "Temporary", hideAfter: (n: number) => `Column ${n} hide after days`,
    removeColumn: (n: number) => `Remove column ${n}`, addColumn: "Add column", template: "Template HTML", cancel: "Cancel",
  },
  es: {
    workspace: "Espacio de trabajo", boards: "Tableros", importBackup: "Importar respaldo JSON", importing: "Importando...",
    import: "Importar", createBoard: "Crear tablero", newBoard: "Nuevo tablero", boardLabel: (name: string) => `el tablero ${name}`, empty: "Aún no hay tableros. Crea uno para comenzar.",
    close: "Cerrar", name: "Nombre", shortName: "Nombre corto", columns: "Columnas",
    temporaryHelp: "Las columnas temporales ocultan sus tickets del tablero N días después de que entran. Los tickets no se eliminan. Útil para Completado.",
    columnName: (n: number) => `Nombre de columna ${n}`, columnType: (n: number) => `Tipo de columna ${n}`,
    classic: "Clásica", temporary: "Temporal", hideAfter: (n: number) => `Ocultar columna ${n} después de (días)`,
    removeColumn: (n: number) => `Quitar columna ${n}`, addColumn: "Agregar columna", template: "HTML de plantilla", cancel: "Cancelar",
  },
};

type NewColumn = {
  name: string;
  type: "classic" | "temporary";
  hide_after_days: string;
};

const boardDraft = {
  en: "Create a board called <name> for <what it's for>.\nColumns: <e.g. To Do, Doing, Done>.",
  es: "Crea un tablero llamado <nombre> para <para qué es>.\nColumnas: <p. ej. Por hacer, En curso, Hecho>.",
};

export default function BoardList({ refresh }: { refresh: number }) {
  const t = useText(text);
  const [boards, setBoards] = useState<Board[]>([]);
  const [error, setError] = useState<unknown>();
  const [importing, setImporting] = useState(false);
  const [creating, setCreating] = useState(false);
  const createButton = useRef<HTMLButtonElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);

  const load = () =>
    api.get<Board[]>("/api/boards").then(setBoards).catch(setError);
  useEffect(() => {
    load();
  }, [refresh]);

  async function importFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) return;
    setImporting(true);
    try {
      const response = await fetch("/api/import", { method: "POST", headers: { "Content-Type": "application/json" }, body: await file.text() });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || `Request failed (${response.status})`);
      navigate(`/boards/${result.id}`);
    } catch (reason) { setError(reason); } finally { setImporting(false); event.target.value = ""; }
  }

  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <p className="eyebrow">{t.workspace}</p>
          <h1>{t.boards}</h1>
        </div>
        <div className="actions">
          <input
            ref={fileInput}
            type="file"
            accept="application/json,.json"
            hidden
            onChange={importFile}
          />
          <button
            data-tip={t.importBackup}
            disabled={importing}
            onClick={() => fileInput.current?.click()}
          >
            <Icon name="upload" /> {importing ? t.importing : t.import}
          </button>
          <button
            ref={createButton}
            className="primary"
            onClick={() => setCreating(true)}
          >
            <Icon name="plus" /> {t.newBoard}
          </button>
        </div>
      </div>
      <ErrorText error={error} />
      {boards.length === 0 && <small>{t.empty}</small>}
      <div className="board-grid">
        {boards.map((board) => (
          <button
            className="board-card"
            key={board.id}
            onClick={() => navigate(`/boards/${board.id}`)}
          >
            <span className="board-mark">{board.short_name.slice(0, 2)}</span>
            <span>
              <strong>{board.name}</strong>
              <small>{board.short_name}</small>
            </span>
            <span className="arrow">
              <Icon name="chevronRight" />
            </span>
          </button>
        ))}
      </div>
      {creating && (
        <CreateBoard
          onCreated={(board) => navigate(`/boards/${board.id}`)}
          onClose={() => {
            setCreating(false);
            createButton.current?.focus();
          }}
        />
      )}
    </div>
  );
}

function CreateBoard({
  onCreated,
  onClose,
}: {
  onCreated: (board: Board) => void;
  onClose: () => void;
}) {
  const t = useText(text);
  const [name, setName] = useState("");
  const [shortName, setShortName] = useState("");
  const [template, setTemplate] = useState("");
  const [columns, setColumns] = useState<NewColumn[]>([
    { name: "To Do", type: "classic", hide_after_days: "1" },
    { name: "In Progress", type: "classic", hide_after_days: "1" },
    { name: "Done", type: "temporary", hide_after_days: "1" },
  ]);
  const [error, setError] = useState<unknown>();
  const ai = useAiCreate();
  const created = useCreated(ai, async (since) =>
    (await api.get<Board[]>("/api/boards"))
      .filter((board) => board.created_at > since)
      .map((board) => ({ id: board.id, label: t.boardLabel(board.name), href: `/boards/${board.id}` })),
  );

  function updateColumn(index: number, patch: Partial<NewColumn>) {
    setColumns((current) => current.map((column, i) => i === index ? { ...column, ...patch } : column));
  }

  const dialog = useRef<HTMLDialogElement>(null);
  const nameInput = useRef<HTMLInputElement>(null);
  // Native modal dialog: inert background, focus trap, Esc closes via onClose.
  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
    nameInput.current?.focus();
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const statuses = columns
        .map((column) => ({ ...column, name: column.name.trim(), hide_after_days: Number(column.hide_after_days) }))
        .filter((column) => column.name);
      const board = await api.post<Board>("/api/boards", {
        name: name.trim(),
        short_name: shortName.trim().toUpperCase(),
        template,
        statuses,
      });
      onCreated(board);
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <dialog
      ref={dialog}
      className="create-dialog"
      aria-labelledby="create-board-title"
      onClose={() => {
        void ai.cleanup();
        onClose();
      }}
      onClick={(event) => event.target === dialog.current && dialog.current.close()}
    >
      <section className="panel create create-modal">
        <div className="page-heading">
          <h2 id="create-board-title">{t.newBoard}</h2>
          <IconButton
            type="button"
            className="ghost"
            icon="x"
            label={t.close}
            onClick={() => dialog.current?.close()}
          />
        </div>
        <CreateTabs
          kind="board"
          ai={ai}
          form={
        <>
        <form onSubmit={submit}>
          <label>
            {t.name}
            <input
              ref={nameInput}
              required
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <label>
            {t.shortName}
            <input
              required
              maxLength={10}
              value={shortName}
              onChange={(event) => setShortName(event.target.value)}
            />
          </label>
          <fieldset className="create-columns">
            <legend>{t.columns}</legend>
            <p className="callout">
              {t.temporaryHelp}
            </p>
            {columns.map((column, index) => (
              <div className="type-row" key={index}>
                <input
                  aria-label={t.columnName(index + 1)}
                  value={column.name}
                  onChange={(event) => updateColumn(index, { name: event.target.value })}
                />
                <Picker
                  label={t.columnType(index + 1)}
                  options={[
                    { id: "classic", name: t.classic },
                    { id: "temporary", name: t.temporary },
                  ]}
                  value={column.type}
                  onChange={(type) => updateColumn(index, { type: type as NewColumn["type"] })}
                />
                <DaysField
                  label={t.hideAfter(index + 1)}
                  value={column.hide_after_days}
                  onChange={(value) => updateColumn(index, { hide_after_days: value })}
                  hidden={column.type !== "temporary"}
                />
                <IconButton
                  type="button"
                  className="danger-text"
                  icon="trash"
                  label={t.removeColumn(index + 1)}
                  disabled={columns.length === 1}
                  onClick={() => setColumns((current) => current.filter((_, i) => i !== index))}
                />
              </div>
            ))}
            <button
              type="button"
              onClick={() => setColumns((current) => [...current, { name: "", type: "classic", hide_after_days: "1" }])}
            >
              <Icon name="plus" /> {t.addColumn}
            </button>
          </fieldset>
          <label>
            {t.template}
            <input
              value={template}
              onChange={(event) => setTemplate(event.target.value)}
            />
          </label>
          <div className="modal-actions">
            <button type="button" onClick={() => dialog.current?.close()}>
              {t.cancel}
            </button>
            <button className="primary">
              <Icon name="plus" /> {t.createBoard}
            </button>
          </div>
        </form>
        <ErrorText error={error} />
        </>
          }
        >
          {(back) => (
            <AskAI
              ai={ai}
              title={t.newBoard}
              draft={boardDraft[lang()]}
              created={created}
              onBack={back}
              onOpen={(href) => {
                dialog.current?.close();
                navigate(href);
              }}
              onDone={() => dialog.current?.close()}
            />
          )}
        </CreateTabs>
      </section>
    </dialog>
  );
}
