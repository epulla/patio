import { useEffect, useState, type FormEvent } from "react";
import { api, type Board } from "./api";
import { ErrorText, navigate } from "./App";

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
            <span className="arrow">→</span>
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
  const [columns, setColumns] = useState("To Do, In Progress, Done");
  const [error, setError] = useState<unknown>();

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const statuses = columns
        .split(",")
        .map((value) => value.trim())
        .filter(Boolean);
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
        <label>
          Column names
          <input
            value={columns}
            onChange={(event) => setColumns(event.target.value)}
          />
        </label>
        <label>
          Template HTML
          <input
            value={template}
            onChange={(event) => setTemplate(event.target.value)}
          />
        </label>
        <button className="primary">Create board</button>
      </form>
      <ErrorText error={error} />
    </section>
  );
}
