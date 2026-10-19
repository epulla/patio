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
import { startModel, useModels } from "./Chats";
import ModelPicker from "./ModelPicker";
import { ConnectorRequired } from "./Connectors";
import { Icon, IconButton } from "./icons";
import Menu from "./Menu";
import Switch from "./Switch";
import Picker from "./Picker";
import { lang, locale, useText } from "./i18n";

type Repeat = "once" | "hours" | "daily" | "weekdays" | "weekly" | "monthly" | "cron";
type When = { repeat: Repeat; at: string; time: string; hours: number; days: number[]; day: number; cron: string };
type Fields = Omit<ScheduledTask, "id" | "next_run_at" | "created_at" | "updated_at">;
type TaskSort = "recent" | "name";

const text = {
  en: {
    once: "Once", hours: "Every few hours", daily: "Every day", weekdays: "Every weekday (Mon–Fri)", weekly: "Every week", monthly: "Every month", cron: "Advanced (cron)",
    days: ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"], skipped: "The previous run was still working, so this one didn't start.",
    running: "Running", waiting: "Waiting for your answer", done: "Finished", failed: "Failed", skippedStatus: "Skipped",
    onceOn: "Once on", everyHour: "Every hour", everyHours: (n: number) => `Every ${n} hours`, dailyAt: "Every day at", weekdaysAt: "Weekdays at", everyDaysAt: (days: string) => `Every ${days} at`, monthlyOn: (n: number) => `Monthly on day ${n} at`, custom: (cron: string) => `Custom: ${cron}`,
    taskGone: "This scheduled task no longer exists.", new: "New scheduled task", description: "A scheduled task sends a prompt to an AI connector at the times you pick. Each run starts a new chat. Tasks run only while Patio is open.", create: "create a scheduled task", empty: "No scheduled tasks yet", example: 'For example, every weekday at 9:00 AM: "Summarize the tickets due this week and list anything overdue."', taskCount: (n: number) => `${n} scheduled task${n === 1 ? "" : "s"}`, ariaTasks: "Scheduled tasks", enable: (title: string) => `Enable ${title}`, more: (title: string) => `More actions for ${title}`, disable: "Disable task", enableTask: "Enable task", edit: "Edit", remove: "Remove", recent: "Most recent", alphabetical: "Alphabetical", sortLabel: "Sort scheduled tasks", sort: "Sort:", back: "Scheduled tasks",
    editNew: "New scheduled task", editAria: (title: string) => `Edit ${title}`, title: "Title", connector: "Connector", noConnector: "No connector", connectorRemoved: "Connector removed", loadingModels: "Loading models", model: "Model", folder: "Folder", scratch: "Scratch folder (no files shared)", mode: "Mode", ask: "Ask before changes", auto: "Auto-allow everything", autoDesc: "Every file change and command runs without asking.", askDesc: "File changes and commands wait for your approval in the run's chat. You get a notification.", thinking: "Thinking", schedule: "Schedule", repeat: "Repeat", at: "At", on: "On", everyHoursLabel: "Every (hours)", daysWeek: "Days of the week", dayMonth: "Day of month", cronLabel: "Cron expression (minute hour day month weekday)", nextRuns: "Next runs:", openOnly: "Runs only while Patio is open.", enabled: "Enabled", saved: "Saved", save: "Save", createTask: "Create task", deleteTitle: (title: string) => `Delete ${title}`, recentRuns: "Recent runs", noRuns: "No runs yet.", runsAria: "Recent runs", late: "Ran late", openChat: "Open chat",
  },
  es: {
    once: "Una vez", hours: "Cada pocas horas", daily: "Todos los días", weekdays: "Cada día laborable (lun–vie)", weekly: "Cada semana", monthly: "Cada mes", cron: "Avanzado (cron)",
    days: ["dom", "lun", "mar", "mié", "jue", "vie", "sáb"], skipped: "La ejecución anterior seguía en curso, así que esta no empezó.",
    running: "En curso", waiting: "Esperando tu respuesta", done: "Terminada", failed: "Fallida", skippedStatus: "Omitida",
    onceOn: "Una vez el", everyHour: "Cada hora", everyHours: (n: number) => `Cada ${n} horas`, dailyAt: "Todos los días a las", weekdaysAt: "Los días laborables a las", everyDaysAt: (days: string) => `Cada ${days} a las`, monthlyOn: (n: number) => `Cada mes, el día ${n} a las`, custom: (cron: string) => `Personalizado: ${cron}`,
    taskGone: "Esta tarea programada ya no existe.", new: "Nueva tarea programada", description: "Una tarea programada envía una instrucción a un conector de IA en los horarios que elijas. Cada ejecución inicia un chat nuevo. Las tareas se ejecutan solo mientras Patio esté abierto.", create: "crear una tarea programada", empty: "Aún no hay tareas programadas", example: 'Por ejemplo, cada día laborable a las 9:00: "Resume los tickets que vencen esta semana y enumera los atrasados."', taskCount: (n: number) => `${n} tarea${n === 1 ? "" : "s"} programada${n === 1 ? "" : "s"}`, ariaTasks: "Tareas programadas", enable: (title: string) => `Activar ${title}`, more: (title: string) => `Más acciones para ${title}`, disable: "Desactivar tarea", enableTask: "Activar tarea", edit: "Editar", remove: "Eliminar", recent: "Más recientes", alphabetical: "Alfabético", sortLabel: "Ordenar tareas programadas", sort: "Orden:", back: "Tareas programadas",
    editNew: "Nueva tarea programada", editAria: (title: string) => `Editar ${title}`, title: "Título", connector: "Conector", noConnector: "Sin conector", connectorRemoved: "Conector eliminado", loadingModels: "Cargando modelos", model: "Modelo", folder: "Carpeta", scratch: "Carpeta temporal (no se comparten archivos)", mode: "Modo", ask: "Preguntar antes de hacer cambios", auto: "Permitir todo automáticamente", autoDesc: "Cada cambio de archivo y comando se ejecuta sin preguntar.", askDesc: "Los cambios y comandos esperan tu aprobación en el chat de la ejecución. Recibirás una notificación.", thinking: "Razonamiento", schedule: "Programación", repeat: "Repetir", at: "A las", on: "El", everyHoursLabel: "Cada (horas)", daysWeek: "Días de la semana", dayMonth: "Día del mes", cronLabel: "Expresión cron (minuto hora día mes día-semana)", nextRuns: "Próximas ejecuciones:", openOnly: "Se ejecuta solo mientras Patio esté abierto.", enabled: "Activada", saved: "Guardada", save: "Guardar", createTask: "Crear tarea", deleteTitle: (title: string) => `Eliminar ${title}`, recentRuns: "Ejecuciones recientes", noRuns: "Aún no hay ejecuciones.", runsAria: "Ejecuciones recientes", late: "Se ejecutó tarde", openChat: "Abrir chat",
  },
};

