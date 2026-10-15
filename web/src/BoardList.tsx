import {
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
} from "react";
import { api, type Board } from "./api";
import { ErrorText, navigate } from "./App";
import { DaysField } from "./BoardView";
import { Icon, IconButton } from "./icons";
import Picker from "./Picker";

type NewColumn = {
  name: string;
  type: "classic" | "temporary";
  hide_after_days: string;
};

export default function BoardList({ refresh }: { refresh: number }) {
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
          <p className="eyebrow">Workspace</p>
          <h1>Boards</h1>
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
            data-tip="Import a JSON backup"
            disabled={importing}
            onClick={() => fileInput.current?.click()}
          >
            <Icon name="upload" /> {importing ? "Importing..." : "Import"}
          </button>
          <button
            ref={createButton}
            className="primary"
            onClick={() => setCreating(true)}
          >
            <Icon name="plus" /> Create board
          </button>
        </div>
      </div>
      <ErrorText error={error} />
      {boards.length === 0 && <small>No boards yet. Create one to get started.</small>}
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
  const [name, setName] = useState("");
  const [shortName, setShortName] = useState("");
  const [template, setTemplate] = useState("");
  const [columns, setColumns] = useState<NewColumn[]>([
    { name: "To Do", type: "classic", hide_after_days: "1" },
    { name: "In Progress", type: "classic", hide_after_days: "1" },
    { name: "Done", type: "temporary", hide_after_days: "1" },
  ]);
  const [error, setError] = useState<unknown>();

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
      onClose={onClose}
    >
      <section className="panel create create-modal">
        <div className="page-heading">
          <h2 id="create-board-title">Create board</h2>
          <IconButton
            type="button"
            className="ghost"
            icon="x"
            label="Close"
            onClick={() => dialog.current?.close()}
          />
        </div>
        <form onSubmit={submit}>
          <label>
            Name
            <input
              ref={nameInput}
              required
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <label>
            Short name
            <input
              required
              maxLength={10}
              value={shortName}
              onChange={(event) => setShortName(event.target.value)}
            />
          </label>
          <fieldset className="create-columns">
            <legend>Columns</legend>
            <p className="callout">
              Temporary columns hide their tickets from the board N days after
              they enter the column. Tickets are not deleted. Good for Done.
            </p>
            {columns.map((column, index) => (
              <div className="type-row" key={index}>
                <input
                  aria-label={`Column ${index + 1} name`}
                  value={column.name}
                  onChange={(event) => updateColumn(index, { name: event.target.value })}
                />
                <Picker
                  label={`Column ${index + 1} type`}
                  options={[
                    { id: "classic", name: "Classic" },
                    { id: "temporary", name: "Temporary" },
                  ]}
                  value={column.type}
                  onChange={(type) => updateColumn(index, { type: type as NewColumn["type"] })}
                />
                <DaysField
                  label={`Column ${index + 1} hide after days`}
                  value={column.hide_after_days}
                  onChange={(value) => updateColumn(index, { hide_after_days: value })}
                  hidden={column.type !== "temporary"}
                />
                <IconButton
                  type="button"
                  className="danger-text"
                  icon="trash"
                  label={`Remove column ${index + 1}`}
                  disabled={columns.length === 1}
                  onClick={() => setColumns((current) => current.filter((_, i) => i !== index))}
                />
              </div>
            ))}
            <button
              type="button"
              onClick={() => setColumns((current) => [...current, { name: "", type: "classic", hide_after_days: "1" }])}
            >
              <Icon name="plus" /> Add column
            </button>
          </fieldset>
          <label>
            Template HTML
            <input
              value={template}
              onChange={(event) => setTemplate(event.target.value)}
            />
          </label>
          <div className="modal-actions">
            <button type="button" onClick={() => dialog.current?.close()}>
              Cancel
            </button>
            <button className="primary">
              <Icon name="plus" /> Create board
            </button>
          </div>
        </form>
        <ErrorText error={error} />
      </section>
    </dialog>
  );
}
