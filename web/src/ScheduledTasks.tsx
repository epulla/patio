import { confirmDialog } from "./confirm";
import { useEffect, useState, type FormEvent } from "react";
import {
  api,
  type AllowedFolder,
  type ChatDefaults,
  type Connector,
  type ScheduledTask,
  type ScheduledTaskRun,
} from "./api";
import { ErrorText, navigate } from "./App";
import { lateTitle, useModels } from "./Chats";
import ModelPicker from "./ModelPicker";
import { ConnectorRequired } from "./Connectors";
import { Icon, IconButton } from "./icons";
import Menu from "./Menu";
import Switch from "./Switch";

type Repeat = "once" | "hours" | "daily" | "weekdays" | "weekly" | "monthly" | "cron";
type When = { repeat: Repeat; at: string; time: string; hours: number; days: number[]; day: number; cron: string };
type Fields = Omit<ScheduledTask, "id" | "next_run_at" | "created_at" | "updated_at">;
type TaskSort = "recent" | "name";

const repeatLabels: Record<Repeat, string> = {
  once: "Once",
  hours: "Every few hours",
  daily: "Every day",
  weekdays: "Every weekday (Mon–Fri)",
  weekly: "Every week",
  monthly: "Every month",
  cron: "Advanced (cron)",
};
const dayNames = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const skippedTitle = "The previous run was still working, so this one didn't start.";
const statusLabels: Record<ScheduledTaskRun["status"], string> = {
  running: "Running",
  done: "Finished",
  failed: "Failed",
  skipped: "Skipped",
};

const pad = (value: number) => String(value).padStart(2, "0");