const statusLabel = (status: ScheduledTaskRun["status"]) =>
  ({ running: text[lang()].running, waiting: text[lang()].waiting, done: text[lang()].done, failed: text[lang()].failed, skipped: text[lang()].skippedStatus })[status];

const pad = (value: number) => String(value).padStart(2, "0");

function localInput(date: Date) {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function clock(time: string) {
  const [hour, minute] = time.split(":").map(Number);
  return new Date(2000, 0, 1, hour, minute).toLocaleTimeString(locale(), { hour: "numeric", minute: "2-digit" });
}

function formatRun(iso: string) {
  return new Date(iso).toLocaleString(locale(), { weekday: "short", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
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
      return `${text[lang()].onceOn} ${formatRun(task.run_at!)}`;
    case "hours":
      return when.hours === 1 ? text[lang()].everyHour : text[lang()].everyHours(when.hours);
    case "daily":
      return `${text[lang()].dailyAt} ${clock(when.time)}`;
    case "weekdays":
      return `${text[lang()].weekdaysAt} ${clock(when.time)}`;
    case "weekly":
      return `${text[lang()].everyDaysAt(when.days.map((day) => text[lang()].days[day]).join(", "))} ${clock(when.time)}`;
    case "monthly":
      return text[lang()].monthlyOn(when.day) + ` ${clock(when.time)}`;
    case "cron":
      return text[lang()].custom(task.schedule);
  }
}

function fieldsOf(task: ScheduledTask): Fields {
  const { id: _id, next_run_at: _next, created_at: _created, updated_at: _updated, ...fields } = task;
  return fields;
}

export default function ScheduledTasks({ id, refresh }: { id?: string; refresh: number }) {
  const t = useText(text);
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
    if (!(await confirmDialog(lang() === "es" ? `¿Eliminar la tarea programada "${task.title}"?` : `Delete scheduled task "${task.title}"?`))) return;
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
          <p className="muted">{t.taskGone}</p>
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
      <Icon name="plus" /> {t.new}
    </button>
  );

  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <p className="eyebrow">AI</p>
          <h1>{t.ariaTasks}</h1>
        </div>
        <div className="actions">{newButton}</div>
      </div>
      <p className="muted">
        {t.description}
      </p>
      <ErrorText error={error} />

      {noConnectors && <ConnectorRequired then={t.create} />}

      {tasks?.length === 0 && !noConnectors && (
        <section className="panel skill-empty">
          <span className="skill-empty-mark" aria-hidden="true">
            <Icon name="clock" />
          </span>
          <h2>{t.empty}</h2>
          <p className="muted">{t.example}</p>
          <div className="actions">{newButton}</div>
        </section>
      )}

      {!!tasks?.length && (
        <div className="list-toolbar">
          <span className="muted">
            {t.taskCount(tasks.length)}
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
        <ul className="panel skill-list" aria-label={t.ariaTasks}>
          {[...tasks]
            .sort((a, b) =>
              sort === "name" ? a.title.localeCompare(b.title) : b.created_at.localeCompare(a.created_at),
            )
            .map((task) => (
              <li key={task.id} className={task.enabled ? "" : "disabled"}>
                <button className="skill-item" onClick={() => navigate(`/ai/scheduled/${task.id}`)}>
                  <span className="skill-name">{task.title}</span>
                  <span className="skill-description">
                    {describe(task)} · {task.next_run_at ? `${lang() === "es" ? "Siguiente:" : "Next:"} ${formatRun(task.next_run_at)}` : lang() === "es" ? "Sin programar" : "Not scheduled"}
                  </span>
                </button>
                <Switch
                  checked={task.enabled}
                  onChange={() => toggle(task)}
                  label={t.enable(task.title)}
                  showLabel={false}
                />
                <Menu label={t.more(task.title)} buttonClassName="icon ghost" button={<Icon name="more" />}>
                  {(close) => (
                    <>
                      <button role="menuitem" onClick={() => (close(), toggle(task))}>
                        <Icon name={task.enabled ? "pause" : "play"} /> {task.enabled ? t.disable : t.enableTask}
                      </button>
                      <button role="menuitem" onClick={() => (close(false), navigate(`/ai/scheduled/${task.id}`))}>
                        <Icon name="pencil" /> {t.edit}
                      </button>
                      <hr className="menu-divider" role="separator" />
                      <button role="menuitem" className="danger-text" onClick={() => (close(), remove(task))}>
                        <Icon name="trash" /> {t.remove}
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

const sortLabels = (t: typeof text.en): Record<TaskSort, string> => ({ recent: t.recent, name: t.alphabetical });

function SortMenu({ value, onChange }: { value: TaskSort; onChange: (value: TaskSort) => void }) {
  const t = useText(text);
  const labels = sortLabels(t);
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
        (Object.keys(labels) as TaskSort[]).map((option) => (
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
    <button type="button" className="back" onClick={() => navigate("/ai/scheduled")}>
      <Icon name="arrowLeft" /> {t.back}
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
  const t = useText(text);
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
  const [defaults, setDefaults] = useState<ChatDefaults>();
  const { models, loading: modelsLoading } = useModels(connectorID || null);
  const defaultModel = (list: Connector[], id: string) => startModel(list, defaults, id);

  useEffect(() => {
    if (!modelsLoading && model && model === defaults?.model && !models.some((m) => m.id === model)) {
      setModel(startModel(connectors ?? [], undefined, connectorID));
    }
  }, [models, modelsLoading]);

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
        setDefaults(defaults);
        const id = defaults.connector_id && connectorList.some((c) => c.id === defaults.connector_id)
          ? defaults.connector_id
          : connectorList[0]?.id ?? "";
        setConnectorID(id);
        setModel(startModel(connectorList, defaults, id));
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
    if (!task || !(await confirmDialog(lang() === "es" ? `¿Eliminar la tarea programada "${task.title}"?` : `Delete scheduled task "${task.title}"?`))) return;
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
      {t.at}
      <input type="time" required value={when.time} onChange={(event) => set({ time: event.target.value })} />
    </label>
  );

  return (
    <div className="stack">
      <BackLink />
      <div className="page-heading">
        <h1>{task ? task.title : t.editNew}</h1>
      </div>
      {connectors?.length === 0 && (
        <ConnectorRequired then={task ? (lang() === "es" ? "elegirlo para esta tarea" : "pick it for this task") : t.create} />
      )}
      {(task || !!connectors?.length) && (
      <form
        className="panel skill-editor"
        onSubmit={submit}
        aria-label={task ? t.editAria(task.title) : t.editNew}
      >
        <div className="skill-fields">
          <label className="grow">
            {t.title}
            <input
              autoFocus
              required
              placeholder={lang() === "es" ? "Resumen semanal de tickets" : "Weekly ticket summary"}
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
            {t.connector}
            <Picker
              label={t.connector}
              empty={connectors?.length === 0 ? t.noConnector : task && !connectorID ? t.connectorRemoved : undefined}
              options={(connectors ?? []).map((c) => ({ id: c.id, name: `${c.name} · ${c.kind}` }))}
              value={connectorID}
              onChange={(id) => {
                setConnectorID(id);
                setModel(defaultModel(connectors ?? [], id));
              }}
            />
          </label>
          <div className="field">
            <span className="label-row">
              {t.model} {modelsLoading && <span className="spinner" role="status" aria-label={t.loadingModels} />}
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
            {t.folder}
            <Picker
              label={t.folder}
              empty={t.scratch}
              options={[
                ...folders.map((f) => ({ id: f.path, name: f.path })),
                ...(folder && !folders.some((f) => f.path === folder) ? [{ id: folder, name: folder }] : []),
              ]}
              value={folder}
              onChange={setFolder}
            />
          </label>
          <label>
            {t.mode}
            <Picker
              label={t.mode}
              options={[
                { id: "ask", name: t.ask },
                { id: "auto", name: t.auto },
              ]}
              value={autoAllow ? "auto" : "ask"}
              onChange={(mode) => setAutoAllow(mode === "auto")}
            />
          </label>
        </div>
        <p className="muted">
          {autoAllow
            ? t.autoDesc
            : t.askDesc}
        </p>
        <Switch checked={thinking} onChange={setThinking} label={t.thinking} />

        <fieldset className="skill-fields">
          <legend>{t.schedule}</legend>
          <label>
            {t.repeat}
            <Picker
              label={t.repeat}
              options={(["once", "hours", "daily", "weekdays", "weekly", "monthly", "cron"] as Repeat[]).map((repeat) => ({ id: repeat, name: t[repeat] }))}
              value={when.repeat}
              onChange={(repeat) => set({ repeat: repeat as Repeat })}
            />
          </label>
          {when.repeat === "once" && (
            <label>
              {t.on}
              <input type="datetime-local" required value={when.at} onChange={(event) => set({ at: event.target.value })} />
            </label>
          )}
          {when.repeat === "hours" && (
            <label>
              {t.everyHoursLabel}
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
            <div role="group" aria-label={t.daysWeek} className="day-picker">
              {t.days.map((name, day) => (
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
              {t.dayMonth}
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
              {t.cronLabel}
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
            <p className="muted">{t.nextRuns} {preview.runs.map(formatRun).join(" · ")}</p>
          )}
          <ErrorText error={preview.error} />
        </div>
        <p className="muted">{t.openOnly}</p>

        <ErrorText error={error} />
        <div className="connector-buttons">
          <Switch checked={enabled} onChange={setEnabled} label={t.enabled} />
          <button
            className="primary"
            disabled={!dirty || !connectorID || (when.repeat === "weekly" && when.days.length === 0)}
          >
            {saved ? t.saved : task ? t.save : t.createTask}
          </button>
          {task && (
            <IconButton type="button" className="danger-text" icon="trash" label={t.deleteTitle(task.title)} onClick={remove} />
          )}
        </div>
      </form>
      )}

      {task && (
        <section className="stack" aria-labelledby="runs-title">
          <h2 id="runs-title">{t.recentRuns}</h2>
          {runs.length === 0 && <p className="muted">{t.noRuns}</p>}
          {runs.length > 0 && (
            <ul className="panel skill-list" aria-label={t.runsAria}>
              {runs.map((run) => (
                <li key={run.id}>
                  <span className="skill-item">
                    <span className="skill-name">
                      {formatRun(run.scheduled_for)}
                      {run.late && (
                        <span className="run-label" title={t.late}>
                          {t.late}
                        </span>
                      )}
                    </span>
                    <span className="skill-description" title={run.status === "skipped" ? text[lang()].skipped : run.error}>
                      {statusLabel(run.status)}
                      {run.status === "skipped" && ` · ${text[lang()].skipped}`}
                      {run.error && ` · ${run.error}`}
                    </span>
                  </span>
                  {run.chat_id && (
                    <button type="button" onClick={() => navigate(`/ai/chats/${run.chat_id}`)}>
                      <Icon name="chat" /> {t.openChat}
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
