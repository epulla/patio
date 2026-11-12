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
    needsConnector: "This needs a connector. Add one to try it.", goConnectors: "Go to Connections",
    steps: {
      intro: ["Welcome to Patio", "Patio keeps your work on boards of tickets, and AI chats that can read and update them. This tour walks you through the main pieces in about two minutes."],
      boards: ["Boards", "All your boards live here. Each board also gets a shortcut lower in the sidebar; drag those to reorder. Drag the sidebar's right edge to resize it; double-click the edge to reset."],
      noBoard: ["Create a board first", "Tickets live on boards. Go to Boards and press Create board, then replay this tour from Settings to see tickets up close."],
      columns: ["Columns", "A board is a set of columns, one per status. Change, add or reorder columns in Board settings, and choose what each one counts as on the Dashboard."],
      newTicket: ["New ticket", "Add work here: a title, an optional description in Markdown, a type and a due date. Write @, a bot's name and a task in the description to have that bot work on it."],
      card: ["Tickets", "We added a sample ticket for you. Drag cards between columns to change their status, and click one to open it."],
      due: ["Due date", "Set a due date and the ticket shows up on the Calendar, with a badge when it's close or late."],
      links: ["Links", "Link related tickets: blocks, relates to, or duplicates. Links show on both tickets."],
      ticketFiles: ["Images and files", "Paste or drop screenshots and files into a description or comment. Images show inline, clicking a file shows it in its folder, and bots get the file paths. Attachments lists them all."],
      comments: ["Comments", "Keep notes and progress here. Comments support Markdown, and AI chats can add them too. @, a bot's name and a task opens a chat for that bot; Patio links it and the bot posts its result here."],
      activityOrder: ["Newest or oldest first", "Comments, History and Transitions show the newest first. Use Order to flip all three; Patio remembers your choice."],
      ticketChat: ["Chat about a ticket", "Start an AI chat that already knows this ticket: it can plan, update the description and comment for you."],
      dashboard: ["Dashboard", "Your work at a glance: tickets done and created against the previous period, done per day, overdue and due-soon tickets, and what's coming up. Pick a range and a board."],
      tickets: ["All tickets", "Every ticket from every board in one list, to search, sort and filter."],
      calendar: ["Calendar", "Due dates, events, reminders and AI tasks in one place. Use the filter to choose what shows, and drag a chip to another day to move it."],
      connections: ["Connections", "Everything Patio connects to lives here, in three tabs: AI, Tools (MCP) and Channels."],
      connectionsAi: ["AI connectors", "The CLIs (Claude Code, Codex, Copilot, opencode) and local models Patio chats through. Add at least one to use chats and bots."],
      connectionsTools: ["Tools (MCP)", "MCP servers give chats more tools, like Gmail, Slack or Jira. Sign in once and every chat can use them. If a server gets stuck or you update it, press Reload at the top for all servers, or on a card to restart only the chats that use that server. Busy chats reconnect after their current reply."],
      connectionsChannels: ["Channels", "Chat with Patio from your phone. Connect a Telegram bot, allow your account, and messages go into normal chats with your default bot. Approvals and questions come with buttons, and /continue picks up any recent chat."],
      chats: ["Chats", `${key("N")} starts a chat. Type # to mention a ticket. Type @, a bot's name and a task to open a chat run by that bot; the reply links it. Pick a model next to the message box, and approve file changes or commands when the AI asks. Hover a ticket, board or chat link to preview it.`],
      attachments: ["Attachments", "Attach files with the paperclip, drag and drop, or paste. Hover an image to preview it and click to enlarge it; Esc or a click outside closes it, and Show in folder opens the file."],
      chatLinks: ["Chat links", "Copy a chat's link from its header or the ⋯ menu on its row (right-click works too), type [[ in a message to pick a chat, or drag a chat from the list into the message box. Links open the chat."],
      chatTree: ["Handoff chats", "Chats a bot hands work to sit under the chat that started them. Click the arrow before the title to show or hide them; a parent shows the running dot while any of its chats work. A yellow ⏸ means a chat paused its queued messages (after Stop, an error or too many bot hand-offs); open it and press Send now to continue. The chat that sent a paused message shows a banner too, with Open and Send now. Bots can also steer a running reply, and a bot's answer to a chat waiting on it gets past the limit. Click any queued message to edit it before it goes. Drag the list's right edge to widen it; double-click the edge to reset."],
      aiTitles: ["AI chat titles", "After a new chat's first reply, AI gives it a short title. Rename a chat to keep your own title. Turn this off or pick the connector and model it uses here."],
      pins: ["Pins and the dock", `Pin a chat to keep it handy: ${key("⇧N", "Shift+N")} starts a pinned chat and ${key("J")} opens the side dock on any page. Group pinned chats into tabs, and drag a chat from the list onto a group to pin it there.`],
      bots: ["Bots", "Bots are AI personas with their own instructions, connector and model, like a developer or a reviewer. Martin is the first one; pick the default bot for new chats in Settings, or choose another bot when you start a chat."],
      training: ["Train your bots", "Give each bot its own skills and memories here; only that bot sees them. Skills and memories you don't limit are shared by all bots. You can also pick bots in a skill's editor under Available to."],
      workflows: ["Workflows", "Draw how bots hand work to each other, like Martin → Billy → Sammy, then run it with a task, or start it in a chat with the workflow button. Steps that wait for you hand off when you say \"go\"; the bot reminds you. Each bot works in its own chat; View flow shows the whole run live."],
      skills: ["Skills and memories", "Skills are reusable instructions chats load for a task. Memories are facts and preferences the AI saves when you correct it, so you don't repeat yourself."],
      monitors: ["Monitors", "Run an MCP tool on a schedule and get notified when its result changes."],
      scheduled: ["Scheduled tasks", "Run a prompt on a schedule, like a daily summary of your boards. The play button runs a task right away. A gray clock and \"late\" on a chat mean it ran late because Patio was closed at the scheduled time. Closing the window keeps Patio running (in the Dock on macOS, minimized elsewhere) so tasks, monitors and Telegram keep working; change this in Settings › Background."],
      patioMcp: ["Use Patio from other AI apps", "Patio is an MCP server too. Copy this setup into Claude Desktop, Claude Code or another client to manage your boards from there."],
      foot: ["Settings and notifications", `Settings, theme and notifications live here. A dot on the gear and a banner in Settings mean a new Patio version is out; after you update, its release notes open once, and the sparkles button next to the version in Settings shows them again. If your computer blocks desktop alerts, the bell tells you how to turn them on. Long dropdowns have a search box: start typing to filter them. Press ${key("/")} anytime for every shortcut, and replay this tour from Settings.`],
    },
  },
  es: {
    back: "Atrás", next: "Siguiente", done: "Listo", skip: "Omitir recorrido", step: (n: number, total: number) => `${n} de ${total}`, loading: "Preparando el recorrido…",
    chapters: { basics: "Básico", ai: "IA", power: "Herramientas avanzadas" },
    needsConnector: "Esto necesita un conector. Agrega uno para probarlo.", goConnectors: "Ir a Conexiones",
    steps: {
      intro: ["Bienvenido a Patio", "Patio organiza tu trabajo en tableros de tickets, con chats de IA que pueden leerlos y actualizarlos. Este recorrido te muestra las piezas principales en unos dos minutos."],
      boards: ["Tableros", "Aquí están todos tus tableros. Cada tablero tiene además un acceso más abajo en la barra lateral; arrástralos para reordenar. Arrastra el borde derecho de la barra lateral para cambiar su ancho; haz doble clic en el borde para restablecerlo."],
      noBoard: ["Primero crea un tablero", "Los tickets viven en tableros. Ve a Tableros y presiona Crear tablero; luego repite este recorrido desde Ajustes para ver los tickets de cerca."],
      columns: ["Columnas", "Un tablero es un conjunto de columnas, una por estado. Cambia, agrega o reordena columnas en la configuración del tablero, y elige qué cuenta cada una en el Panel."],
      newTicket: ["Nuevo ticket", "Agrega trabajo aquí: un título, una descripción opcional en Markdown, un tipo y una fecha de vencimiento. Escribe @, el nombre de un bot y una tarea en la descripción para que ese bot trabaje en ella."],
      card: ["Tickets", "Agregamos un ticket de ejemplo. Arrastra las tarjetas entre columnas para cambiar su estado y haz clic en una para abrirla."],
      due: ["Fecha de vencimiento", "Pon una fecha de vencimiento y el ticket aparece en el Calendario, con una insignia cuando está cerca o atrasado."],
      links: ["Enlaces", "Enlaza tickets relacionados: bloquea, se relaciona o duplica. Los enlaces se ven en ambos tickets."],
      ticketFiles: ["Imágenes y archivos", "Pega o suelta capturas y archivos en una descripción o comentario. Las imágenes se ven en línea, al hacer clic en un archivo se muestra en su carpeta y los bots reciben sus rutas. Adjuntos los lista todos."],
      comments: ["Comentarios", "Guarda notas y avances aquí. Los comentarios admiten Markdown y los chats de IA también pueden agregarlos. @, el nombre de un bot y una tarea abre un chat para ese bot; Patio lo enlaza y el bot publica aquí su resultado."],
      activityOrder: ["Recientes o antiguos primero", "Comentarios, Historial y Transiciones muestran primero lo más reciente. Usa Orden para invertir los tres; Patio recuerda tu elección."],
      ticketChat: ["Chatear sobre un ticket", "Inicia un chat de IA que ya conoce este ticket: puede planificar, actualizar la descripción y comentar por ti."],
      dashboard: ["Panel", "Tu trabajo de un vistazo: tickets hechos y creados frente al periodo anterior, hechos por día, tickets vencidos o por vencer y lo que viene. Elige un periodo y un tablero."],
      tickets: ["Todos los tickets", "Todos los tickets de todos los tableros en una lista, para buscar, ordenar y filtrar."],
      calendar: ["Calendario", "Fechas de vencimiento, eventos, recordatorios y tareas de IA en un solo lugar. Usa el filtro para elegir qué se muestra y arrastra un elemento a otro día para moverlo."],
      connections: ["Conexiones", "Aquí está todo lo que Patio conecta, en tres pestañas: IA, Herramientas (MCP) y Canales."],
      connectionsAi: ["Conectores de IA", "Las CLI (Claude Code, Codex, Copilot, opencode) y modelos locales con los que chatea Patio. Agrega al menos uno para usar chats y bots."],
      connectionsTools: ["Herramientas (MCP)", "Los servidores MCP dan más herramientas a los chats, como Gmail, Slack o Jira. Inicia sesión una vez y todos los chats pueden usarlas. Si un servidor se traba o lo actualizas, presiona Recargar arriba para todos los servidores, o en una tarjeta para reiniciar solo los chats que usan ese servidor. Los chats ocupados se reconectan después de su respuesta actual."],
      connectionsChannels: ["Canales", "Chatea con Patio desde tu teléfono. Conecta un bot de Telegram, permite tu cuenta y los mensajes llegan a chats normales con tu bot por defecto. Las aprobaciones y preguntas llegan con botones, y /continue retoma cualquier chat reciente."],
      chats: ["Chats", `${key("N")} inicia un chat. Escribe # para mencionar un ticket. Escribe @, el nombre de un bot y una tarea para abrir un chat atendido por ese bot; la respuesta lo enlaza. Elige un modelo junto al cuadro de mensaje y aprueba cambios de archivos o comandos cuando la IA lo pida. Pasa el cursor sobre un enlace a un ticket, tablero o chat para verlo.`],
      attachments: ["Adjuntos", "Adjunta archivos con el clip, arrastrándolos o pegándolos. Pasa el cursor sobre una imagen para verla y haz clic para ampliarla; Esc o un clic fuera la cierra, y Mostrar en carpeta abre el archivo."],
      chatLinks: ["Enlaces a chats", "Copia el enlace de un chat desde su encabezado o el menú ⋯ de su fila (también con clic derecho), escribe [[ en un mensaje para elegir un chat, o arrastra un chat de la lista al cuadro de mensaje. Los enlaces abren el chat."],
      chatTree: ["Chats delegados", "Los chats a los que un bot delega trabajo aparecen bajo el chat que los inició. Haz clic en la flecha antes del título para mostrarlos u ocultarlos; el chat padre muestra el punto de actividad mientras alguno trabaja. Un ⏸ amarillo indica que un chat pausó sus mensajes en cola (tras Detener, un error o demasiados traspasos entre bots); ábrelo y presiona Enviar ahora para continuar. El chat que envió un mensaje en pausa también muestra un aviso, con Abrir y Enviar ahora. Los bots también pueden redirigir una respuesta en curso, y la respuesta de un bot a un chat que la espera pasa el límite. Haz clic en un mensaje en cola para editarlo antes de enviarlo. Arrastra el borde derecho de la lista para ensancharla; haz doble clic en el borde para restablecerla."],
      aiTitles: ["Títulos de chat con IA", "Tras la primera respuesta de un chat nuevo, la IA le da un título corto. Renombra un chat para conservar tu propio título. Desactívalo o elige aquí el conector y el modelo que usa."],
      pins: ["Chats fijados y el panel", `Fija un chat para tenerlo a mano: ${key("⇧N", "Shift+N")} inicia un chat fijado y ${key("J")} abre el panel lateral en cualquier página. Agrupa los chats fijados en pestañas y arrastra un chat de la lista a un grupo para fijarlo ahí.`],
      bots: ["Bots", "Los bots son personalidades de IA con sus propias instrucciones, conector y modelo, como un desarrollador o un revisor. Martin es el primero; elige el bot predeterminado para chats nuevos en Ajustes, o elige otro bot al iniciar un chat."],
      training: ["Entrena a tus bots", "Dale a cada bot sus propias habilidades y recuerdos aquí; solo ese bot los ve. Las habilidades y recuerdos que no limites los comparten todos los bots. También puedes elegir bots en el editor de una habilidad, en Disponible para."],
      workflows: ["Flujos", "Dibuja cómo se pasan el trabajo los bots, como Martin → Billy → Sammy, y ejecútalo con una tarea, o inícialo en un chat con el botón de flujos. Los pasos que esperan tu señal traspasan cuando dices \"sigue\"; el bot te lo recuerda. Cada bot trabaja en su propio chat; Ver flujo muestra toda la ejecución en vivo."],
      skills: ["Habilidades y memorias", "Las habilidades son instrucciones reutilizables que los chats cargan para una tarea. Las memorias son datos y preferencias que la IA guarda cuando la corriges, para que no te repitas."],
      monitors: ["Monitores", "Ejecuta una herramienta MCP según un horario y recibe un aviso cuando su resultado cambia."],
      scheduled: ["Tareas programadas", "Ejecuta un prompt según un horario, como un resumen diario de tus tableros. El botón de reproducir ejecuta una tarea al instante. Un reloj gris y \"tarde\" en un chat indican que se ejecutó tarde porque Patio estaba cerrado a la hora programada. Cerrar la ventana mantiene Patio en ejecución (en el Dock en macOS, minimizado en otros sistemas) para que las tareas, los monitores y Telegram sigan funcionando; cámbialo en Ajustes › Segundo plano."],
      patioMcp: ["Usa Patio desde otras apps de IA", "Patio también es un servidor MCP. Copia esta configuración en Claude Desktop, Claude Code u otro cliente para manejar tus tableros desde ahí."],
      foot: ["Ajustes y avisos", `Aquí están los ajustes, el tema y las notificaciones. Un punto en el engranaje y un aviso en Ajustes indican que hay una nueva versión de Patio; al actualizar, sus novedades se abren una vez, y el botón de destellos junto a la versión en Ajustes las vuelve a mostrar. Si tu equipo bloquea los avisos de escritorio, la campana te dice cómo activarlos. Las listas desplegables largas tienen buscador: empieza a escribir para filtrarlas. Presiona ${key("/")} cuando quieras para ver todos los atajos y repite este recorrido desde Ajustes.`],
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
              { id: "ticketFiles", chapter: "basics", path: ticketPath, target: nav("ticket-description") },
              { id: "comments", chapter: "basics", path: ticketPath, target: "section.comments" },
              { id: "activityOrder", chapter: "basics", path: ticketPath, target: nav("activity-order") },
              { id: "ticketChat", chapter: "basics", path: ticketPath, target: nav("ticket-chat"), ai: true },
            ] satisfies Stop[])
          : []),
      ]
    : [{ id: "noBoard", chapter: "basics", path: "/" }];
  return [
    { id: "intro", chapter: "basics" },
    { id: "dashboard", chapter: "basics", path: "/dashboard", target: nav("dashboard") },
    { id: "boards", chapter: "basics", path: "/", target: nav("boards") },
    ...boardStops,
    { id: "tickets", chapter: "basics", target: nav("tickets") },
    { id: "calendar", chapter: "basics", target: nav("calendar") },
    { id: "connections", chapter: "ai", target: nav("connections") },
    { id: "connectionsAi", chapter: "ai", path: "/ai/connectors", target: nav("tab-connectors") },
    { id: "connectionsTools", chapter: "ai", path: "/ai/mcp", target: nav("tab-mcp") },
    { id: "connectionsChannels", chapter: "ai", path: "/ai/channels", target: nav("tab-channels") },
    { id: "chats", chapter: "ai", path: "/ai/chats", target: ["form.composer", nav("new-chat"), ".skill-empty"], ai: true },
    { id: "attachments", chapter: "ai", path: "/ai/chats", target: [nav("attach"), nav("new-chat")], ai: true },
    { id: "chatLinks", chapter: "ai", path: "/ai/chats", target: [nav("chat-row"), nav("chats")], ai: true },
    { id: "chatTree", chapter: "ai", path: "/ai/chats", target: [nav("tree-toggle"), nav("chats")], ai: true },
    { id: "pins", chapter: "ai", path: "/ai/chats", target: [nav("pin"), nav("chats")], ai: true },
    { id: "bots", chapter: "ai", target: nav("bots") },
    { id: "training", chapter: "ai", path: "/ai/bots/ai", target: nav("bot-training") },
    { id: "workflows", chapter: "ai", path: "/ai/workflows", target: '.tabs[role="tablist"]' },
    { id: "skills", chapter: "ai", path: "/ai/skills", target: '.tabs[role="tablist"]' },
    { id: "aiTitles", chapter: "ai", path: "/settings", target: nav("ai-titles"), ai: true },
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
const gap = 16;

// Right of the target, else left, below, or above; else pinned to the bottom right.
// cardHeight is the rendered card's height, since long steps don't fit a fixed guess.
function place(rect: DOMRect, cardHeight: number) {
  const top = Math.max(Math.min(rect.top - 8, innerHeight - cardHeight - 12), 12);
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
  const card = useRef<HTMLElement>(null);
  const [cardHeight, setCardHeight] = useState(240);
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

  useLayoutEffect(() => {
    const height = card.current?.offsetHeight;
    if (height && height !== cardHeight) setCardHeight(height);
  });

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
        ref={card}
        className={`panel tour-card${at ? "" : " centered"}${stop && !ready ? " pending" : ""}`}
        style={at ? place(at, cardHeight) : undefined}
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
          <button type="button" className="tour-skip" onClick={() => close()}>
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
