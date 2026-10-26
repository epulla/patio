import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useText } from "./i18n";

// Asks App to start the feature tour.
export const startTour = () => dispatchEvent(new Event("start-tour"));

const mac = /Mac|iPhone|iPad/.test(navigator.platform);
const key = (k: string, other = k) => (mac ? `⌘${k}` : `Ctrl+${other}`);

const text = {
  en: {
    tour: "Patio tour", back: "Back", next: "Next", done: "Done", skip: "Skip tour", step: (n: number, total: number) => `${n} of ${total}`,
    steps: {
      boards: ["Boards", "All your boards. Each board also gets a shortcut lower in the sidebar; drag those to reorder."],
      tickets: ["Tickets", "Every ticket across all boards in one list, to search and filter."],
      calendar: ["Calendar", "Due dates, events and reminders in one place."],
      monitors: ["Monitors", "Run an MCP tool on a schedule and get notified when its result changes."],
      connectors: ["Connectors", "The AI CLIs and local models Patio chats through. Add or sign in here anytime."],
      chats: ["Chats", `Chat with AI that can read and update your boards. ${key("N")} starts a chat, ${key("⇧N", "Shift+N")} pins one to the side dock, and ${key("J")} toggles the dock.`],
      skills: ["Skills", "Reusable instructions and saved memories that chats load when a task needs them."],
      mcp: ["MCP", "Give chats more tools with MCP servers. Settings also shows how to plug Patio itself into other AI apps."],
      scheduled: ["Scheduled tasks", "Run a prompt on a schedule, like a daily summary."],
      foot: ["Settings and notifications", `Settings, theme and notifications live here. Press ${key("/")} anytime for every shortcut, and replay this tour from Settings.`],
    },
  },
  es: {
    tour: "Recorrido por Patio", back: "Atrás", next: "Siguiente", done: "Listo", skip: "Omitir recorrido", step: (n: number, total: number) => `${n} de ${total}`,
    steps: {
      boards: ["Tableros", "Todos tus tableros. Cada tablero tiene además un acceso más abajo en la barra lateral; arrástralos para reordenar."],
      tickets: ["Tickets", "Todos los tickets de todos los tableros en una lista, para buscar y filtrar."],
      calendar: ["Calendario", "Fechas de vencimiento, eventos y recordatorios en un solo lugar."],
      monitors: ["Monitores", "Ejecuta una herramienta MCP según un horario y recibe un aviso cuando su resultado cambia."],
      connectors: ["Conectores", "Las CLI de IA y modelos locales con los que chatea Patio. Agrégalos o inicia sesión aquí cuando quieras."],
      chats: ["Chats", `Chatea con una IA que puede leer y actualizar tus tableros. ${key("N")} inicia un chat, ${key("⇧N", "Shift+N")} fija uno en el panel lateral y ${key("J")} muestra u oculta el panel.`],
      skills: ["Habilidades", "Instrucciones reutilizables y memorias guardadas que los chats cargan cuando una tarea las necesita."],
      mcp: ["MCP", "Da más herramientas a los chats con servidores MCP. Configuración también muestra cómo conectar Patio a otras apps de IA."],
      scheduled: ["Tareas programadas", "Ejecuta un prompt según un horario, como un resumen diario."],
      foot: ["Configuración y avisos", `Aquí están la configuración, el tema y las notificaciones. Presiona ${key("/")} cuando quieras para ver todos los atajos y repite este recorrido desde Configuración.`],
    },
  },
};

type Stop = keyof typeof text.en.steps;
const stops = Object.keys(text.en.steps) as Stop[];

export default function Tour({ onClose }: { onClose: () => void }) {
  const t = useText(text);
  const [index, setIndex] = useState(0);
  const [rect, setRect] = useState<DOMRect>();
  const dialog = useRef<HTMLDialogElement>(null);
  const nextButton = useRef<HTMLButtonElement>(null);
  const stop = stops[index];
  const [title, body] = t.steps[stop];
  const last = index === stops.length - 1;

  useLayoutEffect(() => {
    const measure = () => setRect(document.querySelector(`[data-tour="${stop}"]`)?.getBoundingClientRect());
    measure();
    nextButton.current?.focus();
    addEventListener("resize", measure);
    return () => removeEventListener("resize", measure);
  }, [stop]);

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "ArrowRight" && !last) setIndex(index + 1);
      if (event.key === "ArrowLeft" && index > 0) setIndex(index - 1);
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [index, last, onClose]);

  const pad = 4;
  const cardTop = rect ? Math.min(Math.max(rect.top - 8, 12), innerHeight - 240) : undefined;

  return (
    <dialog ref={dialog} className="tour" aria-labelledby="tour-title" onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}>
      {rect ? (
        <div
          className="tour-spot"
          style={{ top: rect.top - pad, left: rect.left - pad, width: rect.width + pad * 2, height: rect.height + pad * 2 }}
        />
      ) : (
        <div className="tour-backdrop" />
      )}
      <section
        className={rect ? "panel tour-card" : "panel tour-card centered"}
        style={rect ? { top: cardTop, left: rect.right + 16 } : undefined}
      >
        <p className="eyebrow">
          {t.tour} · {t.step(index + 1, stops.length)}
        </p>
        <h2 id="tour-title">{title}</h2>
        <p>{body}</p>
        <div className="modal-actions">
          <button type="button" className="ghost" onClick={onClose}>
            {t.skip}
          </button>
          {index > 0 && (
            <button type="button" onClick={() => setIndex(index - 1)}>
              {t.back}
            </button>
          )}
          <button ref={nextButton} type="button" className="primary" onClick={() => (last ? onClose() : setIndex(index + 1))}>
            {last ? t.done : t.next}
          </button>
        </div>
      </section>
    </dialog>
  );
}
