import { useEffect, useState } from "react";
import { api, type SearchTicket } from "./api";
import { ErrorText, navigate } from "./App";

export default function Tickets({ refresh }: { refresh: number }) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchTicket[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    const search = query.trim();
    let active = true;
    if (!search) {
      setResults([]);
      setLoading(false);
      setError(undefined);
      return () => { active = false; };
    }
    setLoading(true);
    setError(undefined);
    const timer = setTimeout(() => {
      api.get<SearchTicket[]>(`/api/tickets/search?${new URLSearchParams({ q: search })}`)
        .then((tickets) => { if (active) setResults(tickets); })
        .catch((reason) => { if (active) setError(reason); })
        .finally(() => { if (active) setLoading(false); });
    }, 250);
    return () => { active = false; clearTimeout(timer); };
  }, [query, refresh]);

  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Workspace</p>
          <h1>Tickets</h1>
        </div>
      </div>
      <label>
        Search tickets
        <input type="search" value={query} onChange={(event) => setQuery(event.target.value)} />
      </label>
      <ErrorText error={error} />
      <p aria-live="polite">
        {!query.trim() ? "Enter a search term to find tickets across all boards." : loading ? "Searching…" : `${results.length} results`}
      </p>
      <div className="cards">
        {results.map((ticket) => (
          <button className="ticket-card ticket-search-result" key={ticket.id} onClick={() => navigate(`/tickets/${ticket.id}`)}>
            <strong><span className="ticket-code">{ticket.code}</span> {ticket.title}</strong>
            <span className="ticket-meta">{ticket.board_name} · {ticket.status_name}</span>
          </button>
        ))}
      </div>
    </div>
  );
}
