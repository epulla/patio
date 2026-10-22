import { useEffect, useState, type FormEvent } from "react";
import { api, type Board, type McpServer, type McpServerCheck, type Monitor, type MonitorRun } from "./api";
import { ErrorText, navigate } from "./App";
import { confirmDialog } from "./confirm";
import { Icon, IconButton } from "./icons";
import Menu from "./Menu";
import Switch from "./Switch";
import Picker from "./Picker";
import { locale, useText } from "./i18n";

type Condition = Monitor["condition"];
type Actions = { notify: boolean; ticket: { board_id: string; title: string } | null; chat: { prompt: string } | null };
type Sort = "recent" | "name";

const text = {
  en: {
    eyebrow: "Automation", title: "Monitors", new: "New monitor", back: "Monitors",
    description: "A monitor calls a tool from one of your MCP servers on a schedule and acts when the result changes. No AI runs until something changes. Monitors run only while Patio is open.",
    empty: "No monitors yet", example: "For example, search Slack for a keyword every 5 minutes and get notified about new messages.",
    noServers: "Add an MCP server first, then pick one of its tools to watch.", addServer: "Add MCP server",
    count: (n: number) => `${n} monitor${n === 1 ? "" : "s"}`, enable: (name: string) => `Enable ${name}`, more: (name: string) => `More actions for ${name}`,
    disable: "Disable monitor", enableMonitor: "Enable monitor", runNow: "Run now", edit: "Edit", remove: "Remove",
    recent: "Most recent", alphabetical: "Alphabetical", sortLabel: "Sort monitors", sort: "Sort:",
    ok: "OK", changed: "Changed", failed: "Failed", paused: "Paused", never: "Not run yet", lastRun: "Last run:",
    every: (n: number) => (n === 1 ? "Every minute" : `Every ${n} minutes`), custom: (cron: string) => `Custom: ${cron}`,
    gone: "This monitor no longer exists.", editAria: (name: string) => `Edit ${name}`,
    name: "Name", server: "MCP server", tool: "Tool", loadingTools: "Loading tools", args: "Arguments (JSON)",
    schedule: "Schedule", repeat: "Repeat", everyMinutes: "Every (minutes)", cron: "Advanced (cron)", interval: "Every few minutes",
    cronLabel: "Cron expression (minute hour day month weekday)",
    condition: "Notify when", conditionChanged: "The result changes", conditionNonEmpty: "The result is not empty", conditionNewItems: "New items appear in a list",
    resultPath: "List path (empty = whole result)", idPath: "Item ID path", pathHelp: "Dot paths into the JSON result, e.g. messages and ts. Use Test to see the result.",
    actions: "Actions", notify: "Send a notification", ticket: "Create a ticket", board: "Board", ticketTitle: "Ticket title",
    chat: "Start a chat with the default connector", prompt: "Prompt", placeholders: "Placeholders: {{name}}, {{count}}, {{items}}.",
    test: "Test", testing: "Testing…", testResult: "Test result", enabled: "Enabled", save: "Save", saved: "Saved", create: "Create monitor",
    deleteTitle: (name: string) => `Delete ${name}`, confirmDelete: (name: string) => `Delete monitor "${name}"?`,
    runs: "Recent runs", noRuns: "No runs yet.", newItems: (n: number) => `${n} new`,
    badJSON: "Arguments must be a JSON object.",
  },
  es: {
    eyebrow: "Automatización", title: "Monitores", new: "Nuevo monitor", back: "Monitores",
    description: "Un monitor llama a una herramienta de uno de tus servidores MCP según una programación y actúa cuando el resultado cambia. La IA no se ejecuta hasta que algo cambie. Los monitores funcionan solo mientras Patio esté abierto.",
    empty: "Aún no hay monitores", example: "Por ejemplo, buscar una palabra en Slack cada 5 minutos y recibir una notificación por cada mensaje nuevo.",
    noServers: "Primero agrega un servidor MCP y luego elige una de sus herramientas para vigilar.", addServer: "Agregar servidor MCP",
    count: (n: number) => `${n} monitor${n === 1 ? "" : "es"}`, enable: (name: string) => `Activar ${name}`, more: (name: string) => `Más acciones para ${name}`,
    disable: "Desactivar monitor", enableMonitor: "Activar monitor", runNow: "Ejecutar ahora", edit: "Editar", remove: "Eliminar",
    recent: "Más recientes", alphabetical: "Alfabético", sortLabel: "Ordenar monitores", sort: "Orden:",
    ok: "Sin cambios", changed: "Con cambios", failed: "Fallido", paused: "En pausa", never: "Aún sin ejecutar", lastRun: "Última ejecución:",
    every: (n: number) => (n === 1 ? "Cada minuto" : `Cada ${n} minutos`), custom: (cron: string) => `Personalizado: ${cron}`,
    gone: "Este monitor ya no existe.", editAria: (name: string) => `Editar ${name}`,
    name: "Nombre", server: "Servidor MCP", tool: "Herramienta", loadingTools: "Cargando herramientas", args: "Argumentos (JSON)",
    schedule: "Programación", repeat: "Repetir", everyMinutes: "Cada (minutos)", cron: "Avanzado (cron)", interval: "Cada pocos minutos",
    cronLabel: "Expresión cron (minuto hora día mes día-semana)",
    condition: "Avisar cuando", conditionChanged: "El resultado cambia", conditionNonEmpty: "El resultado no está vacío", conditionNewItems: "Aparecen elementos nuevos en una lista",
    resultPath: "Ruta de la lista (vacía = todo el resultado)", idPath: "Ruta del ID del elemento", pathHelp: "Rutas con puntos dentro del resultado JSON, p. ej. messages y ts. Usa Probar para ver el resultado.",
    actions: "Acciones", notify: "Enviar una notificación", ticket: "Crear un ticket", board: "Tablero", ticketTitle: "Título del ticket",
    chat: "Iniciar un chat con el conector predeterminado", prompt: "Instrucción", placeholders: "Marcadores: {{name}}, {{count}}, {{items}}.",
    test: "Probar", testing: "Probando…", testResult: "Resultado de la prueba", enabled: "Activado", save: "Guardar", saved: "Guardado", create: "Crear monitor",
    deleteTitle: (name: string) => `Eliminar ${name}`, confirmDelete: (name: string) => `¿Eliminar el monitor "${name}"?`,
    runs: "Ejecuciones recientes", noRuns: "Aún no hay ejecuciones.", newItems: (n: number) => `${n} nuevos`,
    badJSON: "Los argumentos deben ser un objeto JSON.",
  },
};

