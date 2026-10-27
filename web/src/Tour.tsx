import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { api, type Board, type Connector, type SearchTicket, type Ticket } from "./api";
import { navigate } from "./App";
import { lang, useText } from "./i18n";

// Asks App to start the feature tour.
export const startTour = () => dispatchEvent(new Event("start-tour"));

const mac = /Mac|iPhone|iPad/.test(navigator.platform);
const key = (k: string, other = k) => (mac ? `⌘${k}` : `Ctrl+${other}`);

const text = {
  en: {
    back: "Back", next: "Next", done: "Done", skip: "Skip tour", step: (n: number, total: number) => `${n} of ${total}`, loading: "Getting the tour ready…",
    chapters: { basics: "Basics", ai: "AI", power: "Power tools" },
    needsConnector: "This needs a connector. Add one to try it.", goConnectors: "Go to Connectors",
    steps: {
      intro: ["Welcome to Patio", "Patio keeps your work on boards of tickets, and AI chats that can read and update them. This tour walks you through the main pieces in about two minutes."],
      boards: ["Boards", "All your boards live here. Each board also gets a shortcut lower in the sidebar; drag those to reorder."],
      noBoard: ["Create a board first", "Tickets live on boards. Go to Boards and press Create board, then replay this tour from Settings to see tickets up close."],
      columns: ["Columns", "A board is a set of columns, one per status. Change, add or reorder columns in Board settings."],
      newTicket: ["New ticket", "Add work here: a title, an optional description in Markdown, a type and a due date."],
      card: ["Tickets", "We added a sample ticket for you. Drag cards between columns to change their status, and click one to open it."],
      due: ["Due date", "Set a due date and the ticket shows up on the Calendar, with a badge when it's close or late."],
      links: ["Links", "Link related tickets: blocks, relates to, or duplicates. Links show on both tickets."],
      comments: ["Comments", "Keep notes and progress here. Comments support Markdown, and AI chats can add them too."],
      ticketChat: ["Chat about a ticket", "Start an AI chat that already knows this ticket: it can plan, update the description and comment for you."],
      tickets: ["All tickets", "Every ticket from every board in one list, to search, sort and filter."],
      calendar: ["Calendar", "Due dates, events, reminders and AI tasks in one place. Use the filter to choose what shows."],
      connectors: ["Connectors", "The AI CLIs (Claude Code, Codex, Copilot, opencode) and local models Patio chats through. Add or sign in here anytime."],
      chats: ["Chats", `${key("N")} starts a chat. Type # to mention a ticket. Type @, a bot's name and a task to open a chat run by that bot; the reply links it. Pick a model next to the message box, and approve file changes or commands when the AI asks.`],
      pins: ["Pins and the dock", `Pin a chat to keep it handy: ${key("⇧N", "Shift+N")} starts a pinned chat and ${key("J")} opens the side dock on any page. Group pinned chats into tabs.`],
      bots: ["Bots", "Bots are AI personas with their own instructions, connector and model, like a developer or a reviewer. Martin is the first one; pick the default bot for new chats in Settings."],
      skills: ["Skills and memories", "Skills are reusable instructions chats load for a task. Memories are facts and preferences the AI saves when you correct it, so you don't repeat yourself."],
      mcp: ["MCP servers", "Give chats more tools, like Gmail, Slack or Jira, by adding MCP servers."],
      monitors: ["Monitors", "Run an MCP tool on a schedule and get notified when its result changes."],
      scheduled: ["Scheduled tasks", "Run a prompt on a schedule, like a daily summary of your boards."],
      patioMcp: ["Use Patio from other AI apps", "Patio is an MCP server too. Copy this setup into Claude Desktop, Claude Code or another client to manage your boards from there."],
      foot: ["Settings and notifications", `Settings, theme and notifications live here. Press ${key("/")} anytime for every shortcut, and replay this tour from Settings.`],
    },
  },
  es: {
    back: "Atrás", next: "Siguiente", done: "Listo", skip: "Omitir recorrido", step: (n: number, total: number) => `${n} de ${total}`, loading: "Preparando el recorrido…",
    chapters: { basics: "Básico", ai: "IA", power: "Herramientas avanzadas" },
    needsConnector: "Esto necesita un conector. Agrega uno para probarlo.", goConnectors: "Ir a Conectores",
    steps: {
      intro: ["Bienvenido a Patio", "Patio organiza tu trabajo en tableros de tickets, con chats de IA que pueden leerlos y actualizarlos. Este recorrido te muestra las piezas principales en unos dos minutos."],
      boards: ["Tableros", "Aquí están todos tus tableros. Cada tablero tiene además un acceso más abajo en la barra lateral; arrástralos para reordenar."],
      noBoard: ["Primero crea un tablero", "Los tickets viven en tableros. Ve a Tableros y presiona Crear tablero; luego repite este recorrido desde Ajustes para ver los tickets de cerca."],
      columns: ["Columnas", "Un tablero es un conjunto de columnas, una por estado. Cambia, agrega o reordena columnas en la configuración del tablero."],
      newTicket: ["Nuevo ticket", "Agrega trabajo aquí: un título, una descripción opcional en Markdown, un tipo y una fecha de vencimiento."],
      card: ["Tickets", "Agregamos un ticket de ejemplo. Arrastra las tarjetas entre columnas para cambiar su estado y haz clic en una para abrirla."],
      due: ["Fecha de vencimiento", "Pon una fecha de vencimiento y el ticket aparece en el Calendario, con una insignia cuando está cerca o atrasado."],
      links: ["Enlaces", "Enlaza tickets relacionados: bloquea, se relaciona o duplica. Los enlaces se ven en ambos tickets."],
      comments: ["Comentarios", "Guarda notas y avances aquí. Los comentarios admiten Markdown y los chats de IA también pueden agregarlos."],
      ticketChat: ["Chatear sobre un ticket", "Inicia un chat de IA que ya conoce este ticket: puede planificar, actualizar la descripción y comentar por ti."],
      tickets: ["Todos los tickets", "Todos los tickets de todos los tableros en una lista, para buscar, ordenar y filtrar."],
      calendar: ["Calendario", "Fechas de vencimiento, eventos, recordatorios y tareas de IA en un solo lugar. Usa el filtro para elegir qué se muestra."],
      connectors: ["Conectores", "Las CLI de IA (Claude Code, Codex, Copilot, opencode) y modelos locales con los que chatea Patio. Agrégalos o inicia sesión aquí cuando quieras."],
      chats: ["Chats", `${key("N")} inicia un chat. Escribe # para mencionar un ticket. Escribe @, el nombre de un bot y una tarea para abrir un chat atendido por ese bot; la respuesta lo enlaza. Elige un modelo junto al cuadro de mensaje y aprueba cambios de archivos o comandos cuando la IA lo pida.`],
      pins: ["Chats fijados y el panel", `Fija un chat para tenerlo a mano: ${key("⇧N", "Shift+N")} inicia un chat fijado y ${key("J")} abre el panel lateral en cualquier página. Agrupa los chats fijados en pestañas.`],
      bots: ["Bots", "Los bots son personalidades de IA con sus propias instrucciones, conector y modelo, como un desarrollador o un revisor. Martin es el primero; elige el bot predeterminado para chats nuevos en Ajustes."],
      skills: ["Habilidades y memorias", "Las habilidades son instrucciones reutilizables que los chats cargan para una tarea. Las memorias son datos y preferencias que la IA guarda cuando la corriges, para que no te repitas."],
      mcp: ["Servidores MCP", "Da más herramientas a los chats, como Gmail, Slack o Jira, agregando servidores MCP."],
      monitors: ["Monitores", "Ejecuta una herramienta MCP según un horario y recibe un aviso cuando su resultado cambia."],
      scheduled: ["Tareas programadas", "Ejecuta un prompt según un horario, como un resumen diario de tus tableros."],
      patioMcp: ["Usa Patio desde otras apps de IA", "Patio también es un servidor MCP. Copia esta configuración en Claude Desktop, Claude Code u otro cliente para manejar tus tableros desde ahí."],
      foot: ["Ajustes y avisos", `Aquí están los ajustes, el tema y las notificaciones. Presiona ${key("/")} cuando quieras para ver todos los atajos y repite este recorrido desde Ajustes.`],
    },
  },
};

