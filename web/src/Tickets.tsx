import { useEffect, useState } from "react";
import { api, type Board, type SearchTicket } from "./api";
import { ErrorText, navigate } from "./App";
import { Icon } from "./icons";
import Menu from "./Menu";
import Picker from "./Picker";

type Column = "code" | "title" | "board" | "status" | "created" | "updated";
type Sort = { column: Column; dir: "asc" | "desc" };

const columns: Record<Column, string> = {
  code: "Code",
  title: "Title",
  board: "Board",
  status: "Status",
  created: "Created",
  updated: "Updated",
};
const presets = [
  { label: "Most recent", sort: { column: "created", dir: "desc" } },
  { label: "Alphabetical", sort: { column: "title", dir: "asc" } },
] satisfies { label: string; sort: Sort }[];
const limit = 200;

function savedSort(): Sort {
  const [column, dir] = (localStorage.getItem("ticket-sort") ?? "").split(":");
  return column in columns && (dir === "asc" || dir === "desc") ? { column: column as Column, dir } : presets[0].sort;
}

function dayStart(day: string, offset = 0) {
  const date = new Date(`${day}T00:00`);
  date.setDate(date.getDate() + offset);
  return date.toISOString();
}

export default function Tickets({ refresh }: { refresh: number }) {
  const [query, setQuery] = useState("");
  const [boardID, setBoardID] = useState("");
  const [dates, setDates] = useState({ created_from: "", created_to: "", updated_from: "", updated_to: "" });
  const [sort, setSort] = useState(savedSort);
  const [boards, setBoards] = useState<Board[]>([]);
  const [results, setResults] = useState<SearchTicket[]>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    api.get<Board[]>("/api/boards").then(setBoards).catch(setError);
  }, [refresh]);

  useEffect(() => {
    const params = new URLSearchParams({ sort: sort.column, dir: sort.dir });
    if (query.trim()) params.set("q", query.trim());
    if (boardID) params.set("board_id", boardID);
    for (const [key, day] of Object.entries(dates)) {
      if (day) params.set(key, dayStart(day, key.endsWith("_to") ? 1 : 0));
    }
    let active = true;
    setLoading(true);
    setError(undefined);
    const timer = setTimeout(() => {
      api.get<SearchTicket[]>(`/api/tickets/search?${params}`)
        .then((tickets) => { if (active) setResults(tickets); })
        .catch((reason) => { if (active) setError(reason); })
        .finally(() => { if (active) setLoading(false); });
    }, 250);
    return () => { active = false; clearTimeout(timer); };
  }, [query, boardID, dates, sort, refresh]);

  function changeSort(next: Sort) {
    localStorage.setItem("ticket-sort", `${next.column}:${next.dir}`);
    setSort(next);
  }

  function sortBy(column: Column) {
    if (column === sort.column) return changeSort({ column, dir: sort.dir === "asc" ? "desc" : "asc" });
    changeSort({ column, dir: column === "created" || column === "updated" ? "desc" : "asc" });
  }

  const dateField = (key: keyof typeof dates, label: string) => (
    <label>
      {label}
      <input type="date" value={dates[key]} onChange={(event) => setDates({ ...dates, [key]: event.target.value })} />
    </label>
  );
  const count = results?.length ?? 0;

  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Workspace</p>
          <h1>Tickets</h1>
        </div>
      </div>
      <p className="muted">Tickets from every board, newest first. Narrow them down by text, board, or date.</p>
      <section className="panel ticket-filters" aria-label="Filters">
        <label className="ticket-filter-search">
          Search
          <input type="search" placeholder="Title or code" value={query} onChange={(event) => setQuery(event.target.value)} />
        </label>
        <label>
          Board
          <Picker label="Board" empty="All boards" options={boards} value={boardID} onChange={setBoardID} />
        </label>
        {dateField("created_from", "Created from")}
        {dateField("created_to", "Created to")}
        {dateField("updated_from", "Updated from")}
        {dateField("updated_to", "Updated to")}
      </section>
      <ErrorText error={error} />
      <div className="list-toolbar">
        <span className="muted" aria-live="polite">
          {loading && !results ? "Searching…" : `${count} ticket${count === 1 ? "" : "s"}${count === limit ? ` · showing first ${limit}` : ""}`}
        </span>
        <SortMenu value={sort} onChange={changeSort} />
      </div>
      <div className="panel ticket-list">
        <div className="ticket-row ticket-list-head">
          {(Object.keys(columns) as Column[]).map((column) => {
            const active = column === sort.column;
            return (
              <button
                key={column}
                type="button"
                className={active ? `active ${sort.dir}` : undefined}
                aria-pressed={active}
                aria-label={`Sort by ${columns[column]}${active ? `, ${sort.dir === "asc" ? "ascending" : "descending"}` : ""}`}
                onClick={() => sortBy(column)}
              >
                {columns[column]} {active && <Icon name="chevronDown" />}
              </button>
            );
          })}
        </div>
        {results?.length === 0 && <p className="muted ticket-list-empty">No tickets match these filters.</p>}
        {!!results?.length && (
          <ul aria-label="Tickets">
            {results.map((ticket) => (
              <li key={ticket.id}>
                <button className="ticket-row" onClick={() => navigate(`/tickets/${ticket.id}`)}>
                  <span className="ticket-row-code">{ticket.code}</span>
                  <span className="ticket-row-title">{ticket.title}</span>
                  <span className="muted"><span className="sr-only">Board </span>{ticket.board_name}</span>
                  <span className="muted"><span className="sr-only">Status </span>{ticket.status_name}</span>
                  <time className="muted" dateTime={ticket.created_at}>
                    <span className="sr-only date-label">Created </span>{new Date(ticket.created_at).toLocaleDateString()}
                  </time>
                  <time className="muted" dateTime={ticket.updated_at}>
                    <span className="sr-only date-label">Updated </span>{new Date(ticket.updated_at).toLocaleDateString()}
                  </time>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function SortMenu({ value, onChange }: { value: Sort; onChange: (value: Sort) => void }) {
  const current = presets.find((preset) => preset.sort.column === value.column && preset.sort.dir === value.dir);
  return (
    <Menu
      label="Sort tickets"
      button={
        <>
          Sort: {current?.label ?? `${columns[value.column]} ${value.dir === "asc" ? "↑" : "↓"}`} <Icon name="chevronDown" />
        </>
      }
    >
      {(close) =>
        presets.map((preset) => (
          <button
            key={preset.label}
            role="menuitemradio"
            aria-checked={preset === current}
            onClick={() => {
              onChange(preset.sort);
              close();
            }}
          >
            <span className="menu-check">{preset === current && <Icon name="check" />}</span>
            {preset.label}
          </button>
        ))
      }
    </Menu>
  );
}
