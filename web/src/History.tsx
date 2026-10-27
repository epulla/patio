import { useEffect, useState } from "react";
import AIBadge from "./AIBadge";
import { api } from "./api";
import { ErrorText, formatDate } from "./App";
import { confirmDialog } from "./confirm";
import { useText } from "./i18n";

export type HistoryItemType = "skill" | "connector" | "mcp_server" | "scheduled_task" | "bot";

type Change = { field: string; old?: unknown; new?: unknown; hidden?: boolean };

type Entry = {
  id: string;
  action: "created" | "updated" | "deleted" | "restored";
  changes: Change[];
  user_name: string;
  user_is_ai: boolean;
  created_at: string;
};

const text = {
  en: {
    history: "History", empty: "No changes recorded yet.", created: "created", updated: "updated", deleted: "deleted", restored: "restored",
    hidden: "changed (hidden)", none: "empty", showChange: "Show changes",
    restore: "Restore this version", confirmRestore: "Restore the description and instructions from this version?",
  },
  es: {
    history: "Historial", empty: "Aún no hay cambios registrados.", created: "creó", updated: "actualizó", deleted: "eliminó", restored: "restauró",
    hidden: "cambiado (oculto)", none: "vacío", showChange: "Ver cambios",
    restore: "Restaurar esta versión", confirmRestore: "¿Restaurar la descripción y las instrucciones de esta versión?",
  },
};

const show = (value: unknown) => (typeof value === "string" ? value : value === undefined || value === null ? "" : JSON.stringify(value));
const isLong = (value: unknown) => show(value).length > 80 || show(value).includes("\n");

type DiffLine = { kind: " " | "+" | "-" | "…"; text: string };

// Line diff (LCS) with unchanged runs folded to 2 lines of context.
function lineDiff(before: string, after: string): DiffLine[] {
  const a = before.split("\n"), b = after.split("\n");
  const lcs = Array.from({ length: a.length + 1 }, () => new Uint32Array(b.length + 1));
  for (let i = a.length - 1; i >= 0; i--)
    for (let j = b.length - 1; j >= 0; j--) lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
  const lines: DiffLine[] = [];
  let i = 0, j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) lines.push({ kind: " ", text: a[i++] }), j++;
    else if (j < b.length && (i === a.length || lcs[i][j + 1] > lcs[i + 1][j])) lines.push({ kind: "+", text: b[j++] });
    else lines.push({ kind: "-", text: a[i++] });
  }
  const changed = lines.map((line) => line.kind !== " ");
  const near = (k: number) => changed.slice(Math.max(0, k - 2), k + 3).some(Boolean);
  const folded: DiffLine[] = [];
  lines.forEach((line, k) => {
    if (near(k)) folded.push(line);
    else if (folded.at(-1)?.kind !== "…") folded.push({ kind: "…", text: "" });
  });
  return folded;
}

export default function History<T>({
  itemType,
  itemId,
  refresh = 0,
  onRestored,
}: {
  itemType: HistoryItemType;
  itemId: string;
  refresh?: number;
  onRestored?: (item: T) => void;
}) {
  const t = useText(text);
  const [entries, setEntries] = useState<Entry[]>();
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    api
      .get<Entry[]>(`/api/history?item_type=${itemType}&item_id=${encodeURIComponent(itemId)}`)
      .then((list) => {
        setEntries(list);
        setError(undefined);
      })
      .catch(setError);
  }, [itemType, itemId, refresh]);

  async function restore(entry: Entry) {
    if (!onRestored || !(await confirmDialog(t.confirmRestore))) return;
    try {
      onRestored(await api.post<T>(`/api/history/${entry.id}/restore`, {}));
      setEntries(await api.get<Entry[]>(`/api/history?item_type=${itemType}&item_id=${encodeURIComponent(itemId)}`));
    } catch (reason) {
      setError(reason);
    }
  }

  const value = (v: unknown) => (show(v) === "" ? <em className="muted">{t.none}</em> : <code>{show(v)}</code>);

  return (
    <details className="panel item-history">
      <summary>
        {t.history}
        {entries && entries.length > 0 && <span className="muted"> ({entries.length})</span>}
      </summary>
      <ErrorText error={error} />
      {entries?.length === 0 && <p className="muted">{t.empty}</p>}
      <ol className="item-history-list">
        {entries?.map((entry, index) => (
          <li key={entry.id}>
            <div className="item-history-head">
              <strong>{entry.user_name}</strong>
              <AIBadge isAI={entry.user_is_ai} /> {t[entry.action]}
              <time className="muted" dateTime={entry.created_at}>
                {formatDate(entry.created_at)}
              </time>
              {onRestored && index > 0 && entry.action !== "deleted" && (
                <button type="button" className="ghost item-history-restore" onClick={() => restore(entry)}>
                  {t.restore}
                </button>
              )}
            </div>
            {entry.action !== "deleted" && entry.changes.length > 0 && (
              <ul className="item-history-changes">
                {entry.changes.map((change) => (
                  <li key={change.field}>
                    <span className="item-history-field">{change.field}</span>{" "}
                    {change.hidden ? (
                      <span className="muted">{t.hidden}</span>
                    ) : isLong(change.old) || isLong(change.new) ? (
                      <details>
                        <summary>{t.showChange}</summary>
                        {entry.action === "created" ? (
                          <pre className="item-history-diff">{show(change.new)}</pre>
                        ) : (
                          <pre className="item-history-diff">
                            {lineDiff(show(change.old), show(change.new)).map((line, k) => (
                              <span key={k} className={`diff-line diff-${{ " ": "same", "+": "add", "-": "del", "…": "gap" }[line.kind]}`}>
                                {line.kind === "…" ? "…" : `${line.kind} ${line.text}`}
                                {"\n"}
                              </span>
                            ))}
                          </pre>
                        )}
                      </details>
                    ) : entry.action === "created" ? (
                      value(change.new)
                    ) : (
                      <>
                        {value(change.old)} → {value(change.new)}
                      </>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </li>
        ))}
      </ol>
    </details>
  );
}