const sample = {
  en: {
    title: "Welcome to Patio",
    description: "A sample ticket to play with.\n\n- [ ] Drag this ticket to another column\n- [ ] Set a due date\n- [ ] Add a comment\n- [ ] Start a chat about this ticket\n- [ ] Delete this ticket when you're done",
  },
  es: {
    title: "Bienvenido a Patio",
    description: "Un ticket de ejemplo para practicar.\n\n- [ ] Arrastra este ticket a otra columna\n- [ ] Pon una fecha de vencimiento\n- [ ] Agrega un comentario\n- [ ] Inicia un chat sobre este ticket\n- [ ] Elimina este ticket cuando termines",
  },
};

type StepId = keyof typeof text.en.steps;
type Chapter = keyof typeof text.en.chapters;
type Context = { board?: Board; ticket?: Ticket; connector: boolean };
type Stop = { id: StepId; chapter: Chapter; target?: string | string[]; path?: string; ai?: boolean };

const nav = (id: string) => `[data-tour="${id}"]`;

function plan({ board, ticket }: Context): Stop[] {
  const ticketPath = ticket && `/tickets/${ticket.code || ticket.id}`;
  const boardStops: Stop[] = board
    ? [
        { id: "columns", chapter: "basics", path: `/boards/${board.id}`, target: ".kanban" },
        { id: "newTicket", chapter: "basics", path: `/boards/${board.id}`, target: nav("new-ticket") },
        ...(ticket
          ? ([
              { id: "card", chapter: "basics", path: `/boards/${board.id}`, target: `.ticket-card[data-ticket="${ticket.id}"]` },
              { id: "due", chapter: "basics", path: ticketPath, target: nav("due") },
              { id: "links", chapter: "basics", path: ticketPath, target: ".ticket-link-section" },
              { id: "comments", chapter: "basics", path: ticketPath, target: "section.comments" },
              { id: "ticketChat", chapter: "basics", path: ticketPath, target: nav("ticket-chat"), ai: true },
            ] satisfies Stop[])
          : []),
      ]
    : [{ id: "noBoard", chapter: "basics", path: "/" }];
  return [
    { id: "intro", chapter: "basics" },
    { id: "boards", chapter: "basics", path: "/", target: nav("boards") },
    ...boardStops,
    { id: "tickets", chapter: "basics", target: nav("tickets") },
    { id: "calendar", chapter: "basics", target: nav("calendar") },
    { id: "connectors", chapter: "ai", target: nav("connectors") },
    { id: "chats", chapter: "ai", path: "/ai/chats", target: ["form.composer", nav("new-chat"), ".skill-empty"], ai: true },
    { id: "pins", chapter: "ai", path: "/ai/chats", target: [nav("pin"), nav("chats")], ai: true },
    { id: "bots", chapter: "ai", target: nav("bots") },
    { id: "skills", chapter: "ai", path: "/ai/skills", target: '.tabs[role="tablist"]' },
    { id: "mcp", chapter: "ai", target: nav("mcp") },
    { id: "monitors", chapter: "power", target: nav("monitors") },
    { id: "scheduled", chapter: "power", target: nav("scheduled"), ai: true },
    { id: "patioMcp", chapter: "power", path: "/settings", target: [".mcp-config", ".mcp-status"] },
    { id: "foot", chapter: "power", target: nav("foot") },
  ];
}

