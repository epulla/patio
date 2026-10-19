import { useEffect, useState } from "react";
import { api, type Board, type SearchTicket } from "./api";
import { ErrorText, navigate } from "./App";
import { Icon } from "./icons";
import Menu from "./Menu";
import Picker from "./Picker";
import { locale, useText } from "./i18n";

const text = {
  en: {
    code: "Code", title: "Title", board: "Board", status: "Status", created: "Created", updated: "Updated",
    recent: "Most recent", alphabetical: "Alphabetical", workspace: "Workspace", tickets: "Tickets",
    description: "Tickets from every board, newest first. Narrow them down by text, board, or date.",
    filters: "Filters", search: "Search", titleOrCode: "Title or code", allBoards: "All boards",
    createdFrom: "Created from", createdTo: "Created to", updatedFrom: "Updated from", updatedTo: "Updated to",
    searching: "Searching…", ticket: (n: number) => `${n} ticket${n === 1 ? "" : "s"}`,
    showingFirst: (n: number) => ` · showing first ${n}`, ascending: "ascending", descending: "descending",
    noMatches: "No tickets match these filters.", boardLabel: "Board ", statusLabel: "Status ",
    sortTickets: "Sort tickets", sort: "Sort:",
  },
  es: {
    code: "Código", title: "Título", board: "Tablero", status: "Estado", created: "Creado", updated: "Actualizado",
    recent: "Más recientes", alphabetical: "Alfabético", workspace: "Espacio de trabajo", tickets: "Tickets",
    description: "Tickets de todos los tableros, primero los más recientes. Filtra por texto, tablero o fecha.",
    filters: "Filtros", search: "Buscar", titleOrCode: "Título o código", allBoards: "Todos los tableros",
    createdFrom: "Creado desde", createdTo: "Creado hasta", updatedFrom: "Actualizado desde", updatedTo: "Actualizado hasta",
    searching: "Buscando…", ticket: (n: number) => `${n} ticket${n === 1 ? "" : "s"}`,
    showingFirst: (n: number) => ` · mostrando los primeros ${n}`, ascending: "ascendente", descending: "descendente",
    noMatches: "Ningún ticket coincide con estos filtros.", boardLabel: "Tablero ", statusLabel: "Estado ",
    sortTickets: "Ordenar tickets", sort: "Orden:",
  },
};

type Column = "code" | "title" | "board" | "status" | "created" | "updated";
type Sort = { column: Column; dir: "asc" | "desc" };

const presets = [
  { label: "recent", sort: { column: "created", dir: "desc" } },
  { label: "alphabetical", sort: { column: "title", dir: "asc" } },
] satisfies { label: string; sort: Sort }[];
const limit = 200;
const columns: Column[] = ["code", "title", "board", "status", "created", "updated"];

function savedSort(): Sort {
  const [column, dir] = (localStorage.getItem("ticket-sort") ?? "").split(":");
  return columns.includes(column as Column) && (dir === "asc" || dir === "desc") ? { column: column as Column, dir } : presets[0].sort;
}

function dayStart(day: string, offset = 0) {
  const date = new Date(`${day}T00:00`);
  date.setDate(date.getDate() + offset);
  return date.toISOString();
}

export default function Tickets({ refresh }: { refresh: number }) {
  const t = useText(text);
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
      <p className="eyebrow">{t.workspace}</p>
          <h1>{t.tickets}</h1>
        </div>
      </div>
      <p className="muted">{t.description}</p>
      <section className="panel ticket-filters" aria-label={t.filters}>
        <label className="ticket-filter-search">
          {t.search}
          <input type="search" placeholder={t.titleOrCode} value={query} onChange={(event) => setQuery(event.target.value)} />
        </label>
        <label>
          {t.board}
          <Picker label={t.board} empty={t.allBoards} options={boards} value={boardID} onChange={setBoardID} />
        </label>
        {dateField("created_from", t.createdFrom)}
        {dateField("created_to", t.createdTo)}
        {dateField("updated_from", t.updatedFrom)}
        {dateField("updated_to", t.updatedTo)}
      </section>
      <ErrorText error={error} />
      <div className="list-toolbar">
        <span className="muted" aria-live="polite">
          {loading && !results ? t.searching : `${t.ticket(count)}${count === limit ? t.showingFirst(limit) : ""}`}
        </span>
        <SortMenu value={sort} onChange={changeSort} />
      </div>
      <div className="panel ticket-list">
        <div className="ticket-row ticket-list-head">
          {(Object.keys(columns) as Column[]).map((column) => {
            const active = column === sort.column;
            const labels = t;
            return (
              <button
                key={column}
                type="button"
                className={active ? `active ${sort.dir}` : undefined}
                aria-pressed={active}
                aria-label={`${t.sortTickets}: ${labels[column]}${active ? `, ${sort.dir === "asc" ? t.ascending : t.descending}` : ""}`}
                onClick={() => sortBy(column)}
              >
                {labels[column]} {active && <Icon name="chevronDown" />}
              </button>
            );
          })}
        </div>
        {results?.length === 0 && <p className="muted ticket-list-empty">{t.noMatches}</p>}
        {!!results?.length && (
          <ul aria-label={t.tickets}>
            {results.map((ticket) => (
              <li key={ticket.id}>
                <button className="ticket-row" onClick={() => navigate(`/tickets/${ticket.id}`)}>
                  <span className="ticket-row-code">{ticket.code}</span>
                  <span className="ticket-row-title">{ticket.title}</span>
                  <span className="muted"><span className="sr-only">{t.boardLabel}</span>{ticket.board_name}</span>
                  <span className="muted"><span className="sr-only">{t.statusLabel}</span>{ticket.status_name}</span>
                  <time className="muted" dateTime={ticket.created_at}>
                    <span className="sr-only date-label">{t.created} </span>{new Date(ticket.created_at).toLocaleDateString(locale())}
                  </time>
                  <time className="muted" dateTime={ticket.updated_at}>
                    <span className="sr-only date-label">{t.updated} </span>{new Date(ticket.updated_at).toLocaleDateString(locale())}
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
  const t = useText(text);
  const current = presets.find((preset) => preset.sort.column === value.column && preset.sort.dir === value.dir);
  return (
    <Menu
      label={t.sortTickets}
      button={
        <>
          {t.sort} {current ? (current.label === "recent" ? t.recent : t.alphabetical) : `${t[value.column]} ${value.dir === "asc" ? "↑" : "↓"}`} <Icon name="chevronDown" />
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
            {preset.label === "recent" ? t.recent : t.alphabetical}
          </button>
        ))
      }
    </Menu>
  );
}