const formatTime = (iso: string) =>
  new Date(iso).toLocaleString(locale(), { weekday: "short", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });

const minutesOf = (schedule: string) => Number(schedule.match(/^@every (\d+)m$/)?.[1]) || 0;

function describe(t: typeof text.en, schedule: string) {
  const minutes = minutesOf(schedule);
  return minutes ? t.every(minutes) : t.custom(schedule);
}

function status(t: typeof text.en, monitor: Monitor) {
  if (!monitor.enabled) return { key: "paused", label: t.paused };
  if (!monitor.last_status) return { key: "never", label: t.never };
  return { key: monitor.last_status, label: t[monitor.last_status] };
}

function parseActions(raw: string): Actions {
  try {
    return { notify: false, ticket: null, chat: null, ...JSON.parse(raw) };
  } catch {
    return { notify: true, ticket: null, chat: null };
  }
}

export default function Monitors({ id, refresh }: { id?: string; refresh: number }) {
  const t = useText(text);
  const [monitors, setMonitors] = useState<Monitor[]>();
  const [servers, setServers] = useState<McpServer[]>();
  const [sort, setSort] = useState<Sort>(() => (localStorage.getItem("monitor-sort") === "name" ? "name" : "recent"));
  const [error, setError] = useState<unknown>();
  const [reload, setReload] = useState(0);

  useEffect(() => {
    Promise.all([api.get<Monitor[]>("/api/monitors"), api.get<McpServer[]>("/api/mcp-servers")])
      .then(([list, serverList]) => {
        setMonitors(list);
        setServers(serverList);
        setError(undefined);
      })
      .catch(setError);
  }, [refresh, reload]);

  const changed = () => setReload((value) => value + 1);

  async function act(run: () => Promise<unknown>) {
    try {
      await run();
      changed();
    } catch (reason) {
      setError(reason);
    }
  }

  const toggle = (monitor: Monitor) =>
    act(() =>
      api.put(`/api/monitors/${monitor.id}`, {
        name: monitor.name,
        mcp_server_id: monitor.mcp_server_id,
        tool: monitor.tool,
        args: JSON.parse(monitor.args),
        schedule: monitor.schedule,
        condition: monitor.condition,
        result_path: monitor.result_path,
        id_path: monitor.id_path,
        actions: JSON.parse(monitor.actions),
        enabled: !monitor.enabled,
      }),
    );
  const runNow = (monitor: Monitor) => act(() => api.post(`/api/monitors/${monitor.id}/run`, {}));
  const remove = async (monitor: Monitor) => {
    if (await confirmDialog(t.confirmDelete(monitor.name))) act(() => api.delete(`/api/monitors/${monitor.id}`));
  };

  if (id) {
    const monitor = monitors?.find((m) => m.id === id);
    if (id !== "new" && !monitor) {
      return monitors ? (
        <div className="stack">
          <BackLink />
          <p className="muted">{t.gone}</p>
        </div>
      ) : null;
    }
    return (
      <MonitorEditor
        key={id}
        monitor={monitor}
        servers={servers ?? []}
        refresh={refresh + reload}
        onSaved={() => {
          changed();
          if (!monitor) navigate("/monitors");
        }}
        onRunNow={() => monitor && runNow(monitor)}
        onDeleted={() => navigate("/monitors")}
      />
    );
  }

  const noServers = servers?.length === 0;
  const newButton = !noServers && (
    <button className="primary" onClick={() => navigate("/monitors/new")}>
      <Icon name="plus" /> {t.new}
    </button>
  );
  const serverName = (serverID: string) => servers?.find((s) => s.id === serverID)?.name ?? "?";

  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <p className="eyebrow">{t.eyebrow}</p>
          <h1>{t.title}</h1>
        </div>
        <div className="actions">{newButton}</div>
      </div>
      <p className="muted">{t.description}</p>
      <ErrorText error={error} />

      {(noServers || monitors?.length === 0) && (
        <section className="panel skill-empty">
          <span className="skill-empty-mark" aria-hidden="true">
            <Icon name="eye" />
          </span>
          <h2>{t.empty}</h2>
          <p className="muted">{noServers ? t.noServers : t.example}</p>
          <div className="actions">
            {noServers ? (
              <button className="primary" onClick={() => navigate("/ai/mcp")}>
                <Icon name="plug" /> {t.addServer}
              </button>
            ) : (
              newButton
            )}
          </div>
        </section>
      )}

      {!!monitors?.length && (
        <div className="list-toolbar">
          <span className="muted">{t.count(monitors.length)}</span>
          <SortMenu
            value={sort}
            onChange={(value) => {
              localStorage.setItem("monitor-sort", value);
              setSort(value);
            }}
          />
        </div>
      )}

      {!!monitors?.length && (
        <ul className="panel skill-list" aria-label={t.title}>
          {[...monitors]
            .sort((a, b) => (sort === "name" ? a.name.localeCompare(b.name) : b.created_at.localeCompare(a.created_at)))
            .map((monitor) => {
              const state = status(t, monitor);
              return (
                <li key={monitor.id} className={monitor.enabled ? "" : "disabled"}>
                  <button className="skill-item" onClick={() => navigate(`/monitors/${monitor.id}`)}>
                    <span className="skill-name">
                      <span className={`monitor-dot ${state.key}`} aria-hidden="true" />
                      {monitor.name}
                    </span>
                    <span className="skill-description" title={monitor.last_error || undefined}>
                      {state.label} · {serverName(monitor.mcp_server_id)} · {monitor.tool} · {describe(t, monitor.schedule)}
                      {monitor.last_run_at && ` · ${t.lastRun} ${formatTime(monitor.last_run_at)}`}
                      {monitor.last_status === "failed" && monitor.last_error && ` · ${monitor.last_error}`}
                    </span>
                  </button>
                  <Switch checked={monitor.enabled} onChange={() => toggle(monitor)} label={t.enable(monitor.name)} showLabel={false} />
                  <Menu label={t.more(monitor.name)} buttonClassName="icon ghost" button={<Icon name="more" />}>
                    {(close) => (
                      <>
                        <button role="menuitem" onClick={() => (close(), toggle(monitor))}>
                          <Icon name={monitor.enabled ? "pause" : "play"} /> {monitor.enabled ? t.disable : t.enableMonitor}
                        </button>
                        <button role="menuitem" onClick={() => (close(), runNow(monitor))}>
                          <Icon name="refresh" /> {t.runNow}
                        </button>
                        <button role="menuitem" onClick={() => (close(false), navigate(`/monitors/${monitor.id}`))}>
                          <Icon name="pencil" /> {t.edit}
                        </button>
                        <hr className="menu-divider" role="separator" />
                        <button role="menuitem" className="danger-text" onClick={() => (close(), remove(monitor))}>
                          <Icon name="trash" /> {t.remove}
                        </button>
                      </>
                    )}
                  </Menu>
                </li>
              );
            })}
        </ul>
      )}
    </div>
  );
}