// The first ticket on the first board, or a new sample ticket there.
async function prepare(): Promise<Context> {
  const [boards, connectors] = await Promise.all([api.get<Board[]>("/api/boards"), api.get<Connector[]>("/api/connectors")]);
  const board = boards[0];
  if (!board) return { connector: connectors.length > 0 };
  const tickets = await api.get<SearchTicket[]>(`/api/tickets/search?board_id=${encodeURIComponent(board.id)}`);
  const ticket = tickets[0] ?? (await api.post<Ticket>(`/api/boards/${board.id}/tickets`, sample[lang()]));
  return { board, ticket, connector: connectors.length > 0 };
}

// The first selector that matches, in order of preference.
const find = (selectors: string[]) => selectors.map((selector) => document.querySelector(selector)).find(Boolean) ?? null;

function waitFor(selectors: string[], timeout = 1000): Promise<Element | null> {
  return new Promise((resolve) => {
    const started = Date.now();
    const poll = () => {
      const element = find(selectors);
      if (element || Date.now() - started > timeout) return resolve(element);
      setTimeout(poll, 100);
    };
    poll();
  });
}

const cardWidth = 340;
const cardHeight = 240;
const gap = 16;

// Right of the target, else left, below, or above; else pinned to the bottom right.
function place(rect: DOMRect) {
  const top = Math.min(Math.max(rect.top - 8, 12), innerHeight - cardHeight);
  const left = Math.min(Math.max(rect.left, 12), innerWidth - cardWidth - 12);
  if (innerWidth - rect.right > cardWidth + gap * 2) return { top, left: rect.right + gap };
  if (rect.left > cardWidth + gap * 2) return { top, left: rect.left - cardWidth - gap };
  if (innerHeight - rect.bottom > cardHeight + gap) return { top: rect.bottom + gap, left };
  if (rect.top > cardHeight + gap) return { top: rect.top - cardHeight - gap, left };
  return { top: innerHeight - cardHeight - gap, left: innerWidth - cardWidth - gap };
}