function localInput(date: Date) {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function clock(time: string) {
  const [hour, minute] = time.split(":").map(Number);
  return new Date(2000, 0, 1, hour, minute).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

function formatRun(iso: string) {
  return new Date(iso).toLocaleString([], { weekday: "short", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

function parseWhen(task?: ScheduledTask): When {
  const tomorrow = new Date(Date.now() + 86400000);
  tomorrow.setHours(9, 0, 0, 0);
  const when: When = { repeat: "daily", at: localInput(tomorrow), time: "09:00", hours: 4, days: [1], day: 1, cron: "0 9 * * *" };
  if (!task) return when;
  if (task.run_at) return { ...when, repeat: "once", at: localInput(new Date(task.run_at)) };
  when.cron = task.schedule;
  let match = task.schedule.match(/^0 \*\/(\d+) \* \* \*$/);
  if (match) return { ...when, repeat: "hours", hours: Number(match[1]) };
  match = task.schedule.match(/^(\d+) (\d+) (\*|\d+) \* (\*|1-5|[0-6](?:,[0-6])*)$/);
  if (!match || (match[3] !== "*" && match[4] !== "*")) return { ...when, repeat: "cron" };
  const time = `${pad(Number(match[2]))}:${pad(Number(match[1]))}`;
  if (match[3] !== "*") return { ...when, repeat: "monthly", time, day: Number(match[3]) };
  if (match[4] === "*") return { ...when, repeat: "daily", time };
  if (match[4] === "1-5") return { ...when, repeat: "weekdays", time };
  return { ...when, repeat: "weekly", time, days: match[4].split(",").map(Number) };
}

function compile(when: When): { schedule: string; run_at: string | null } {
  const [hour, minute] = when.time.split(":").map(Number);
  const at = `${minute} ${hour}`;
  switch (when.repeat) {
    case "once":
      return { schedule: "", run_at: when.at ? new Date(when.at).toISOString() : null };
    case "hours":
      return { schedule: `0 */${when.hours} * * *`, run_at: null };
    case "daily":
      return { schedule: `${at} * * *`, run_at: null };
    case "weekdays":
      return { schedule: `${at} * * 1-5`, run_at: null };
    case "weekly":
      return { schedule: `${at} * * ${[...when.days].sort().join(",")}`, run_at: null };
    case "monthly":
      return { schedule: `${at} ${when.day} * *`, run_at: null };
    case "cron":
      return { schedule: when.cron.trim(), run_at: null };
  }
}

function describe(task: ScheduledTask) {
  const when = parseWhen(task);
  switch (when.repeat) {
    case "once":
      return `Once on ${formatRun(task.run_at!)}`;
    case "hours":
      return when.hours === 1 ? "Every hour" : `Every ${when.hours} hours`;
    case "daily":
      return `Every day at ${clock(when.time)}`;
    case "weekdays":
      return `Weekdays at ${clock(when.time)}`;
    case "weekly":
      return `Every ${when.days.map((day) => dayNames[day]).join(", ")} at ${clock(when.time)}`;
    case "monthly":
      return `Monthly on day ${when.day} at ${clock(when.time)}`;
    case "cron":
      return `Custom: ${task.schedule}`;
  }
}

function fieldsOf(task: ScheduledTask): Fields {
  const { id: _id, next_run_at: _next, created_at: _created, updated_at: _updated, ...fields } = task;
  return fields;
}

export default function ScheduledTasks({ id, refresh }: { id?: string; refresh: number }) {
  const [tasks, setTasks] = useState<ScheduledTask[]>();
  const [connectors, setConnectors] = useState<Connector[]>();
  const [sort, setSort] = useState<TaskSort>(() =>
    localStorage.getItem("scheduled-task-sort") === "name" ? "name" : "recent",
  );
  const [error, setError] = useState<unknown>();
  const [reload, setReload] = useState(0);

  useEffect(() => {
    Promise.all([api.get<ScheduledTask[]>("/api/scheduled-tasks"), api.get<Connector[]>("/api/connectors")])
      .then(([list, connectorList]) => {
        setTasks(list);
        setConnectors(connectorList);
        setError(undefined);
      })
      .catch(setError);
  }, [refresh, reload]);

  const changed = () => setReload((value) => value + 1);

  async function toggle(task: ScheduledTask) {
    try {
      await api.put(`/api/scheduled-tasks/${task.id}`, { ...fieldsOf(task), enabled: !task.enabled });
      changed();
    } catch (reason) {
      setError(reason);
    }
  }

  async function remove(task: ScheduledTask) {
    if (!(await confirmDialog(`Delete scheduled task "${task.title}"?`))) return;
    try {
      await api.delete(`/api/scheduled-tasks/${task.id}`);
      changed();
    } catch (reason) {
      setError(reason);
    }
  }

  if (id) {
    const task = tasks?.find((t) => t.id === id);
    if (id !== "new" && !task) {
      return tasks ? (
        <div className="stack">
          <BackLink />
          <p className="muted">This scheduled task no longer exists.</p>
        </div>
      ) : null;
    }
    return (
      <TaskEditor
        key={id}
        task={task}
        refresh={refresh}
        onSaved={() => {
          changed();
          if (!task) navigate("/ai/scheduled");
        }}
        onDeleted={() => navigate("/ai/scheduled")}
      />
    );
  }

  const noConnectors = connectors?.length === 0;
  const newButton = !noConnectors && (
    <button className="primary" onClick={() => navigate("/ai/scheduled/new")}>
      <Icon name="plus" /> New scheduled task
    </button>
  );

  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <p className="eyebrow">AI</p>
          <h1>Scheduled tasks</h1>
        </div>
        <div className="actions">{newButton}</div>
      </div>
      <p className="muted">
        A scheduled task sends a prompt to an AI connector at the times you pick. Each run starts a new chat. Tasks
        run only while Patio is open.
      </p>
      <ErrorText error={error} />

      {noConnectors && <ConnectorRequired then="create a scheduled task" />}

      {tasks?.length === 0 && !noConnectors && (
        <section className="panel skill-empty">
          <span className="skill-empty-mark" aria-hidden="true">
            <Icon name="clock" />
          </span>
          <h2>No scheduled tasks yet</h2>
          <p className="muted">
            For example, every weekday at 9:00 AM: "Summarize the tickets due this week and list anything overdue."
          </p>
          <div className="actions">{newButton}</div>
        </section>
      )}

      {!!tasks?.length && (
        <div className="list-toolbar">
          <span className="muted">
            {tasks.length} scheduled task{tasks.length === 1 ? "" : "s"}
          </span>
          <SortMenu
            value={sort}
            onChange={(value) => {
              localStorage.setItem("scheduled-task-sort", value);
              setSort(value);
            }}
          />
        </div>
      )}

      {!!tasks?.length && (
        <ul className="panel skill-list" aria-label="Scheduled tasks">
          {[...tasks]
            .sort((a, b) =>
              sort === "name" ? a.title.localeCompare(b.title) : b.created_at.localeCompare(a.created_at),
            )
            .map((task) => (
              <li key={task.id} className={task.enabled ? "" : "disabled"}>
                <button className="skill-item" onClick={() => navigate(`/ai/scheduled/${task.id}`)}>
                  <span className="skill-name">{task.title}</span>
                  <span className="skill-description">
                    {describe(task)} · {task.next_run_at ? `Next: ${formatRun(task.next_run_at)}` : "Not scheduled"}
                  </span>
                </button>
                <Switch
                  checked={task.enabled}
                  onChange={() => toggle(task)}
                  label={`Enable ${task.title}`}
                  showLabel={false}
                />
                <Menu label={`More actions for ${task.title}`} buttonClassName="icon ghost" button={<Icon name="more" />}>
                  {(close) => (
                    <>
                      <button role="menuitem" onClick={() => (close(), toggle(task))}>
                        <Icon name={task.enabled ? "pause" : "play"} /> {task.enabled ? "Disable task" : "Enable task"}
                      </button>
                      <button role="menuitem" onClick={() => (close(false), navigate(`/ai/scheduled/${task.id}`))}>
                        <Icon name="pencil" /> Edit
                      </button>
                      <hr className="menu-divider" role="separator" />
                      <button role="menuitem" className="danger-text" onClick={() => (close(), remove(task))}>
                        <Icon name="trash" /> Remove
                      </button>
                    </>
                  )}
                </Menu>
              </li>
            ))}
        </ul>
      )}
    </div>
  );
}

const sortLabels: Record<TaskSort, string> = { recent: "Most recent", name: "Alphabetical" };

function SortMenu({ value, onChange }: { value: TaskSort; onChange: (value: TaskSort) => void }) {
  return (
    <Menu
      label="Sort scheduled tasks"
      button={
        <>
          Sort: {sortLabels[value]} <Icon name="chevronDown" />
        </>
      }
    >
      {(close) =>
        (Object.keys(sortLabels) as TaskSort[]).map((option) => (
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
            {sortLabels[option]}
          </button>
        ))
      }
    </Menu>
  );
}

function BackLink() {
  return (
    <button type="button" className="back" onClick={() => navigate("/ai/scheduled")}>
      <Icon name="arrowLeft" /> Scheduled tasks
    </button>
  );
}

function TaskEditor({
  task,
  refresh,
  onSaved,
  onDeleted,
}: {
  task?: ScheduledTask;
  refresh: number;
  onSaved: () => void;
  onDeleted: () => void;
}) {
  const [title, setTitle] = useState(task?.title ?? "");
  const [prompt, setPrompt] = useState(task?.prompt ?? "");
  const [connectorID, setConnectorID] = useState(task?.connector_id ?? "");
  const [model, setModel] = useState(task?.model ?? "");
  const [folder, setFolder] = useState(task?.folder ?? "");
  const [thinking, setThinking] = useState(task?.thinking ?? true);
  const [autoAllow, setAutoAllow] = useState(task?.auto_allow ?? true);
  const [enabled, setEnabled] = useState(task?.enabled ?? true);
  const [when, setWhen] = useState(() => parseWhen(task));
  const [connectors, setConnectors] = useState<Connector[]>();
  const [folders, setFolders] = useState<AllowedFolder[]>([]);
  const [runs, setRuns] = useState<ScheduledTaskRun[]>([]);
  const [preview, setPreview] = useState<{ runs?: string[]; error?: unknown }>({});
  const [error, setError] = useState<unknown>();
  const [saved, setSaved] = useState(false);
  const { models, loading: modelsLoading } = useModels(connectorID || null);
  const defaultModel = (list: Connector[], id: string) => list.find((c) => c.id === id && c.kind === "local")?.model ?? "";

  const fields: Fields = {
    title: title.trim(),
    prompt,
    connector_id: connectorID || null,
    model,
    folder: folder || null,
    thinking,
    auto_allow: autoAllow,
    enabled,
    ...compile(when),
  };
  const { schedule, run_at } = fields;
  const dirty = !task || Object.entries(fieldsOf(task)).some(([key, value]) => value !== fields[key as keyof Fields]);

  useEffect(() => {
    Promise.all([
      api.get<Connector[]>("/api/connectors"),
      api.get<AllowedFolder[]>("/api/settings/folders"),
      task ? undefined : api.get<ChatDefaults>("/api/settings/defaults"),
    ])
      .then(([connectorList, folderList, defaults]) => {
        setConnectors(connectorList);
        setFolders(folderList);
        if (!defaults) return;
        const id = defaults.connector_id && connectorList.some((c) => c.id === defaults.connector_id)
          ? defaults.connector_id
          : connectorList[0]?.id ?? "";
        setConnectorID(id);
        setModel(defaultModel(connectorList, id));
        setFolder(defaults.folder ?? "");
        setThinking(defaults.thinking);
      })
      .catch(setError);
  }, []);

  useEffect(() => {
    if (!task) return;
    api
      .get<{ task: ScheduledTask; runs: ScheduledTaskRun[] }>(`/api/scheduled-tasks/${task.id}`)
      .then((detail) => setRuns(detail.runs))
      .catch(setError);
  }, [task?.id, refresh]);

  useEffect(() => {
    if (!schedule && !run_at) return setPreview({});
    let active = true;
    api
      .post<{ runs: string[] }>("/api/scheduled-tasks/preview", { schedule, run_at })
      .then((result) => active && setPreview({ runs: result.runs }))
      .catch((reason) => active && setPreview({ error: reason }));
    return () => {
      active = false;
    };
  }, [schedule, run_at]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      if (task) await api.put(`/api/scheduled-tasks/${task.id}`, fields);
      else await api.post("/api/scheduled-tasks", fields);
      setError(undefined);
      setSaved(true);
      window.setTimeout(() => setSaved(false), 1500);
      onSaved();
    } catch (reason) {
      setError(reason);
    }
  }

  async function remove() {
    if (!task || !(await confirmDialog(`Delete scheduled task "${task.title}"?`))) return;
    try {
      await api.delete(`/api/scheduled-tasks/${task.id}`);
      onDeleted();
    } catch (reason) {
      setError(reason);
    }
  }

  const set = (patch: Partial<When>) => setWhen((current) => ({ ...current, ...patch }));
  const timeInput = (
    <label>
      At
      <input type="time" required value={when.time} onChange={(event) => set({ time: event.target.value })} />
    </label>
  );

  return (
    <div className="stack">
      <BackLink />
      <div className="page-heading">
        <h1>{task ? task.title : "New scheduled task"}</h1>
      </div>
      {connectors?.length === 0 && (
        <ConnectorRequired then={task ? "pick it for this task" : "create a scheduled task"} />
      )}
      {(task || !!connectors?.length) && (
      <form
        className="panel skill-editor"
        onSubmit={submit}
        aria-label={task ? `Edit ${task.title}` : "New scheduled task"}
      >
        <div className="skill-fields">
          <label className="grow">
            Title
            <input
              autoFocus
              required
              placeholder="Weekly ticket summary"
              value={title}
              onChange={(event) => setTitle(event.target.value)}
            />
          </label>
        </div>
        <label>
          Prompt
          <textarea
            rows={6}
            required
            placeholder="Summarize the tickets due this week and list anything overdue."
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
          />
        </label>
        <div className="skill-fields">
          <label>
            Connector
            <select
              required
              value={connectorID}
              onChange={(event) => {
                setConnectorID(event.target.value);
                setModel(defaultModel(connectors ?? [], event.target.value));
              }}
            >
              {connectors?.length === 0 && <option value="">No connector</option>}
              {task && !connectorID && <option value="">Connector removed</option>}
              {connectors?.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name} · {c.kind}
                </option>
              ))}
            </select>
          </label>
          <div className="field">
            <span className="label-row">
              Model {modelsLoading && <span className="spinner" role="status" aria-label="Loading models" />}
            </span>
            <ModelPicker
              models={models}
              value={model}
              loading={modelsLoading}
              pinKind={connectors?.find((c) => c.id === connectorID)?.kind}
              onChange={setModel}
            />
          </div>
          <label className="grow">
            Folder
            <select value={folder} onChange={(event) => setFolder(event.target.value)}>
              <option value="">Scratch folder (no files shared)</option>
              {folders.map((f) => (
                <option key={f.id} value={f.path}>
                  {f.path}
                </option>
              ))}
              {folder && !folders.some((f) => f.path === folder) && <option value={folder}>{folder}</option>}
            </select>
          </label>
          <label>
            Mode
            <select value={autoAllow ? "auto" : "ask"} onChange={(event) => setAutoAllow(event.target.value === "auto")}>
              <option value="ask">Ask before changes</option>
              <option value="auto">Auto-allow everything</option>
            </select>
          </label>
        </div>
        <p className="muted">
          {autoAllow
            ? "Every file change and command runs without asking."
            : "File changes and commands wait for your approval in the run's chat. You get a notification."}
        </p>
        <Switch checked={thinking} onChange={setThinking} label="Thinking" />

        <fieldset className="skill-fields">
          <legend>Schedule</legend>
          <label>
            Repeat
            <select value={when.repeat} onChange={(event) => set({ repeat: event.target.value as Repeat })}>
              {(Object.keys(repeatLabels) as Repeat[]).map((repeat) => (
                <option key={repeat} value={repeat}>
                  {repeatLabels[repeat]}
                </option>
              ))}
            </select>
          </label>
          {when.repeat === "once" && (
            <label>
              On
              <input type="datetime-local" required value={when.at} onChange={(event) => set({ at: event.target.value })} />
            </label>
          )}
          {when.repeat === "hours" && (
            <label>
              Every (hours)
              <input
                type="number"
                min={1}
                max={23}
                required
                value={when.hours}
                onChange={(event) => set({ hours: Number(event.target.value) })}
              />
            </label>
          )}
          {when.repeat === "weekly" && (
            <div role="group" aria-label="Days of the week" className="day-picker">
              {dayNames.map((name, day) => (
                <label key={name}>
                  <input
                    type="checkbox"
                    checked={when.days.includes(day)}
                    onChange={(event) =>
                      set({ days: event.target.checked ? [...when.days, day] : when.days.filter((d) => d !== day) })
                    }
                  />
                  {name}
                </label>
              ))}
            </div>
          )}
          {when.repeat === "monthly" && (
            <label>
              Day of month
              <input
                type="number"
                min={1}
                max={31}
                required
                value={when.day}
                onChange={(event) => set({ day: Number(event.target.value) })}
              />
            </label>
          )}
          {["daily", "weekdays", "weekly", "monthly"].includes(when.repeat) && timeInput}
          {when.repeat === "cron" && (
            <label className="grow">
              Cron expression (minute hour day month weekday)
              <input
                required
                spellCheck={false}
                placeholder="0 9 * * 1-5"
                value={when.cron}
                onChange={(event) => set({ cron: event.target.value })}
              />
            </label>
          )}
        </fieldset>
        <div aria-live="polite">
          {preview.runs && (
            <p className="muted">Next runs: {preview.runs.map(formatRun).join(" · ")}</p>
          )}
          <ErrorText error={preview.error} />
        </div>
        <p className="muted">Runs only while Patio is open.</p>

        <ErrorText error={error} />
        <div className="connector-buttons">
          <Switch checked={enabled} onChange={setEnabled} label="Enabled" />
          <button
            className="primary"
            disabled={!dirty || !connectorID || (when.repeat === "weekly" && when.days.length === 0)}
          >
            {saved ? "Saved" : task ? "Save" : "Create task"}
          </button>
          {task && (
            <IconButton type="button" className="danger-text" icon="trash" label={`Delete ${task.title}`} onClick={remove} />
          )}
        </div>
      </form>
      )}

      {task && (
        <section className="stack" aria-labelledby="runs-title">
          <h2 id="runs-title">Recent runs</h2>
          {runs.length === 0 && <p className="muted">No runs yet.</p>}
          {runs.length > 0 && (
            <ul className="panel skill-list" aria-label="Recent runs">
              {runs.map((run) => (
                <li key={run.id}>
                  <span className="skill-item">
                    <span className="skill-name">
                      {formatRun(run.scheduled_for)}
                      {run.late && (
                        <span className="run-label" title={lateTitle}>
                          Ran late
                        </span>
                      )}
                    </span>
                    <span className="skill-description" title={run.status === "skipped" ? skippedTitle : run.error}>
                      {statusLabels[run.status]}
                      {run.status === "skipped" && ` · ${skippedTitle}`}
                      {run.error && ` · ${run.error}`}
                    </span>
                  </span>
                  {run.chat_id && (
                    <button type="button" onClick={() => navigate(`/ai/chats/${run.chat_id}`)}>
                      <Icon name="chat" /> Open chat
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
    </div>
  );
}
