import { useEffect, useState, type FormEvent } from "react";
import { api, type Board } from "./api";
import { ErrorText, navigate } from "./App";
import { Icon, IconButton } from "./icons";

type NewColumn = {
  name: string;
  type: "classic" | "temporary";
  hide_after_days: string;
};

export default function BoardList({ refresh }: { refresh: number }) {
  const [boards, setBoards] = useState<Board[]>([]);
  const [error, setError] = useState<unknown>();
  const [importing, setImporting] = useState(false);

  const load = () =>
    api.get<Board[]>("/api/boards").then(setBoards).catch(setError);
  useEffect(() => {
    load();
  }, [refresh]);

  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Workspace</p>
          <h1>Boards</h1>
        </div>
      </div>
      <ErrorText error={error} />
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
      <CreateBoard onCreated={(board) => navigate(`/boards/${board.id}`)} />
      <section className="panel import">
        <h2>Import board</h2>
        <label>
          JSON backup
          <input type="file" accept="application/json,.json" disabled={importing} onChange={async (event) => {
            const file = event.target.files?.[0];
            if (!file) return;
            setImporting(true);
            try {
              const response = await fetch("/api/import", { method: "POST", headers: { "Content-Type": "application/json" }, body: await file.text() });
              const result = await response.json();
              if (!response.ok) throw new Error(result.error || `Request failed (${response.status})`);
              navigate(`/boards/${result.id}`);
            } catch (reason) { setError(reason); } finally { setImporting(false); event.target.value = ""; }
          }} aria-describedby="import-hint" />
        </label>
        <small id="import-hint">Full board backup — import into my-tickets on another computer.</small>
      </section>
    </div>
  );
}

function CreateBoard({ onCreated }: { onCreated: (board: Board) => void }) {
  const [name, setName] = useState("");
  const [shortName, setShortName] = useState("");
  const [template, setTemplate] = useState("");
  const [columns, setColumns] = useState<NewColumn[]>([
    { name: "To Do", type: "classic", hide_after_days: "1" },
    { name: "In Progress", type: "classic", hide_after_days: "1" },
    { name: "Done", type: "classic", hide_after_days: "1" },
  ]);
  const [error, setError] = useState<unknown>();

  function updateColumn(index: number, patch: Partial<NewColumn>) {
    setColumns((current) => current.map((column, i) => i === index ? { ...column, ...patch } : column));
  }

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
    <section className="panel create">
      <div>
        <p className="eyebrow">Start something new</p>
        <h2>Create board</h2>
      </div>
      <form onSubmit={submit}>
        <label>
          Name
          <input
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
          <small>
            Temporary: tickets auto-hide from the board N days after entering the column. They are not deleted.
          </small>
          {columns.map((column, index) => (
            <div className="type-row" key={index}>
              <input
                aria-label={`Column ${index + 1} name`}
                value={column.name}
                onChange={(event) => updateColumn(index, { name: event.target.value })}
              />
              <select
                aria-label={`Column ${index + 1} type`}
                value={column.type}
                onChange={(event) => updateColumn(index, { type: event.target.value as NewColumn["type"] })}
              >
                <option value="classic">Classic</option>
                <option value="temporary">Temporary</option>
              </select>
              {column.type === "temporary" && (
                <>
                  <input
                    type="number"
                    min={1}
                    step={1}
                    required
                    title="Hide tickets after N days"
                    aria-label={`Column ${index + 1} hide after days`}
                    value={column.hide_after_days}
                    onChange={(event) => updateColumn(index, { hide_after_days: event.target.value })}
                    style={{ width: "5rem" }}
                  />
                  <small>days</small>
                </>
              )}
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
        <button className="primary">
          <Icon name="plus" /> Create board
        </button>
      </form>
      <ErrorText error={error} />
    </section>
  );
}