function SortMenu({ value, onChange }: { value: Sort; onChange: (value: Sort) => void }) {
  const t = useText(text);
  const labels: Record<Sort, string> = { recent: t.recent, name: t.alphabetical };
  return (
    <Menu
      label={t.sortLabel}
      button={
        <>
          {t.sort} {labels[value]} <Icon name="chevronDown" />
        </>
      }
    >
      {(close) =>
        (Object.keys(labels) as Sort[]).map((option) => (
          <button
            key={option}
            role="menuitemradio"
            aria-checked={option === value}
            onClick={() => {
              onChange(option);
              close();
            }}
          >
            <span className="menu-check">{option === value && <Icon name="check" />}</span>
            {labels[option]}
          </button>
        ))
      }
    </Menu>
  );
}

function BackLink() {
  const t = useText(text);
  return (
    <button type="button" className="back" onClick={() => navigate("/monitors")}>
      <Icon name="arrowLeft" /> {t.back}
    </button>
  );
}

function MonitorEditor({
  monitor,
  servers,
  refresh,
  onSaved,
  onRunNow,
  onDeleted,
}: {
  monitor?: Monitor;
  servers: McpServer[];
  refresh: number;
  onSaved: () => void;
  onRunNow: () => void;
  onDeleted: () => void;
}) {
  const t = useText(text);
  const initial = parseActions(monitor?.actions ?? '{"notify":true}');
  const [name, setName] = useState(monitor?.name ?? "");
  const [serverID, setServerID] = useState(monitor?.mcp_server_id ?? servers.find((s) => s.enabled)?.id ?? "");
  const [tool, setTool] = useState(monitor?.tool ?? "");
  const [args, setArgs] = useState(monitor ? JSON.stringify(JSON.parse(monitor.args), null, 2) : "{}");
  const [cron, setCron] = useState(!!monitor && !minutesOf(monitor.schedule));
  const [minutes, setMinutes] = useState(monitor ? minutesOf(monitor.schedule) || 5 : 5);
  const [cronText, setCronText] = useState(monitor && !minutesOf(monitor.schedule) ? monitor.schedule : "*/5 * * * *");
  const [condition, setCondition] = useState<Condition>(monitor?.condition ?? "changed");
  const [resultPath, setResultPath] = useState(monitor?.result_path ?? "");
  const [idPath, setIDPath] = useState(monitor?.id_path ?? "");
  const [notify, setNotify] = useState(initial.notify);
  const [ticket, setTicket] = useState(initial.ticket);
  const [chat, setChat] = useState(initial.chat);
  const [enabled, setEnabled] = useState(monitor?.enabled ?? true);
  const [tools, setTools] = useState<McpServerCheck["tools"]>([]);
  const [toolsLoading, setToolsLoading] = useState(false);
  const [boards, setBoards] = useState<Board[]>([]);
  const [runs, setRuns] = useState<MonitorRun[]>([]);
  const [test, setTest] = useState<{ running?: boolean; result?: string; error?: string }>({});
  const [error, setError] = useState<unknown>();
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api.get<Board[]>("/api/boards").then(setBoards).catch(setError);
  }, []);

  useEffect(() => {
    if (!serverID) return setTools([]);
    let active = true;
    setToolsLoading(true);
    api
      .get<McpServerCheck>(`/api/mcp-servers/${serverID}/check`)
      .then((check) => {
        if (!active) return;
        setTools(check.tools);
        setError(check.ok ? undefined : new Error(check.error));
      })
      .catch((reason) => active && setError(reason))
      .finally(() => active && setToolsLoading(false));
    return () => {
      active = false;
    };
  }, [serverID]);

  useEffect(() => {
    if (!monitor) return;
    api.get<MonitorRun[]>(`/api/monitors/${monitor.id}/runs`).then(setRuns).catch(setError);
  }, [monitor?.id, refresh]);

  function parsedArgs() {
    try {
      const value = JSON.parse(args);
      if (value && typeof value === "object" && !Array.isArray(value)) return value as Record<string, unknown>;
    } catch {}
    throw new Error(t.badJSON);
  }

  const body = () => ({
    name: name.trim(),
    mcp_server_id: serverID,
    tool,
    args: parsedArgs(),
    schedule: cron ? cronText.trim() : `@every ${minutes}m`,
    condition,
    result_path: condition === "new_items" ? resultPath.trim() : "",
    id_path: condition === "new_items" ? idPath.trim() : "",
    actions: { notify, ticket, chat },
    enabled,
  });

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      if (monitor) await api.put(`/api/monitors/${monitor.id}`, body());
      else await api.post("/api/monitors", body());
      setError(undefined);
      setSaved(true);
      window.setTimeout(() => setSaved(false), 1500);
      onSaved();
    } catch (reason) {
      setError(reason);
    }
  }

  async function runTest() {
    setTest({ running: true });
    try {
      const result = await api.post<{ result?: string; error?: string }>("/api/monitors/test", {
        mcp_server_id: serverID,
        tool,
        args: parsedArgs(),
      });
      setTest(result);
    } catch (reason) {
      setTest({ error: reason instanceof Error ? reason.message : String(reason) });
    }
  }

  async function remove() {
    if (!monitor || !(await confirmDialog(t.confirmDelete(monitor.name)))) return;
    try {
      await api.delete(`/api/monitors/${monitor.id}`);
      onDeleted();
    } catch (reason) {
      setError(reason);
    }
  }

  const pretty = (raw: string) => {
    try {
      return JSON.stringify(JSON.parse(raw), null, 2);
    } catch {
      return raw;
    }
  };

  return (
    <div className="stack">
      <BackLink />
      <div className="page-heading">
        <h1>{monitor ? monitor.name : t.new}</h1>
        {monitor && (
          <div className="actions">
            <button type="button" onClick={onRunNow}>
              <Icon name="refresh" /> {t.runNow}
            </button>
          </div>
        )}
      </div>
      <form className="panel skill-editor" onSubmit={submit} aria-label={monitor ? t.editAria(monitor.name) : t.new}>
        <div className="skill-fields">
          <label className="grow">
            {t.name}
            <input autoFocus required value={name} onChange={(event) => setName(event.target.value)} />
          </label>
        </div>
        <div className="skill-fields">
          <label>
            {t.server}
            <Picker
              label={t.server}
              options={servers.map((s) => ({ id: s.id, name: s.name }))}
              value={serverID}
              onChange={(id) => {
                setServerID(id);
                setTool("");
              }}
            />
          </label>
          <div className="field grow">
            <span className="label-row">
              {t.tool} {toolsLoading && <span className="spinner" role="status" aria-label={t.loadingTools} />}
            </span>
            <Picker
              label={t.tool}
              options={[
                ...tools.map((item) => ({ id: item.name, name: item.name })),
                ...(tool && !tools.some((item) => item.name === tool) ? [{ id: tool, name: tool }] : []),
              ]}
              value={tool}
              onChange={setTool}
            />
          </div>
        </div>
        {tools.find((item) => item.name === tool)?.description && (
          <p className="muted">{tools.find((item) => item.name === tool)?.description}</p>
        )}
        <label>
          {t.args}
          <textarea rows={4} spellCheck={false} className="monitor-args" value={args} onChange={(event) => setArgs(event.target.value)} />
        </label>
        <div className="connector-buttons">
          <button type="button" disabled={!serverID || !tool || test.running} onClick={runTest}>
            <Icon name="play" /> {test.running ? t.testing : t.test}
          </button>
        </div>
        <div aria-live="polite">
          <ErrorText error={test.error && new Error(test.error)} />
          {test.result !== undefined && (
            <pre className="mcp-config monitor-result" aria-label={t.testResult}>
              {pretty(test.result).slice(0, 20000)}
            </pre>
          )}
        </div>

        <fieldset className="skill-fields">
          <legend>{t.schedule}</legend>
          <label>
            {t.repeat}
            <Picker
              label={t.repeat}
              options={[
                { id: "every", name: t.interval },
                { id: "cron", name: t.cron },
              ]}
              value={cron ? "cron" : "every"}
              onChange={(value) => setCron(value === "cron")}
            />
          </label>
          {cron ? (
            <label className="grow">
              {t.cronLabel}
              <input required spellCheck={false} placeholder="*/5 * * * *" value={cronText} onChange={(event) => setCronText(event.target.value)} />
            </label>
          ) : (
            <label>
              {t.everyMinutes}
              <input type="number" min={1} required value={minutes} onChange={(event) => setMinutes(Number(event.target.value))} />
            </label>
          )}
        </fieldset>

        <fieldset className="stack">
          <legend>{t.condition}</legend>
          <Picker
            label={t.condition}
            options={[
              { id: "changed", name: t.conditionChanged },
              { id: "non_empty", name: t.conditionNonEmpty },
              { id: "new_items", name: t.conditionNewItems },
            ]}
            value={condition}
            onChange={(value) => setCondition(value as Condition)}
          />
          {condition === "new_items" && (
            <>
              <div className="skill-fields">
                <label className="grow">
                  {t.resultPath}
                  <input spellCheck={false} placeholder="messages" value={resultPath} onChange={(event) => setResultPath(event.target.value)} />
                </label>
                <label className="grow">
                  {t.idPath}
                  <input required spellCheck={false} placeholder="ts" value={idPath} onChange={(event) => setIDPath(event.target.value)} />
                </label>
              </div>
              <p className="muted">{t.pathHelp}</p>
            </>
          )}
        </fieldset>

        <fieldset className="stack">
          <legend>{t.actions}</legend>
          <Switch checked={notify} onChange={setNotify} label={t.notify} />
          <Switch
            checked={!!ticket}
            onChange={(on) => setTicket(on ? { board_id: boards[0]?.id ?? "", title: "{{name}}: {{count}} new" } : null)}
            label={t.ticket}
          />
          {ticket && (
            <div className="skill-fields">
              <label>
                {t.board}
                <Picker
                  label={t.board}
                  options={boards.map((b) => ({ id: b.id, name: b.name }))}
                  value={ticket.board_id}
                  onChange={(board_id) => setTicket({ ...ticket, board_id })}
                />
              </label>
              <label className="grow">
                {t.ticketTitle}
                <input required value={ticket.title} onChange={(event) => setTicket({ ...ticket, title: event.target.value })} />
              </label>
            </div>
          )}
          <Switch
            checked={!!chat}
            onChange={(on) => setChat(on ? { prompt: "{{name}} found {{count}} new items:\n\n{{items}}" } : null)}
            label={t.chat}
          />
          {chat && (
            <label>
              {t.prompt}
              <textarea rows={4} required value={chat.prompt} onChange={(event) => setChat({ prompt: event.target.value })} />
            </label>
          )}
          {(ticket || chat) && <p className="muted">{t.placeholders}</p>}
        </fieldset>

        <ErrorText error={error} />
        <div className="connector-buttons">
          <Switch checked={enabled} onChange={setEnabled} label={t.enabled} />
          <button className="primary" disabled={!serverID || !tool}>
            {saved ? t.saved : monitor ? t.save : t.create}
          </button>
          {monitor && <IconButton type="button" className="danger-text" icon="trash" label={t.deleteTitle(monitor.name)} onClick={remove} />}
        </div>
      </form>

      {monitor && (
        <section className="stack" aria-labelledby="monitor-runs-title">
          <h2 id="monitor-runs-title">{t.runs}</h2>
          {runs.length === 0 && <p className="muted">{t.noRuns}</p>}
          {runs.length > 0 && (
            <ul className="panel skill-list" aria-label={t.runs}>
              {runs.map((run) => (
                <li key={run.id}>
                  <span className="skill-item">
                    <span className="skill-name">
                      <span className={`monitor-dot ${run.status}`} aria-hidden="true" />
                      {formatTime(run.started_at)}
                    </span>
                    <span className="skill-description" title={run.error || undefined}>
                      {t[run.status]}
                      {run.new_count > 0 && ` · ${t.newItems(run.new_count)}`}
                      {run.error && ` · ${run.error}`}
                    </span>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
    </div>
  );
}