export default function Tour({ onClose }: { onClose: () => void }) {
  const t = useText(text);
  const [context, setContext] = useState<Context>();
  const [index, setIndex] = useState(0);
  const [rect, setRect] = useState<DOMRect>();
  const [ready, setReady] = useState(false);
  const dialog = useRef<HTMLDialogElement>(null);
  const nextButton = useRef<HTMLButtonElement>(null);
  const start = useRef(location.pathname + location.search);
  const prepared = useRef(false);
  const stops = context ? plan(context) : [];
  const stop = stops[index];
  const last = index === stops.length - 1;

  function close(to = start.current) {
    if (location.pathname + location.search !== to) navigate(to);
    onClose();
  }

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
    // StrictMode runs effects twice; preparing twice could create two sample tickets.
    if (prepared.current) return;
    prepared.current = true;
    prepare()
      .then(setContext)
      .catch(() => setContext({ connector: false }));
  }, []);

  useLayoutEffect(() => {
    if (!stop) return;
    let cancelled = false;
    let settled = false;
    const selectors = stop.target ? [stop.target].flat() : [];
    // Re-query each time: pages can re-render and replace the target node.
    const measure = () => settled && !cancelled && setRect(find(selectors)?.getBoundingClientRect());
    setReady(false);
    if (stop.path && location.pathname !== stop.path) navigate(stop.path);
    (selectors.length ? waitFor(selectors) : Promise.resolve(null)).then(() => {
      if (cancelled) return;
      // Pages still loading data shift the target, so scroll once its position stops changing.
      let top = NaN;
      let tries = 0;
      const settle = () => {
        if (cancelled) return;
        const next = find(selectors)?.getBoundingClientRect().top ?? 0;
        if (next !== top && tries++ < 10) {
          top = next;
          return setTimeout(settle, 150);
        }
        find(selectors)?.scrollIntoView({ block: "center" });
        settled = true;
        measure();
        setReady(true);
        nextButton.current?.focus();
      };
      settle();
    });
    addEventListener("resize", measure);
    addEventListener("scroll", measure, true);
    return () => {
      cancelled = true;
      removeEventListener("resize", measure);
      removeEventListener("scroll", measure, true);
    };
  }, [stop?.id, stop?.path]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "ArrowRight" && !last) setIndex(index + 1);
      if (event.key === "ArrowLeft" && index > 0) setIndex(index - 1);
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [index, last]);

  const pad = 4;
  const spot = ready && rect;
  // While the next stop settles, the hidden card keeps its last position instead of jumping to the center.
  const shown = useRef<DOMRect | undefined>(undefined);
  if (ready || !stop) shown.current = rect;
  const at = shown.current;
  const [title, body] = stop ? t.steps[stop.id] : [t.loading, ""];

  return (
    <dialog
      ref={dialog}
      className="tour"
      aria-labelledby="tour-title"
      aria-describedby="tour-body"
      onCancel={(event) => {
        event.preventDefault();
        close();
      }}
    >
      {spot ? (
        <div
          key="spot"
          className="tour-spot"
          style={{ top: rect.top - pad, left: rect.left - pad, width: rect.width + pad * 2, height: rect.height + pad * 2 }}
        />
      ) : (
        <div key="backdrop" className="tour-backdrop" />
      )}
      <section
        className={`panel tour-card${at ? "" : " centered"}${stop && !ready ? " pending" : ""}`}
        style={at ? place(at) : undefined}
        aria-live="polite"
      >
        {stop && (
          <p className="eyebrow">
            {t.chapters[stop.chapter]} · {t.step(index + 1, stops.length)}
          </p>
        )}
        <h2 id="tour-title">{title}</h2>
        {body && <p id="tour-body">{body}</p>}
        {stop?.ai && context && !context.connector && (
          <div className="callout tour-nudge">
            {t.needsConnector}
            <button type="button" onClick={() => close("/ai/connectors")}>
              {t.goConnectors}
            </button>
          </div>
        )}
        <div className="modal-actions">
          <button type="button" className="ghost" onClick={() => close()}>
            {t.skip}
          </button>
          {index > 0 && (
            <button type="button" onClick={() => setIndex(index - 1)}>
              {t.back}
            </button>
          )}
          <button
            ref={nextButton}
            type="button"
            className="primary"
            disabled={!stop}
            onClick={() => (last ? close() : setIndex(index + 1))}
          >
            {last ? t.done : t.next}
          </button>
        </div>
      </section>
    </dialog>
  );
}
