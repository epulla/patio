import { confirmDialog } from "./confirm";
import {
  memo,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type CSSProperties,
  type DragEvent,
  type FormEvent,
  type KeyboardEvent,
  type ReactNode,
  type UIEvent,
} from "react";
import Markdown, { copyMarkdown } from "./Markdown";
import Menu from "./Menu";
import ModelPicker, { type ModelOption } from "./ModelPicker";
import {
  api,
  type AllowedFolder,
  type Bot,
  type Chat,
  type ChatApproval,
  type ChatAnswer,
  type ChatAttachment,
  type ChatDefaults,
  type ChatMessage,
  type ChatQuestion,
  type Connector,
} from "./api";
import { ErrorText, navigate } from "./App";
import { Icon, IconButton, type IconName } from "./icons";
import SplitButton from "./SplitButton";
import Switch from "./Switch";
import { formatBytes } from "./Settings";
import { ConnectorRequired } from "./Connectors";
import { requestNewPinnedChat, newPinnedShortcut, addGroup, allPinnedIds, moveGroup, groupDragType, pinColors, pinStyle, setGroupColor, type PinColor, deleteGroup, groupOf, isUnpinKey, moveTab, stepIndex, tabMoveStep, tabStep, maxPins, moveToGroup, pin, pinNumber, pinShortcut, rememberPinnedChat, renameGroup, setActiveGroup, setGroupView, setPins, shownPin, type PinView, lastPinnedChat, maxPanes, pruneSplit, replaceLeaf, setGroupSplit, setSplitRatio, splitLeaf, splitLeaves, type SplitEdge, type SplitNode, unpin, unpinAriaShortcut, unpinShortcut, usePinGroups, usePins } from "./pins";
import Picker from "./Picker";
import { useMentions } from "./mentions";
import { markSeen, statusIcons, statusLabels, syncChatStatus, useChatStatus } from "./chatStatus";
import { canSpeak, SpeakButton } from "./speech";
import PinTabs, { dragGhost, reorderType as pinTabType, useGroupReorder } from "./PinTabs";
import type { usePinnedChats } from "./ChatDock";
import { lang, locale, useText } from "./i18n";

const labels = {
  en: { fromChat: "From", deletedChat: "a deleted chat", continuedFrom: "Continued from", handedOffCount: (n: number) => `Handed off to ${n} chat${n === 1 ? "" : "s"}`, defaultModel: "Default", queue: "Queue", queuedList: "Queued messages", queuedNote: "Sent together after the current reply.", queuedSteerNote: "Sent together after the current reply, or now to steer it.", queueHeld: "Queue paused.", sendQueued: "Send now", cancelQueued: (text: string) => `Cancel queued message: ${text}`, queuedAnnounce: "Message queued", lateTitle: "The app was closed at the scheduled time, so this ran when it opened again.", copied: "Copied", copy: "Copy", unpinFirst: (max: number) => `Unpin a chat first (max ${max}).`, layout: "Layout", views: { one: "One tab", grid: "All in a grid", split: "Custom split" }, splitHint: "Drag a tab onto a pane edge to split it, or onto its middle to replace it", chats: "Chats", newChat: "New chat", collapseList: "Collapse chat list", expandList: "Expand chat list", expandNoChats: "Expand chat list (no chats yet)", pinned: (n: number) => `Pinned (${n})`, group: (name: string, n: number) => `${name} (${n})`, newGroup: "New group", newPinnedChat: (shortcut: string) => `New pinned chat (${shortcut})`, groupOptions: "Pin groups", newGroupName: (n: number) => `Group ${n}`, groupName: "Group name", groupActions: (name: string) => `${name} options`, rename: "Rename", color: "Color", colors: { pink: "Pink", red: "Red", orange: "Orange", yellow: "Yellow", green: "Green", teal: "Teal", blue: "Blue", purple: "Purple" }, renameHint: "Double-click to rename", deleteGroup: () => "Delete group", deleteNamedGroup: (name: string) => `Delete ${name}`, deleteGroupHint: "Unpins its chats; the chats stay", moveToGroup: (name: string) => `Move to ${name}`, groupFull: (name: string) => `${name} is full (max ${maxPins}). Unpin a chat there first.`, emptyGroup: (name: string) => `${name} has no chats yet. Pin one from the list.`, scheduled: "scheduled", pinnedWord: "pinned", noChats: "No chats yet.", unpin: "Unpin chat", pin: "Pin chat", dragSwap: "Drag to swap", moveChat: "Move chat", moveEarlier: "Move earlier", moveLater: "Move later", moveUp: "Move up", moveDown: "Move down", dragGroup: "Drag to reorder", resizeColumns: "Resize columns", resizeRows: "Resize rows", pickChat: "Pick a chat or start a new one.", startChat: "start a chat", attachLimit: (max: number) => `Attach at most ${max} files per message.`, imageLarge: (name: string) => `${name} is larger than 10 MB.`, textLarge: (name: string) => `${name} is larger than 1 GB. Text files can be up to 1 GB.`, stopThinking: "Stop the reply to change thinking", thinkingHint: "Let the model reason before answering. Slower but smarter.", thinkingLive: "Thinking…", resultLoadError: (reason: unknown) => `Could not load the result: ${reason}`, resultLoading: "Loading result…", toolResult: "Tool result", chars: (size: number) => `${size} chars`, charsK: (size: string) => `${size}k chars`, showFolder: "Show in folder", approvalRequest: (name: string) => `Approval request for ${name}`, deny: "Deny", chooseMany: "Choose one or more", selectedCount: (n: number) => `${n} selected`, otherAnswer: (header: string) => `Other answer for ${header}`, otherOptional: "Other answer (optional)", sendAnswers: "Send answers", next: "Next", chooseFolder: "Choose folder", close: "Close", folderPath: "Folder path", noFolders: "No folders inside.", connector: "Connector", noConnector: "No chat connector", model: "Model", loadingModels: "Loading models", folderLabel: "Folder", folders: "Folders", current: "current", wantFolders: "Want more folders?", addSettings: "Add them in Settings", cancel: "Cancel", useFolder: "Use this folder", startChatButton: "Start chat", scratchFolder: "Scratch folder (no files shared)", addFolders: "Add folders in Settings → Allowed folders.", focusChat: (shortcut: string) => `Focus this chat with ${shortcut}`, attachments: "Attachments", autoAllowed: "Auto-allowed", failed: "failed", allow: (name: string) => `Allow ${name}?`, allowOnce: "Allow once", askedOne: "Asked 1 question:", askedMany: (n: number) => `Asked ${n} questions:`, questionsFromAssistant: "Questions from the assistant", questionTabs: "Questions", back: "Back", chatTitle: "Chat title", closeSplit: "Close split", splitSide: "Split side by side", splitOptions: "Split options", sideBySide: "Side by side", stacked: "Stacked", openMain: "Open main window", openWindow: "Open in new window", deleteChat: "Delete chat", scheduledTask: "Scheduled task", assistantReplying: "Assistant is replying.", resizeSplit: "Resize split", otherWindow: "Replying in another window.", openItsWindow: "Open in its window", dropFiles: "Drop files to attach", filesToSend: "Files to send", remove: (name: string) => `Remove ${name}`, uploading: "Uploading…", message: "Message", placeholder: "Message… (Enter to send, Shift+Enter for a new line, @ bot, # ticket)", connectorRemovedPlaceholder: "This chat's connector was removed", attachFiles: "Attach files", folder: (name: string) => `Folder: ${name}`, changeFolder: "Change folder…", fileManager: "Open in file manager", fileManagerHelp: (folder: string) => `Open in file manager: ${folder}. Start a new chat to use another folder.`, chatSettings: "Chat settings", autoAllowOn: "Chat settings (auto-allow is on)", thinking: "Thinking", showThinking: "Show thinking", claudeThinking: "Claude Code doesn't share its reasoning, so no thinking appears in this chat", codexThinking: "Show Codex's reasoning summaries when it sends them; simple questions often have none", modelThinking: "Show the model's reasoning above its replies", autoAllow: "Auto-allow", autoAllowHint: "Every file change and command runs without asking", askEvery: "Ask before every file change and command", share: (label: string) => `Share ${label} with the agent`, sharing: (label: string) => `Sharing ${label} with the agent. Click to stop`, clickShare: (label: string) => `Click to share ${label} with the agent`, connectorRemoved: "Connector removed", stop: "Stop", send: "Send", missingChat: "This chat no longer exists.", loadingChat: "Loading chat…", loadingChatLabel: "Loading chat", folderNone: "Scratch folder (no files shared)", hint: "Ask anything. The assistant can read files in this chat's folder and use your tickets. Attach text files and images with the paperclip, drag and drop, or paste.", working: "Working", allowChanges: "Allow every file change and command in this chat without asking? You can turn it off anytime.", turnOnAutoAllow: "Turn on auto-allow", deleteConfirm: (title: string) => `Delete chat \"${title}\"?`, replyReceived: "Reply received.", jumpLatest: "Jump to latest" },
  es: { fromChat: "De", deletedChat: "un chat eliminado", continuedFrom: "Continúa de", handedOffCount: (n: number) => `Delegado a ${n} chat${n === 1 ? "" : "s"}`, defaultModel: "Predeterminado", queue: "Encolar", queuedList: "Mensajes en cola", queuedNote: "Se envían juntos después de la respuesta actual.", queuedSteerNote: "Se envían juntos después de la respuesta actual, o ahora para redirigirla.", queueHeld: "Cola en pausa.", sendQueued: "Enviar ahora", cancelQueued: (text: string) => `Cancelar mensaje en cola: ${text}`, queuedAnnounce: "Mensaje en cola", lateTitle: "La app estaba cerrada a la hora programada y se ejecutó al volver a abrirla.", copied: "Copiado", copy: "Copiar", unpinFirst: (max: number) => `Quita un chat de fijados primero (máx. ${max}).`, layout: "Diseño", views: { one: "Una pestaña", grid: "Todas en cuadrícula", split: "División personalizada" }, splitHint: "Arrastra una pestaña al borde de un panel para dividirlo, o al centro para reemplazarlo", chats: "Chats", newChat: "Nuevo chat", collapseList: "Contraer lista de chats", expandList: "Expandir lista de chats", expandNoChats: "Expandir lista de chats (aún no hay chats)", pinned: (n: number) => `Fijados (${n})`, group: (name: string, n: number) => `${name} (${n})`, newGroup: "Nuevo grupo", newPinnedChat: (shortcut: string) => `Nuevo chat fijado (${shortcut})`, groupOptions: "Grupos fijados", newGroupName: (n: number) => `Grupo ${n}`, groupName: "Nombre del grupo", groupActions: (name: string) => `Opciones de ${name}`, rename: "Renombrar", color: "Color", colors: { pink: "Rosa", red: "Rojo", orange: "Naranja", yellow: "Amarillo", green: "Verde", teal: "Turquesa", blue: "Azul", purple: "Morado" }, renameHint: "Doble clic para renombrar", deleteGroup: () => "Eliminar grupo", deleteNamedGroup: (name: string) => `Eliminar ${name}`, deleteGroupHint: "Quita sus chats de fijados; los chats se conservan", moveToGroup: (name: string) => `Mover a ${name}`, groupFull: (name: string) => `${name} está lleno (máx. ${maxPins}). Quita un chat de ahí primero.`, emptyGroup: (name: string) => `${name} aún no tiene chats. Fija uno desde la lista.`, scheduled: "programado", pinnedWord: "fijado", noChats: "Aún no hay chats.", unpin: "Quitar chat de fijados", pin: "Fijar chat", dragSwap: "Arrastra para intercambiar", moveChat: "Mover chat", moveEarlier: "Mover antes", moveLater: "Mover después", moveUp: "Mover arriba", moveDown: "Mover abajo", dragGroup: "Arrastra para reordenar", resizeColumns: "Cambiar tamaño de columnas", resizeRows: "Cambiar tamaño de filas", pickChat: "Elige un chat o inicia uno nuevo.", startChat: "iniciar un chat", attachLimit: (max: number) => `Adjunta como máximo ${max} archivos por mensaje.`, imageLarge: (name: string) => `${name} supera los 10 MB.`, textLarge: (name: string) => `${name} supera 1 GB. Los archivos de texto pueden pesar hasta 1 GB.`, stopThinking: "Detén la respuesta para cambiar el razonamiento", thinkingHint: "Deja que el modelo razone antes de responder. Tardará más, pero será más inteligente.", thinkingLive: "Razonando…", resultLoadError: (reason: unknown) => `No se pudo cargar el resultado: ${reason}`, resultLoading: "Cargando resultado…", toolResult: "Resultado de la herramienta", chars: (size: number) => `${size} caracteres`, charsK: (size: string) => `${size} mil caracteres`, showFolder: "Mostrar en carpeta", approvalRequest: (name: string) => `Solicitud de aprobación para ${name}`, deny: "Rechazar", chooseMany: "Elige una o más", selectedCount: (n: number) => (n === 1 ? "1 seleccionada" : `${n} seleccionadas`), otherAnswer: (header: string) => `Otra respuesta para ${header}`, otherOptional: "Otra respuesta (opcional)", sendAnswers: "Enviar respuestas", next: "Siguiente", chooseFolder: "Elegir carpeta", close: "Cerrar", folderPath: "Ruta de la carpeta", noFolders: "No hay carpetas aquí.", connector: "Conector", noConnector: "No hay conector de chat", model: "Modelo", loadingModels: "Cargando modelos", folderLabel: "Carpeta", folders: "Carpetas", current: "actual", wantFolders: "¿Quieres más carpetas?", addSettings: "Agrégalas en Ajustes", cancel: "Cancelar", useFolder: "Usar esta carpeta", startChatButton: "Iniciar chat", scratchFolder: "Carpeta temporal (sin archivos compartidos)", addFolders: "Agrega carpetas en Ajustes → Carpetas permitidas.", focusChat: (shortcut: string) => `Enfocar este chat con ${shortcut}`, attachments: "Archivos adjuntos", autoAllowed: "Permitido automáticamente", failed: "falló", allow: (name: string) => `¿Permitir ${name}?`, allowOnce: "Permitir una vez", askedOne: "Hizo 1 pregunta:", askedMany: (n: number) => `Hizo ${n} preguntas:`, questionsFromAssistant: "Preguntas del asistente", questionTabs: "Preguntas", back: "Atrás", chatTitle: "Título del chat", closeSplit: "Cerrar división", splitSide: "Dividir en paralelo", splitOptions: "Opciones de división", sideBySide: "En paralelo", stacked: "Apilado", openMain: "Abrir ventana principal", openWindow: "Abrir en ventana nueva", deleteChat: "Eliminar chat", scheduledTask: "Tarea programada", assistantReplying: "El asistente está respondiendo.", resizeSplit: "Cambiar tamaño de división", otherWindow: "Respondiendo en otra ventana.", openItsWindow: "Abrir en su ventana", dropFiles: "Suelta archivos para adjuntarlos", filesToSend: "Archivos para enviar", remove: (name: string) => `Quitar ${name}`, uploading: "Subiendo…", message: "Mensaje", placeholder: "Mensaje… (Enter para enviar, Shift+Enter para salto de línea, @ bot, # ticket)", connectorRemovedPlaceholder: "Se quitó el conector de este chat", attachFiles: "Adjuntar archivos", folder: (name: string) => `Carpeta: ${name}`, changeFolder: "Cambiar carpeta…", fileManager: "Abrir en el administrador de archivos", fileManagerHelp: (folder: string) => `Abrir en el administrador de archivos: ${folder}. Inicia un chat nuevo para usar otra carpeta.`, chatSettings: "Ajustes del chat", autoAllowOn: "Ajustes del chat (permisos automáticos activos)", thinking: "Razonamiento", showThinking: "Mostrar razonamiento", claudeThinking: "Claude Code no comparte su razonamiento, así que no aparece en este chat", codexThinking: "Mostrar resúmenes de razonamiento de Codex cuando los envíe; las preguntas simples a menudo no tienen", modelThinking: "Mostrar el razonamiento del modelo sobre sus respuestas", autoAllow: "Permitir automáticamente", autoAllowHint: "Todos los cambios de archivos y comandos se ejecutan sin preguntar", askEvery: "Preguntar antes de cada cambio de archivo y comando", share: (label: string) => `Compartir ${label} con el agente`, sharing: (label: string) => `Compartiendo ${label} con el agente. Haz clic para dejar de compartir`, clickShare: (label: string) => `Haz clic para compartir ${label} con el agente`, connectorRemoved: "Se quitó el conector", stop: "Detener", send: "Enviar", missingChat: "Este chat ya no existe.", loadingChat: "Cargando chat…", loadingChatLabel: "Cargando chat", folderNone: "Carpeta temporal (sin archivos compartidos)", hint: "Pregunta lo que quieras. El asistente puede leer archivos de la carpeta del chat y usar tus tickets. Adjunta archivos de texto e imágenes con el clip, arrástralos o pégalos.", working: "Procesando", allowChanges: "¿Permitir todos los cambios de archivos y comandos en este chat sin preguntar? Puedes desactivarlo cuando quieras.", turnOnAutoAllow: "Activar permisos automáticos", deleteConfirm: (title: string) => `¿Eliminar el chat \"${title}\"?`, replyReceived: "Llegó una respuesta.", jumpLatest: "Ir al último mensaje" },
};

const viewIcons: Record<PinView, IconName> = { one: "square", grid: "boards", split: "columns" };

const mac = /Mac|iPhone|iPad/.test(navigator.platform);
const newChatShortcut = mac ? "⌘N" : "Ctrl+N";
const newChatAriaShortcut = mac ? "Meta+N" : "Control+N";

type ChatDetail = {
  chat: Chat;
  messages: ChatMessage[];
  pending_approvals: ChatApproval[];
  auto_approvals?: ChatApproval[];
  parent: LinkedChat | null;
  children: LinkedChat[];
  version: string;
};
type LinkedChat = { id: string; title: string };
type ChatEvent =
  | { type: "delta"; text: string }
  | { type: "thinking_delta"; text: string }
  | { type: "tool" | "thinking" | "question"; message: ChatMessage }
  | { type: "tool_result"; message_id: string; tool_result_size: number; tool_error: boolean }
  | { type: "done"; message?: ChatMessage }
  | { type: "error"; message: string }
  | { type: "approval" }
  | { type: "queue" };
type QueuedMessage = {
  id: string;
  text: string;
  attachments: ChatAttachment[];
  created_at: string;
  from_chat_id: string | null;
  from_chat_title: string;
};

export const lateTitle = labels[lang()].lateTitle;

const modelCache = new Map<string, ModelOption[]>();

export function useModels(connectorID: string | null | undefined) {
  const t = useText(labels);
  const fallbackModels: ModelOption[] = [{ id: "", label: t.defaultModel }];
  const cached = connectorID ? modelCache.get(connectorID) : fallbackModels;
  const [state, setState] = useState({ id: connectorID, models: cached ?? fallbackModels, loading: !cached });
  useEffect(() => {
    if (!connectorID) return setState({ id: connectorID, models: fallbackModels, loading: false });
    const hit = modelCache.get(connectorID);
    if (hit) return setState({ id: connectorID, models: hit, loading: false });
    let active = true;
    setState({ id: connectorID, models: fallbackModels, loading: true });
    api
      .get<{ models: ModelOption[]; note?: string }>(`/api/connectors/${connectorID}/models`)
      .then((result) => {
        const models = result.models.length ? result.models : fallbackModels;
        // A note means the connector failed and only "Default" came back; retry next time.
        if (!result.note) modelCache.set(connectorID, models);
        if (active) setState({ id: connectorID, models, loading: false });
      })
      .catch(() => active && setState({ id: connectorID, models: fallbackModels, loading: false }));
    return () => {
      active = false;
    };
  }, [connectorID]);
  return state.id === connectorID ? state : { models: fallbackModels, loading: true };
}

function MessageTime({ iso }: { iso: string }) {
  const t = useText(labels);
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return null;
  const now = new Date();
  const time: Intl.DateTimeFormatOptions = { hour: "numeric", minute: "2-digit" };
  const options: Intl.DateTimeFormatOptions =
    date.toDateString() === now.toDateString()
      ? time
      : { month: "short", day: "numeric", ...(date.getFullYear() !== now.getFullYear() && { year: "numeric" }), ...time };
  return (
    <time className="message-time" dateTime={iso} title={date.toLocaleString(locale())}>
      {date.toLocaleString(locale(), options)}
    </time>
  );
}

function CopyButton({ text }: { text: string }) {
  const t = useText(labels);
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);
  return (
    <IconButton
      type="button"
      className="ghost"
      icon={copied ? "check" : "copy"}
      label={copied ? t.copied : t.copy}
      onClick={() => copyMarkdown(text).then(() => setCopied(true), () => undefined)}
    />
  );
}

type ChatPage = { chats: Chat[]; next_cursor: string | null };

const pageSize = 50;
const paneType = "application/x-chat-pane";
const tabType = "application/x-chat-tab";

// Within a quarter of an edge splits the pane there; elsewhere replaces its chat.
function dropZone(event: DragEvent, element: HTMLElement): SplitEdge | "center" {
  const box = element.getBoundingClientRect();
  const x = (event.clientX - box.left) / box.width;
  const y = (event.clientY - box.top) / box.height;
  const edge = Math.min(x, 1 - x, y, 1 - y);
  if (edge > 0.25) return "center";
  return edge === x ? "left" : edge === 1 - x ? "right" : edge === y ? "top" : "bottom";
}

// Remounted per page (key) so a still-visible sentinel fires again.
function LoadMore({ onVisible }: { onVisible: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const callback = useRef(onVisible);
  callback.current = onVisible;
  useEffect(() => {
    const observer = new IntersectionObserver(([entry]) => entry.isIntersecting && callback.current());
    if (ref.current) observer.observe(ref.current);
    return () => observer.disconnect();
  }, []);
  return <div ref={ref} className="chat-list-end" aria-hidden="true" />;
}

export default function Chats({
  id,
  refresh,
  windowed,
  pinned,
}: {
  id?: string;
  refresh: number;
  windowed?: boolean;
  pinned?: ReturnType<typeof usePinnedChats>;
}) {
  const t = useText(labels);
  const statusText = useText(statusLabels);
  const [chats, setChats] = useState<Chat[]>([]);
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<unknown>();
  const [reload, setReload] = useState(0);
  const pins = usePins();
  const groups = usePinGroups();
  const activeGroup = groups.groups.find((group) => group.id === groups.active)!;
  const pinnedIds = new Map(groups.groups.flatMap((group) => group.ids.map((chatID) => [chatID, group.color] as const)));
  const chatPinStyle = (chatID: string) => {
    const color = pinnedIds.get(chatID);
    return color && pinStyle(color);
  };
  const showGroups = pinnedIds.size > 0 || groups.groups.length > 1;
  const [renaming, setRenaming] = useState<string>();
  const reorder = useGroupReorder(groups.groups);
  const groupDropped = useRef(false);
  const [dropInto, setDropInto] = useState<string>();
  const [listOpen, setListOpen] = useState(() => localStorage.getItem("chat-list") !== "closed");
  const [cursor, setCursor] = useState<string | null>(null);
  const loadedCount = useRef(0);
  const generation = useRef(0);
  const loadingMore = useRef(false);
  const existingPins = useRef(new Set<string>());
  // The rail scrolls, which would clip a CSS [data-tip] tooltip, so it is positioned fixed instead.
  const [tip, setTip] = useState<{ text: string; top: number; left: number; focus: boolean }>();

  function showTip(button: HTMLElement, text: string, focus: boolean) {
    if (focus && !button.matches(":focus-visible")) return;
    const rect = button.getBoundingClientRect();
    setTip({ text, top: rect.top + rect.height / 2, left: rect.right + 8, focus });
  }

  // With a single connector the dialog has nothing to choose, so create the chat with its defaults.
  const startingChat = useRef(false);
  async function newChat() {
    if (connectors.length !== 1) return setCreating(true);
    if (startingChat.current) return;
    startingChat.current = true;
    try {
      const chat = await createChatWithDefaults(connectors[0]);
      navigate(`/ai/chats/${chat.id}`);
    } catch (reason) {
      setError(reason);
    } finally {
      startingChat.current = false;
    }
  }

  useEffect(() => {
    const current = ++generation.current;
    Promise.all([
      api.get<ChatPage>(`/api/chats?limit=${Math.max(pageSize, loadedCount.current)}`),
      api.get<Connector[]>("/api/connectors"),
    ])
      .then(([page, connectorList]) => {
        if (current !== generation.current) return;
        setChats(page.chats);
        syncChatStatus(page.chats);
        setCursor(page.next_cursor);
        setConnectors(connectorList);
        setLoaded(true);
        setError(undefined);
        const listed = new Set(page.chats.map((chat) => chat.id));
        for (const pinned of allPinnedIds()) {
          if (listed.has(pinned) || existingPins.current.has(pinned)) continue;
          api
            .get(`/api/chats/${pinned}`)
            .then(() => existingPins.current.add(pinned))
            .catch((err: unknown) => {
              if (!(err instanceof Error) || err.message !== "chat not found") return;
              unpin(pinned);
            });
        }
      })
      .catch(setError);
  }, [refresh, reload, id]);

  useEffect(() => {
    loadedCount.current = chats.length;
  }, [chats]);

  // Opening a chat pinned in another group switches to that group, so its grid shows.
  useEffect(() => {
    const group = id && groupOf(id);
    if (group) setActiveGroup(group.id);
  }, [id]);

  async function loadMore() {
    if (!cursor || loadingMore.current) return;
    loadingMore.current = true;
    const current = generation.current;
    try {
      const page = await api.get<ChatPage>(`/api/chats?limit=${pageSize}&cursor=${encodeURIComponent(cursor)}`);
      if (current !== generation.current) return;
      syncChatStatus(page.chats);
      setChats((chats) => {
        const seen = new Set(chats.map((chat) => chat.id));
        return [...chats, ...page.chats.filter((chat) => !seen.has(chat.id))];
      });
      setCursor(page.next_cursor);
    } catch (err) {
      setError(err);
    } finally {
      loadingMore.current = false;
    }
  }

  function togglePin(chatID: string) {
    if (groupOf(chatID)) return unpin(chatID);
    if (!pin(chatID)) setError(new Error(t.unpinFirst(maxPins)));
  }

  function moveChat(chatID: string, groupID: string, name: string) {
    if (!moveToGroup(chatID, groupID)) setError(new Error(t.groupFull(name)));
  }

  // Naming needs the full list, so a group made from the collapsed rail expands it.
  function createGroup() {
    const groupID = addGroup(t.newGroupName(groups.groups.length + 1));
    localStorage.setItem("chat-list", "open");
    setListOpen(true);
    setTip(undefined);
    navigate("/ai/chats");
    setRenaming(groupID);
  }

  function openGroup(groupID: string) {
    setActiveGroup(groupID);
    navigate("/ai/chats");
  }

  const grid = useRef<HTMLDivElement>(null);
  const [gridWidth, setGridWidth] = useState(0);
  const [responsiveSizes, setResponsiveSizes] = useState<{ key: string; cols: number[]; rows: number[] }>();
  const [drag, setDrag] = useState<{ from: number; over?: number }>();
  const flip = useRef<Map<string, DOMRect>>(undefined);

  // FLIP: remember pane positions before a swap, then slide each pane from there to its new cell.
  useLayoutEffect(() => {
    const before = flip.current;
    flip.current = undefined;
    if (!before || matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    grid.current?.querySelectorAll<HTMLElement>("[data-pane]").forEach((pane) => {
      const old = before.get(pane.dataset.pane!);
      const now = pane.getBoundingClientRect();
      if (!old || (old.left === now.left && old.top === now.top)) return;
      pane.animate([{ transform: `translate(${old.left - now.left}px, ${old.top - now.top}px)` }, { transform: "none" }], {
        duration: 200,
        easing: "ease-out",
      });
    });
  }, [pins.ids]);

  function swap(from: number, to: number) {
    flip.current = new Map(
      [...(grid.current?.querySelectorAll<HTMLElement>("[data-pane]") ?? [])].map((pane) => [pane.dataset.pane!, pane.getBoundingClientRect()]),
    );
    const ids = [...pins.ids];
    [ids[from], ids[to]] = [ids[to], ids[from]];
    setPins({ ...pins, ids });
  }

  const showGroup = !windowed && pins.ids.length > 0 && (!id || pins.ids.includes(id));
  const showGrid = showGroup && activeGroup.view === "grid";
  const shown = showGroup && !showGrid ? id ?? shownPin(pins.ids) : id;
  const split: SplitNode | undefined =
    showGroup && activeGroup.view === "split" ? pruneSplit(activeGroup.split, pins.ids) ?? { chat: shown! } : undefined;
  const placed = splitLeaves(split);
  // A pinned chat opened outside the split (tab, Cmd+N, list) replaces the last focused pane.
  useLayoutEffect(() => {
    if (!split || !id || placed.includes(id)) return;
    const last = lastPinnedChat();
    setGroupSplit(activeGroup.id, replaceLeaf(split, last && placed.includes(last) ? last : placed[0], id));
  }, [split, id]);
  useEffect(() => {
    if (showGroup && shown) rememberPinnedChat(shown);
  }, [showGroup, shown]);
  const [drop, setDrop] = useState<{ pane: string; zone: SplitEdge | "center" }>();
  const dragged = useRef<string>(undefined);

  function startDrag(chatID: string, event: DragEvent, title: string) {
    event.dataTransfer.setData(tabType, chatID);
    event.dataTransfer.effectAllowed = "move";
    dragged.current = chatID;
    dragGhost(event, title);
  }

  function endDrag() {
    dragged.current = undefined;
    setDrop(undefined);
  }
  useLayoutEffect(() => {
    const element = grid.current;
    if (!showGrid || !element) return;
    setGridWidth(element.clientWidth);
    const observer = new ResizeObserver(() => setGridWidth(element.clientWidth));
    observer.observe(element);
    return () => observer.disconnect();
  }, [showGrid]);

  const columnCount = Math.max(1, Math.min(pins.cols.length, Math.floor((gridWidth + 6) / 566)));
  const rowCount = Math.ceil(pins.ids.length / columnCount);
  const desktopLayout = columnCount === pins.cols.length;
  const layoutKey = `${pins.ids.length}:${columnCount}`;
  const temporarySizes = responsiveSizes?.key === layoutKey ? responsiveSizes : undefined;
  const columns = desktopLayout ? pins.cols : temporarySizes?.cols ?? Array(columnCount).fill(1 / columnCount);
  const rows = desktopLayout ? pins.rows : temporarySizes?.rows ?? Array(rowCount).fill(1 / rowCount);
  const columnTotal = columns.reduce((sum, size) => sum + size, 0);
  const fullRows = Math.floor(pins.ids.length / columnCount);
  const status = useChatStatus(chats.map((chat) => chat.id));
  const visible = (showGrid ? pins.ids : split ? placed : shown ? [shown] : []).join();
  useEffect(() => visible.split(",").forEach(markSeen), [visible, status]);

  function openPin(chatID: string) {
    navigate(`/ai/chats/${chatID}`);
    document.querySelector<HTMLTextAreaElement>(`[data-pane="${chatID}"] textarea`)?.focus();
  }

  const showTabs = !windowed && (pins.ids.length > 0 || !!id);
  useEffect(() => {
    if (!showTabs) return;
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const chatID = document.activeElement?.closest<HTMLElement>("[data-pane]")?.dataset.pane ?? shown ?? id;
      if (isUnpinKey(event)) {
        if (!chatID) return;
        event.preventDefault();
        // Closing an unpinned chat's preview tab leaves the chat itself alone.
        return pins.ids.includes(chatID) ? unpin(chatID) : navigate("/ai/chats");
      }
      const move = tabMoveStep(event);
      if (move) {
        if (!chatID || !pins.ids.includes(chatID)) return;
        event.preventDefault();
        const index = pins.ids.indexOf(chatID);
        // moveTab counts the insertion point before removing the tab, so moving right skips one more.
        return moveTab(chatID, move > 0 ? index + 2 : index - 1);
      }
      const step = tabStep(event);
      const n = step
        ? pins.ids.length && stepIndex(chatID ? pins.ids.indexOf(chatID) : -1, step, pins.ids.length) + 1
        : pinNumber(event, pins.ids.length);
      if (!n) return;
      event.preventDefault();
      openPin(pins.ids[n - 1]);
    };
    addEventListener("keydown", onKeyDown);
    return () => removeEventListener("keydown", onKeyDown);
  }, [showTabs, pins.ids, shown, id]);

  useEffect(() => {
    if (windowed || (loaded && !connectors.length)) return;
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey) || event.shiftKey || event.altKey || event.key.toLowerCase() !== "n") return;
      event.preventDefault();
      if (document.querySelector("dialog[open]")) return;
      newChat();
    };
    addEventListener("keydown", onKeyDown);
    return () => removeEventListener("keydown", onKeyDown);
  }, [windowed, loaded, connectors]);
  function renderSplit(node: SplitNode, path: string): ReactNode {
    if (!("chat" in node)) {
      const row = node.dir === "row";
      const sizes = [node.ratio, 1 - node.ratio];
      return (
        <div
          key={path}
          className="split-node"
          style={{ [row ? "gridTemplateColumns" : "gridTemplateRows"]: `minmax(0, ${sizes[0]}fr) 6px minmax(0, ${sizes[1]}fr)` }}
        >
          {renderSplit(node.a, path + "a")}
          <Splitter
            vertical={row}
            label={t.resizeSplit}
            sizes={sizes}
            index={0}
            min={240}
            onChange={([a, b]) => setGroupSplit(activeGroup.id, setSplitRatio(split!, path, a / (a + b)))}
          />
          {renderSplit(node.b, path + "b")}
        </div>
      );
    }
    const chatID = node.chat;
    const index = pins.ids.indexOf(chatID);
    return (
      <div
        key={chatID}
        className="grid-pane"
        data-pane={chatID}
        data-drop={drop?.pane === chatID ? drop.zone : undefined}
        onFocus={() => rememberPinnedChat(chatID)}
        onPointerDown={() => rememberPinnedChat(chatID)}
        onDragOver={(event) => {
          const chat = dragged.current;
          if (!chat || !event.dataTransfer.types.includes(tabType)) return;
          const zone = dropZone(event, event.currentTarget);
          if (chat === chatID || (zone !== "center" && !placed.includes(chat) && placed.length >= maxPanes)) {
            if (drop) setDrop(undefined);
            return;
          }
          event.preventDefault();
          if (drop?.pane !== chatID || drop.zone !== zone) setDrop({ pane: chatID, zone });
        }}
        onDragLeave={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDrop(undefined);
        }}
        onDrop={(event) => {
          const chat = dragged.current;
          if (!chat || drop?.pane !== chatID) return;
          event.preventDefault();
          endDrag();
          setGroupSplit(activeGroup.id, drop.zone === "center" ? replaceLeaf(split!, chatID, chat) : splitLeaf(split!, chatID, drop.zone, chat));
          navigate(`/ai/chats/${chat}`);
        }}
      >
        <Conversation
          id={chatID}
          refresh={refresh}
          connectors={connectors}
          pinned
          focused={chatID === id}
          onPin={() => togglePin(chatID)}
          onChanged={() => setReload((value) => value + 1)}
          pane={
            <>
              <span
                className="pane-grip"
                draggable
                aria-hidden="true"
                data-tip={t.splitHint}
                onDragStart={(event) =>
                  startDrag(chatID, event, event.currentTarget.closest(".conversation-head")?.querySelector("input")?.value || t.chats)
                }
                onDragEnd={endDrag}
              >
                <Icon name="grip" />
              </span>
              <kbd className="pin-kbd" data-tip={t.focusChat(pinShortcut(index + 1))}>
                {pinShortcut(index + 1)}
              </kbd>
            </>
          }
        />
      </div>
    );
  }
  const tracks = (sizes: number[]) => sizes.map((size) => `minmax(360px, ${size}fr)`).join(" 6px ");

  return (
    <div className={windowed ? "chats windowed" : "chats"} style={pinStyle(activeGroup.color)}>
      {!windowed && (
      <aside className={listOpen ? "chat-list" : "chat-list collapsed"} aria-label={t.chats}>
        <div className="chat-list-head">
          {listOpen && (connectors.length > 0 || !loaded) && (
            <button
              className="primary"
              data-tip={`${t.newChat} (${newChatShortcut})`}
              aria-keyshortcuts={newChatAriaShortcut}
              data-tour="new-chat"
              onClick={newChat}
            >
              <Icon name="plus" /> {t.newChat}
            </button>
          )}
          <IconButton
            className="ghost"
            icon={listOpen ? "chevronsLeft" : "chevronsRight"}
            label={
              listOpen
                ? t.collapseList
                : loaded && chats.length === 0 && !error
                  ? t.expandNoChats
                  : t.expandList
            }
            aria-expanded={listOpen}
            onClick={() => {
              localStorage.setItem("chat-list", listOpen ? "closed" : "open");
              setListOpen(!listOpen);
              setTip(undefined);
            }}
          />
        </div>
        {!listOpen && (connectors.length > 0 || !loaded) && (
          <IconButton
            className="ghost"
            icon="plus"
            label={`${t.newChat} (${newChatShortcut})`}
            aria-keyshortcuts={newChatAriaShortcut}
            onClick={newChat}
          />
        )}
        {!listOpen && showGroups && (
          <SplitButton
            icon="boards"
            label={t.group(activeGroup.name, pins.ids.length)}
            menuLabel={t.groupOptions}
            current={showGroup || !id}
            onClick={() => navigate("/ai/chats")}
          >
            {(close) => (
              <>
                {groups.groups.map((group) => (
                  <button
                    key={group.id}
                    role="menuitemradio"
                    aria-checked={group.id === groups.active}
                    onClick={() => (close(), openGroup(group.id))}
                  >
                    <span className="menu-check">{group.id === groups.active && <Icon name="check" />}</span>
                    <span className="pin-mark" style={pinStyle(group.color)} aria-hidden="true"><Icon name="boards" /></span>
                    {t.group(group.name, group.ids.length)}
                  </button>
                ))}
                <hr className="menu-divider" />
                <button role="menuitem" disabled={pins.ids.length >= maxPins} onClick={() => (close(false), requestNewPinnedChat())}>
                  <Icon name="chat" /> {t.newPinnedChat(newPinnedShortcut)}
                </button>
                <button role="menuitem" onClick={() => (close(false), createGroup())}>
                  <Icon name="plus" /> {t.newGroup}
                </button>
                {groups.groups.length > 1 && (
                  <button role="menuitem" className="danger-text" title={t.deleteGroupHint} onClick={() => (close(), deleteGroup(groups.active))}>
                    <Icon name="trash" /> {t.deleteNamedGroup(activeGroup.name)}
                  </button>
                )}
              </>
            )}
          </SplitButton>
        )}
        {!listOpen && chats.length > 0 && (
          <nav className="chat-rail" onScroll={() => setTip(undefined)}>
            {chats.map((chat) => {
              const pinned = pinnedIds.has(chat.id);
              const scheduled = !!chat.scheduled_task_id;
              const state = status[chat.id];
              return (
                <button
                  key={chat.id}
                  className="chat-initial"
                  style={chatPinStyle(chat.id)}
                  aria-label={`${chat.title}${state ? `, ${statusText[state]}` : ""}${scheduled ? `, ${t.scheduled}` : ""}${pinned ? `, ${t.pinnedWord}` : ""}`}
                  aria-current={!showGroup && chat.id === id ? "page" : undefined}
                  onClick={() => navigate(`/ai/chats/${chat.id}`)}
                  onMouseEnter={(event) => showTip(event.currentTarget, chat.title, false)}
                  onFocus={(event) => showTip(event.currentTarget, chat.title, true)}
                  onMouseLeave={() => setTip(undefined)}
                  onBlur={() => setTip(undefined)}
                >
                  {[...chat.title.trim()][0]?.toUpperCase() ?? "?"}
                  {(state || scheduled || pinned) && (
                    <span className="chat-badges" aria-hidden="true">
                      {state && (
                        <span className="dock-dot" data-state={state}>
                          <Icon name={statusIcons[state]} />
                        </span>
                      )}
                      {scheduled && (
                        <span className="badge-scheduled">
                          <Icon name="clock" />
                        </span>
                      )}
                      {pinned && (
                        <span className="badge-pinned">
                          <Icon name="pin" />
                        </span>
                      )}
                    </span>
                  )}
                </button>
              );
            })}
            {cursor && <LoadMore key={chats.length} onVisible={loadMore} />}
          </nav>
        )}
        {tip && (
          <div className={tip.focus ? "rail-tip focus" : "rail-tip"} aria-hidden="true" style={{ top: tip.top, left: tip.left }}>
            {tip.text}
          </div>
        )}
        <ErrorText error={error} />
        {listOpen && chats.length === 0 && !error && <p className="muted">{t.noChats}</p>}
        {listOpen && (
        <nav
          ref={reorder.list}
          onDragOver={(event) => event.dataTransfer.types.includes(groupDragType) && event.preventDefault()}
          onDrop={(event) => {
            if (event.dataTransfer.types.includes(groupDragType)) groupDropped.current = true;
          }}
        >
          {showGroups &&
            reorder.ordered.map((group, index) => (
              <div
                key={group.id}
                className={`chat-row group-row${dropInto === group.id ? " drop-into" : ""}`}
                style={pinStyle(group.color)}
                data-group={group.id}
                data-dragging={reorder.dragging === group.id ? "" : undefined}
                draggable={renaming !== group.id}
                title={t.dragGroup}
                onDragStart={(event) => {
                  event.dataTransfer.setData(groupDragType, group.id);
                  event.dataTransfer.effectAllowed = "move";
                  groupDropped.current = false;
                  reorder.start(group.id);
                }}
                onDragOver={(event) => {
                  // A pinned tab dropped on a group moves the chat there; a group row reorders.
                  if (event.dataTransfer.types.includes(pinTabType)) {
                    event.preventDefault();
                    return setDropInto(group.id);
                  }
                  if (!event.dataTransfer.types.includes(groupDragType)) return;
                  event.preventDefault();
                  reorder.over(group.id, event.currentTarget);
                }}
                onDragLeave={(event) => {
                  // Moving onto the row's own button or menu isn't leaving the row.
                  if (event.currentTarget.contains(event.relatedTarget as Node)) return;
                  setDropInto(undefined);
                }}
                onDrop={(event) => {
                  const chatID = event.dataTransfer.getData(pinTabType);
                  if (chatID) {
                    event.preventDefault();
                    moveChat(chatID, group.id, group.name);
                  }
                  setDropInto(undefined);
                }}
                onDragEnd={() => reorder.end(groupDropped.current)}
              >
                {renaming === group.id ? (
                  <input
                    className="group-name"
                    aria-label={t.groupName}
                    autoFocus
                    defaultValue={group.name}
                    onFocus={(event) => event.currentTarget.select()}
                    onBlur={(event) => {
                      const name = event.currentTarget.value.trim();
                      if (name) renameGroup(group.id, name);
                      setRenaming(undefined);
                    }}
                    onKeyDown={(event) => {
                      if (event.key === "Escape") event.currentTarget.value = group.name;
                      if (event.key === "Enter" || event.key === "Escape") event.currentTarget.blur();
                    }}
                  />
                ) : (
                  <button
                    className="chat-link"
                    title={t.renameHint}
                    aria-current={(showGroup || !id) && group.id === groups.active ? "page" : undefined}
                    onClick={() => openGroup(group.id)}
                    onDoubleClick={() => setRenaming(group.id)}
                  >
                    <span className="chat-link-pinned">
                      <Icon name="boards" /> {t.group(group.name, group.ids.length)}
                    </span>
                  </button>
                )}
                {renaming !== group.id && (
                  <Menu label={t.groupActions(group.name)} buttonClassName="icon ghost" button={<Icon name="more" />}>
                    {(close) => (
                      <>
                        <button role="menuitem" onClick={() => (close(false), setRenaming(group.id))}>
                          <Icon name="pencil" /> {t.rename}
                        </button>
                        <div className="pin-swatches" role="group" aria-label={t.color}>
                          {(Object.keys(pinColors) as PinColor[]).map((color) => (
                            <button
                              key={color}
                              role="menuitemradio"
                              aria-checked={group.color === color}
                              aria-label={t.colors[color]}
                              title={t.colors[color]}
                              className="pin-swatch"
                              style={pinStyle(color)}
                              onClick={() => (close(), setGroupColor(group.id, color))}
                            />
                          ))}
                        </div>
                        {index > 0 && (
                          <button role="menuitem" onClick={() => (close(), moveGroup(group.id, index - 1))}>
                            <Icon name="chevronLeft" /> {t.moveUp}
                          </button>
                        )}
                        {index < groups.groups.length - 1 && (
                          <button role="menuitem" onClick={() => (close(), moveGroup(group.id, index + 2))}>
                            <Icon name="chevronRight" /> {t.moveDown}
                          </button>
                        )}
                        {groups.groups.length > 1 && (
                          <button role="menuitem" className="danger-text" title={t.deleteGroupHint} onClick={() => (close(false), deleteGroup(group.id))}>
                            <Icon name="trash" /> {t.deleteGroup()}
                          </button>
                        )}
                      </>
                    )}
                  </Menu>
                )}
              </div>
            ))}
          {showGroups && (
            <button className="chat-link new-group" onClick={createGroup}>
              <span className="chat-link-pinned">
                <Icon name="plus" /> {t.newGroup}
              </span>
            </button>
          )}
          {chats.map((chat) => {
            const pinned = pinnedIds.has(chat.id);
            const state = status[chat.id];
            return (
              <div key={chat.id} className="chat-row" style={chatPinStyle(chat.id)}>
                <button
                  className="chat-link"
                  aria-current={!showGroup && chat.id === id ? "page" : undefined}
                  onClick={() => navigate(`/ai/chats/${chat.id}`)}
                >
                  <span className="chat-link-title">
                    {state && (
                      <span className="dock-dot" data-state={state} aria-hidden="true">
                        <Icon name={statusIcons[state]} />
                      </span>
                    )}
                    <span>{chat.title}</span>
                    {state && <span className="sr-only">, {statusText[state]}</span>}
                  </span>
                  <small>
                    {new Date(chat.updated_at).toLocaleDateString(locale())}
                    {chat.scheduled_task_id && (
                      <>
                        {" · "}
                        <Icon name="clock" /> {t.scheduled}
                      </>
                    )}
                    {chat.scheduled_late && (
                    <span className="run-label" title={t.lateTitle}>
                        Ran late
                      </span>
                    )}
                  </small>
                </button>
                <IconButton
                  className={pinned ? "ghost pinned" : "ghost"}
                  icon="pin"
                  label={pinned ? t.unpin : t.pin}
                  onClick={() => togglePin(chat.id)}
                />
              </div>
            );
          })}
          {cursor && <LoadMore key={chats.length} onVisible={loadMore} />}
        </nav>
        )}
      </aside>
      )}
      {showTabs && (
        <PinTabs
          chats={pinned?.chats ?? chats}
          status={{ ...status, ...pinned?.status }}
          current={split ? id : shown}
          placed={split && placed}
          onTabDragStart={split && startDrag}
          onTabDragEnd={endDrag}
          preview={
            id && !pins.ids.includes(id)
              ? {
                  title: chats.find((chat) => chat.id === id)?.title ?? t.chats,
                  status: status[id],
                  onClose: () => navigate("/ai/chats"),
                  onPin: () => togglePin(id),
                }
              : undefined
          }
          onSelect={openPin}
          onUnpin={unpin}
          onGroup={openGroup}
        >
          {pins.ids.length > 0 && (
            <Menu
              label={t.layout}
              buttonClassName="icon ghost"
              button={<Icon name={viewIcons[activeGroup.view]} />}
            >
              {(close) =>
                (["one", "grid", "split"] as PinView[]).map((view) => (
                  <button
                    key={view}
                    role="menuitemradio"
                    aria-checked={activeGroup.view === view}
                    title={view === "split" ? t.splitHint : undefined}
                    onClick={() => (close(), setGroupView(activeGroup.id, view))}
                  >
                    <span className="menu-check">{activeGroup.view === view && <Icon name="check" />}</span>
                    <Icon name={viewIcons[view]} /> {t.views[view]}
                  </button>
                ))
              }
            </Menu>
          )}
        </PinTabs>
      )}
      {showGrid ? (
        <div
          ref={grid}
          className="chat-grid"
          style={{ gridTemplateRows: tracks(rows) }}
          onDragOver={(event) => {
            if (drag && !(event.target as Element).closest(".grid-pane")) setDrag({ from: drag.from });
          }}
          onDragLeave={(event) => {
            const to = event.relatedTarget as Node | null;
            if (drag && to && !event.currentTarget.contains(to)) setDrag({ from: drag.from });
          }}
        >
          {pins.ids.map((chatID, index) => {
            const row = Math.floor(index / columnCount);
            const column = index % columnCount;
            const count = Math.min(columnCount, pins.ids.length - row * columnCount);
            const share = count === columnCount ? columns[column] / columnTotal : 1 / count;
            const offset = count === columnCount ? columns.slice(0, column).reduce((sum, size) => sum + size, 0) / columnTotal : column / count;
            return (
            <div
              key={chatID}
              className="grid-pane"
              data-pane={chatID}
              // WebKit doesn't focus links on click, so pointerdown covers clicking a link in the pane.
              onFocus={() => rememberPinnedChat(chatID)}
              onPointerDown={() => rememberPinnedChat(chatID)}
              data-dragging={drag?.from === index || undefined}
              data-drop-target={(drag && drag.from !== index && drag.over === index) || undefined}
              style={{
                gridColumn: 1,
                gridRow: row * 2 + 1,
                width: `calc((100% - ${(count - 1) * 6}px) * ${share})`,
                marginLeft: `calc((100% - ${(count - 1) * 6}px) * ${offset} + ${column * 6}px)`,
              }}
              onDragOver={(event) => {
                if (!event.dataTransfer.types.includes(paneType)) return;
                event.preventDefault();
                if (drag && drag.over !== index) setDrag({ from: drag.from, over: index });
              }}
              onDrop={(event) => {
                const from = event.dataTransfer.getData(paneType);
                if (!from) return;
                event.preventDefault();
                setDrag(undefined);
                if (Number(from) !== index) swap(Number(from), index);
              }}
            >
              <Conversation
                id={chatID}
                refresh={refresh}
                connectors={connectors}
                pinned
                focused={chatID === id}
                onPin={() => togglePin(chatID)}
                onChanged={() => setReload((value) => value + 1)}
                pane={
                  <>
                    <span
                      className="pane-grip"
                      draggable
                      aria-hidden="true"
                      data-tip={t.dragSwap}
                      onDragStart={(event) => {
                        event.dataTransfer.setData(paneType, String(index));
                        event.dataTransfer.effectAllowed = "move";
                        // WebKit snapshots only painted pixels, so drag a solid card instead of the transparent header.
                        const ghost = document.createElement("div");
                        ghost.className = "pane-drag-ghost";
                        ghost.textContent =
                        event.currentTarget.closest(".conversation-head")?.querySelector("input")?.value || t.chats;
                        document.body.append(ghost);
                        event.dataTransfer.setDragImage(ghost, 20, 20);
                        // Defer so the preview is captured before the card is removed and the pane dims.
                        setTimeout(() => {
                          ghost.remove();
                          setDrag({ from: index });
                        });
                      }}
                      onDragEnd={() => setDrag(undefined)}
                    >
                      <Icon name="grip" />
                    </span>
                    <kbd className="pin-kbd" data-tip={t.focusChat(pinShortcut(index + 1))}>
                      {pinShortcut(index + 1)}
                    </kbd>
                    {(pins.ids.length > 1 || groups.groups.length > 1) && (
                      <Menu label={t.moveChat} buttonClassName="icon ghost" button={<Icon name="more" />}>
                        {(close) => (
                          <>
                            {index > 0 && (
                              <button role="menuitem" onClick={() => (close(), swap(index, index - 1))}>
                                <Icon name="chevronLeft" /> {t.moveEarlier}
                              </button>
                            )}
                            {index < pins.ids.length - 1 && (
                              <button role="menuitem" onClick={() => (close(), swap(index, index + 1))}>
                                <Icon name="chevronRight" /> {t.moveLater}
                              </button>
                            )}
                            {groups.groups
                              .filter((group) => group.id !== groups.active)
                              .map((group) => (
                                <button key={group.id} role="menuitem" onClick={() => (close(), moveChat(chatID, group.id, group.name))}>
                                  <Icon name="boards" /> {t.moveToGroup(group.name)}
                                </button>
                              ))}
                          </>
                        )}
                      </Menu>
                    )}
                  </>
                }
              />
            </div>
            );
          })}
          {columns.slice(1).map((_, index) => (
            <Splitter
              key={`col-${index}`}
              vertical
              label={t.resizeColumns}
              sizes={columns}
              index={index}
              min={240}
              style={{
                gridColumn: 1,
                gridRow: `1 / ${fullRows * 2}`,
                width: 6,
                marginLeft: `calc((100% - ${(columnCount - 1) * 6}px) * ${columns.slice(0, index + 1).reduce((sum, size) => sum + size, 0) / columnTotal} + ${index * 6}px)`,
              }}
              onChange={(cols) => desktopLayout
                ? setPins((current) => ({ ...current, cols }))
                : setResponsiveSizes({ key: layoutKey, cols, rows })}
            />
          ))}
          {rows.slice(1).map((_, index) => (
            <Splitter
              key={`row-${index}`}
              label={t.resizeRows}
              sizes={rows}
              index={index}
              min={360}
              style={{ gridRow: index * 2 + 2, gridColumn: "1 / -1" }}
              onChange={(rows) => desktopLayout
                ? setPins((current) => ({ ...current, rows }))
                : setResponsiveSizes({ key: layoutKey, cols: columns, rows })}
            />
          ))}
        </div>
      ) : split ? (
        <div className="chat-split">{renderSplit(split, "")}</div>
      ) : shown ? (
        <Conversation
          key={shown}
          id={shown}
          refresh={refresh}
          windowed={windowed}
          connectors={connectors}
          pinned={pinnedIds.has(shown)}
          focused={showGroup && shown === id}
          onPin={windowed ? undefined : () => togglePin(shown)}
          onChanged={() => setReload((value) => value + 1)}
        />
      ) : connectors.length || !loaded ? (
        <section className="chat-empty">
          <h1>{t.chats}</h1>
          <p className="muted">{groups.groups.length > 1 ? t.emptyGroup(activeGroup.name) : t.pickChat}</p>
        </section>
      ) : (
        <ConnectorRequired then={t.startChat} />
      )}
      {creating && (
        <NewChatDialog
          connectors={connectors}
          onClose={() => setCreating(false)}
          onCreated={(chat) => {
            setCreating(false);
            navigate(`/ai/chats/${chat.id}`);
          }}
        />
      )}
    </div>
  );
}

function Splitter({
  label,
  vertical,
  sizes,
  index,
  min,
  style,
  onChange,
}: {
  label: string;
  vertical?: boolean;
  sizes: number[];
  index: number;
  min: number;
  style?: CSSProperties;
  onChange: (sizes: number[]) => void;
}) {
  function move(element: HTMLElement, start: number[], pixels: number) {
    const box = element.parentElement?.getBoundingClientRect();
    if (!box) return;
    const limit = min / (vertical ? box.width : box.height);
    if (start[index] + start[index + 1] < limit * 2) return;
    const delta = Math.min(Math.max(pixels / (vertical ? box.width : box.height), limit - start[index]), start[index + 1] - limit);
    const next = [...start];
    next[index] += delta;
    next[index + 1] -= delta;
    onChange(next);
  }

  return (
    <div
      role="separator"
      tabIndex={0}
      aria-label={label}
      aria-orientation={vertical ? "vertical" : "horizontal"}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round((sizes[index] / (sizes[index] + sizes[index + 1])) * 100)}
      className="splitter"
      style={style}
      onPointerDown={(event) => {
        event.preventDefault();
        const element = event.currentTarget;
        element.focus();
        const start = sizes;
        const origin = vertical ? event.clientX : event.clientY;
        element.setPointerCapture(event.pointerId);
        element.onpointermove = (moved) => move(element, start, (vertical ? moved.clientX : moved.clientY) - origin);
        element.onlostpointercapture = () => (element.onpointermove = null);
      }}
      onKeyDown={(event) => {
        const steps: Record<string, number> = vertical ? { ArrowLeft: -1, ArrowRight: 1 } : { ArrowUp: -1, ArrowDown: 1 };
        const step = steps[event.key];
        if (!step) return;
        event.preventDefault();
        move(event.currentTarget, sizes, step * 24);
      }}
    />
  );
}

export type PageContext = { icon: IconName; label: string; line: string };

export const startContextKey = (chatID: string) => `chat-context:${chatID}`;

// Last page context sent per chat, so the same page isn't repeated on every message.
const sentContext = new Map<string, string>();

export function Conversation({
  id,
  refresh,
  windowed,
  compact,
  otherWindow,
  context: pageContext,
  connectors,
  pinned,
  focused,
  pane,
  onPin,
  onChanged,
}: {
  id: string;
  refresh: number;
  windowed?: boolean;
  compact?: boolean;
  otherWindow?: boolean;
  context?: PageContext;
  connectors: Connector[];
  pinned?: boolean;
  focused?: boolean;
  pane?: ReactNode;
  onPin?: () => void;
  onChanged: () => void;
}) {
  const t = useText(labels);
  const uid = useId();
  const [split, setSplit] = useState<"" | "row" | "column">("");
  const [choosingFolder, setChoosingFolder] = useState(false);
  const [splitSizes, setSplitSizes] = useState([0.5, 0.5]);
  const input = useRef<HTMLTextAreaElement>(null);
  const [detail, setDetail] = useState<ChatDetail>();
  const [streaming, setStreaming] = useState("");
  const [thinking, setThinking] = useState("");
  const [running, setRunning] = useState(false);
  const [queue, setQueue] = useState<QueuedMessage[]>([]);
  const posting = useRef(false);
  const draftKey = `chat-draft:${id}`;
  const [text, setText] = useState(() => localStorage.getItem(draftKey) ?? "");
  function changeText(value: string) {
    setText(value);
    if (value) localStorage.setItem(draftKey, value);
    else localStorage.removeItem(draftKey);
  }
  const mentions = useMentions(input, changeText, "above");
  // Context a chat was started with (e.g. from a ticket) wins over the page until the first send.
  const startKey = startContextKey(id);
  const [startContext, setStartContext] = useState<PageContext | undefined>(() => {
    try {
      return JSON.parse(localStorage.getItem(startKey) ?? "null") ?? undefined;
    } catch {
      return undefined;
    }
  });
  const context = startContext ?? pageContext;
  const [contextOff, setContextOff] = useState<string>();
  const contextOn = !!context && contextOff !== context.line;
  const [title, setTitle] = useState("");
  const [error, setError] = useState<unknown>();
  const [missing, setMissing] = useState(false);
  const [announcement, setAnnouncement] = useState("");
  const [pending, setPending] = useState<ChatAttachment[]>([]);
  const [uploading, setUploading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const end = useRef<HTMLDivElement>(null);
  const filePicker = useRef<HTMLInputElement>(null);
  const { models, loading: modelsLoading } = useModels(detail?.chat.connector_id);

  // Every db-changed reloads; the server answers "unchanged" when this chat's version still matches.
  const loaded = useRef<{ id: string; version: string }>(undefined);
  const load = () =>
    api
      .get<ChatDetail | { unchanged: true }>(
        `/api/chats/${id}${loaded.current?.id === id ? `?version=${encodeURIComponent(loaded.current.version)}` : ""}`,
      )
      .then((value) => {
        if ("unchanged" in value) return;
        loaded.current = { id, version: value.version };
        setDetail(value);
        setTitle(value.chat.title);
        setRunning(value.chat.running);
      })
      .catch((reason) => {
        if (String(reason).includes("not found")) setMissing(true);
        else setError(reason);
      });

  const loadQueue = () => api.get<QueuedMessage[]>(`/api/chats/${id}/queue`).then(setQueue).catch(setError);

  useEffect(() => {
    void load();
    void loadQueue();
  }, [id, refresh]);

  useEffect(() => {
    setPending([]);
    setDragging(false);
  }, [id]);

  useEffect(() => {
    const unsubscribe = window.runtime?.EventsOn(`chat:${id}`, (data) => {
      const event = data as ChatEvent;
      if (event.type === "approval") void load();
      if (event.type === "queue") {
        void loadQueue();
        void load();
      }
      if (event.type === "delta") setStreaming((current) => current + event.text);
      if (event.type === "thinking_delta") setThinking((current) => current + event.text);
      if (event.type === "thinking") setThinking("");
      if (event.type === "tool_result")
        setDetail((current) =>
          current && {
            ...current,
            messages: current.messages.map((m) =>
              m.id === event.message_id
                ? { ...m, tool_result_size: event.tool_result_size, tool_error: event.tool_error }
                : m,
            ),
          },
        );
      if (event.type === "tool" || event.type === "thinking" || event.type === "question" || event.type === "done") {
        if (event.type === "done") {
          setStreaming("");
          setThinking("");
          setRunning(false);
          setAnnouncement(t.replyReceived);
          void load();
          return;
        }
        const message = event.message;
        if (message)
          setDetail((current) =>
            current && !current.messages.some((m) => m.id === message.id)
              ? { ...current, messages: [...current.messages, message] }
              : current,
          );
      }
      if (event.type === "error") {
        setStreaming("");
        setThinking("");
        setRunning(false);
        setError(event.message);
      }
    });
    return () => unsubscribe?.();
  }, [id]);

  // Follow new output only while the reader is at the bottom, so scrolling up to read isn't interrupted.
  const atBottom = useRef(true);
  useEffect(() => {
    atBottom.current = true;
  }, [id]);
  useEffect(() => {
    const box = end.current?.parentElement;
    if (box && atBottom.current) box.scrollTop = box.scrollHeight;
  }, [detail?.messages.length, streaming, detail?.pending_approvals.length, split]);

  const [away, setAway] = useState(false);
  const onMessagesScroll = (event: UIEvent<HTMLDivElement>) => {
    const box = event.currentTarget;
    const distance = box.scrollHeight - box.scrollTop - box.clientHeight;
    atBottom.current = distance < 80;
    setAway(distance > 200);
  };
  const jump = away && (
    <button
      type="button"
      className="jump-latest"
      onClick={() => end.current?.scrollIntoView({ behavior: "smooth", block: "end" })}
    >
      <Icon name="chevronDown" /> {t.jumpLatest}
    </button>
  );

  const ready = !!detail;
  useEffect(() => {
    if (focused && ready) input.current?.focus();
  }, [focused, ready]);

  async function attach(files: File[]) {
    if (!files.length || uploading) return;
    const problem = attachmentProblem(files, pending.length);
    if (problem) {
      setError(new Error(problem));
      return;
    }
    setError(undefined);
    setUploading(true);
    try {
      for (const file of files) {
        const added = await api.upload<ChatAttachment[]>(`/api/chats/${id}/attachments`, file);
        setPending((current) => [...current, ...added]);
      }
    } catch (reason) {
      setError(reason);
    } finally {
      setUploading(false);
    }
  }

  async function unattach(attachment: ChatAttachment) {
    try {
      await api.delete(`/api/chats/${id}/attachments/${encodeURIComponent(attachment.name)}`);
      setPending((current) => current.filter((a) => a.name !== attachment.name));
    } catch (reason) {
      setError(reason);
    }
  }

  async function revealAttachment(attachment: ChatAttachment) {
    try {
      await api.post(`/api/chats/${id}/attachments/${encodeURIComponent(attachment.name)}/reveal`, {});
    } catch (reason) {
      setError(reason);
    }
  }

  function onPaste(event: ClipboardEvent<HTMLTextAreaElement>) {
    const files = Array.from(event.clipboardData.files);
    if (!files.length) return;
    event.preventDefault();
    void attach(files.map(pastedFile));
  }

  function onDragOver(event: DragEvent) {
    if (!connector || !event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
    setDragging(true);
  }

  function onDragLeave(event: DragEvent) {
    const box = event.currentTarget.getBoundingClientRect();
    const inside =
      event.clientX > box.left && event.clientX < box.right && event.clientY > box.top && event.clientY < box.bottom;
    if (!inside) setDragging(false);
  }

  function onDrop(event: DragEvent) {
    if (!event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
    event.stopPropagation();
    setDragging(false);
    if (connector) void attach(Array.from(event.dataTransfer.files));
  }

  async function send(event?: FormEvent) {
    event?.preventDefault();
    const value = text.trim();
    if ((!value && !pending.length) || uploading || !detail || posting.current) return;
    // While a reply runs the backend queues the message instead of sending it.
    const queueing = running;
    posting.current = true;
    setError(undefined);
    if (!queueing) {
      setRunning(true);
      atBottom.current = true;
    }
    try {
      const prefix = context && contextOn && sentContext.get(id) !== context.line ? `${context.line}\n\n` : "";
      const result = await api.post<ChatMessage | { queued: QueuedMessage }>(`/api/chats/${id}/messages`, {
        text: prefix + value,
        ...(pending.length ? { attachments: pending.map((a) => a.name) } : {}),
      });
      if (prefix) sentContext.set(id, context!.line);
      if (startContext) {
        localStorage.removeItem(startKey);
        setStartContext(undefined);
      }
      changeText("");
      setPending([]);
      if ("queued" in result) {
        const item = result.queued;
        setQueue((current) => (current.some((q) => q.id === item.id) ? current : [...current, item]));
        setAnnouncement(t.queuedAnnounce);
        void load();
        return;
      }
      const message = result;
      addSent(message);
        if (detail.chat.title === "New chat" && detail.messages.length === 0) {
        const label = (value || pending.map((a) => a.name).join(", ")).replace(/\s+/g, " ");
        await rename(label.length > 48 ? `${label.slice(0, 48)}…` : label);
      }
    } catch (reason) {
      if (!queueing) setRunning(false);
      setError(reason);
    } finally {
      posting.current = false;
    }
  }

  async function sendQueue() {
    const steering = running;
    setError(undefined);
    setRunning(true);
    atBottom.current = true;
    try {
      addSent(await api.post<ChatMessage>(`/api/chats/${id}/queue/send`, {}));
      void loadQueue();
    } catch (reason) {
      if (!steering) setRunning(false);
      setError(reason);
      void load();
    }
  }

  async function cancelQueued(item: QueuedMessage) {
    try {
      await api.delete(`/api/chats/${id}/queue/${item.id}`);
      setQueue((current) => current.filter((q) => q.id !== item.id));
    } catch (reason) {
      setError(reason);
    }
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (mentions.onKeyDown(event)) return;
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      void send();
    }
  }

  async function stop() {
    try {
      await api.post(`/api/chats/${id}/stop`, {});
    } catch (reason) {
      setError(reason);
    }
  }

  async function rename(next: string) {
    const value = next.trim();
    if (!value || value === detail?.chat.title) return setTitle(detail?.chat.title ?? "");
    try {
      await api.put(`/api/chats/${id}`, { title: value });
      setTitle(value);
      setDetail((current) => current && { ...current, chat: { ...current.chat, title: value } });
      onChanged();
    } catch (reason) {
      setError(reason);
    }
  }

  async function update(
    fields: { model: string } | { thinking: boolean } | { show_thinking: boolean } | { auto_allow: boolean },
  ) {
    try {
      await api.put(`/api/chats/${id}`, fields);
      setDetail((current) => current && { ...current, chat: { ...current.chat, ...fields } });
      setError(undefined);
    } catch (reason) {
      setError(reason);
    }
  }

  async function changeAutoAllow(on: boolean) {
    if (on && !(await confirmDialog(t.allowChanges, t.turnOnAutoAllow))) return;
    await update({ auto_allow: on });
    if (on) void load();
  }

  async function changeFolder(folder: string | null) {
    const chat = await api.put<Chat>(`/api/chats/${id}`, { folder });
    setDetail((current) => current && { ...current, chat: { ...current.chat, ...chat } });
    setChoosingFolder(false);
    setError(undefined);
  }

  async function reveal(path: string) {
    try {
      await api.post(`/api/chats/${id}/reveal`, { path });
      setError(undefined);
    } catch (reason) {
      setError(reason);
    }
  }

  async function remove() {
    if (!(await confirmDialog(t.deleteConfirm(detail?.chat.title ?? "")))) return;
    try {
      await api.delete(`/api/chats/${id}`);
      onChanged();
      navigate("/ai/chats");
    } catch (reason) {
      setError(reason);
    }
  }

  // A sent message also closes open question cards, matching the backend.
  function addSent(message: ChatMessage) {
    setDetail(
      (current) =>
        current && {
          ...current,
          messages: [
            ...current.messages.map((m) => (m.role === "question" ? { ...m, answered: true } : m)),
            ...(current.messages.some((m) => m.id === message.id) ? [] : [message]),
          ],
        },
    );
  }

  const answerQuestion = useCallback(
    async (questionID: string, answers: ChatAnswer[]) => {
      setError(undefined);
      setRunning(true);
      atBottom.current = true;
      try {
        addSent(await api.post<ChatMessage>(`/api/chats/${id}/questions/${questionID}/answer`, { answers }));
      } catch (reason) {
        setRunning(false);
        setError(reason);
        void load();
      }
    },
    [id],
  );

  async function answer(approval: ChatApproval, allow: boolean) {
    try {
      await api.post(`/api/chats/${id}/approvals/${approval.id}`, { allow });
      setDetail(
        (current) =>
          current && {
            ...current,
            pending_approvals: current.pending_approvals.filter((a) => a.id !== approval.id),
          },
      );
    } catch (reason) {
      setError(reason);
      void load();
    }
  }

  if (missing)
    return (
      <section className="chat-empty">
        <p className="muted">{t.missingChat}</p>
      </section>
    );
  if (!detail)
    return (
      <section className="chat-empty" aria-busy="true" aria-label={t.loadingChatLabel}>
        <p role="status">{t.loadingChat}</p>
      </section>
    );

  const connector = connectors.find((c) => c.id === detail.chat.connector_id);
  const folderLabel = detail.chat.folder ?? t.folderNone;
  const openQuestion = [...detail.messages]
    .reverse()
    .find((m) => m.role === "question" && !m.answered && parseQuestions(m.content).length);
  const template = `minmax(0, ${splitSizes[0]}fr) 6px minmax(0, ${splitSizes[1]}fr)`;
  const history = (
    <>
      {detail.messages.length === 0 && !streaming && (
        <p className="muted chat-hint">
          {t.hint}
        </p>
      )}
      <Timeline chatId={id} detail={detail} onError={setError} />
      {thinking && detail.chat.show_thinking && <ThinkingBlock text={thinking} live />}
      {streaming && (
        <div className="message assistant">
          <Markdown text={streaming} onFile={reveal} />
        </div>
      )}
      {running && !streaming && (
        <p className="muted typing">
          {t.working}<span className="typing-dots" aria-hidden="true" />
        </p>
      )}
      {detail.pending_approvals.map((approval) => (
        <ApprovalCard key={approval.id} approval={approval} onAnswer={(allow) => answer(approval, allow)} />
      ))}
    </>
  );

  const chatLinks = (detail.chat.scheduled_task_id || detail.parent || detail.children.length > 0) && (
          <div className="conversation-meta">
            {detail.chat.scheduled_task_id && (
              <button
                type="button"
                className="folder-chip"
                onClick={() => navigate(`/ai/scheduled/${detail.chat.scheduled_task_id}`)}
              >
                <Icon name="clock" /> {t.scheduledTask}
              </button>
            )}
            {detail.parent && (
              <button
                type="button"
                className="folder-chip chat-link-chip"
                title={detail.parent.title}
                onClick={() => openChat(detail.parent!.id)}
              >
                <Icon name="arrowLeft" /> <span>{t.continuedFrom} {detail.parent.title}</span>
              </button>
            )}
            {detail.children.length > 0 && (
              <div className="chat-links-menu">
              <Menu
                label={t.handedOffCount(detail.children.length)}
                buttonClassName="folder-chip"
                button={
                  <>
                    <Icon name="chevronRight" /> {t.handedOffCount(detail.children.length)} <Icon name="chevronDown" />
                  </>
                }
              >
                {(close) =>
                  detail.children.map((child) => (
                    <button key={child.id} role="menuitem" className="chat-link-item" title={child.title} onClick={() => (close(false), openChat(child.id))}>
                      <Icon name="chat" /> <span>{child.title}</span>
                    </button>
                  ))
                }
              </Menu>
              </div>
            )}
          </div>
        );

  return (
    <section
      className={dragging ? "conversation dragging" : "conversation"}
      aria-labelledby={`${uid}-title`}
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
    >
      {!compact && (
      <header className="conversation-head">
        <div className="conversation-title">
          <label className="sr-only" htmlFor={`${uid}-title`}>
            {t.chatTitle}
          </label>
          <input
            id={`${uid}-title`}
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            onBlur={(event) => rename(event.target.value)}
            onKeyDown={(event) => event.key === "Enter" && event.currentTarget.blur()}
          />
        </div>
        {detail.chat.bot_id && <BotTag id={detail.chat.bot_id} />}
        {pane}
        {onPin && (
          <IconButton
            className={pinned ? "ghost pinned" : "ghost"}
            data-tour="pin"
            icon="pin"
            label={pinned ? `${t.unpin} (${unpinShortcut})` : t.pin}
            aria-keyshortcuts={pinned ? unpinAriaShortcut : undefined}
            onClick={onPin}
          />
        )}
        {!pane &&
          (split ? (
            <IconButton className="ghost" icon="square" label={t.closeSplit} onClick={() => setSplit("")} />
          ) : (
            <SplitButton icon="columns" label={t.splitSide} menuLabel={t.splitOptions} onClick={() => setSplit("row")}>
              {(close) => (
                <>
                  <button role="menuitem" onClick={() => (close(), setSplit("row"))}>
                    <Icon name="columns" /> {t.sideBySide}
                  </button>
                  <button role="menuitem" onClick={() => (close(), setSplit("column"))}>
                    <Icon name="rows" /> {t.stacked}
                  </button>
                </>
              )}
            </SplitButton>
          ))}
        <IconButton
          className="ghost"
          icon={windowed ? "arrowLeft" : "externalLink"}
          label={windowed ? t.openMain : t.openWindow}
          onClick={() =>
            api
              .post(windowed ? "/api/windows/main" : `/api/chats/${id}/window`, windowed ? { path: `/ai/chats/${id}` } : {})
              .catch(setError)
          }
        />
        <IconButton className="danger-text" icon="trash" label={t.deleteChat} onClick={remove} />
        {!compact && chatLinks}
      </header>
      )}
      {compact && chatLinks}
      {choosingFolder && (
        <FolderDialog
          current={detail.chat.folder}
          onPick={changeFolder}
          onClose={() => setChoosingFolder(false)}
          onSettings={() =>
            windowed ? api.post("/api/windows/main", { path: "/settings" }).catch(setError) : navigate("/settings")
          }
        />
      )}

      <p className="sr-only" role="status">
        {running ? t.assistantReplying : announcement}
      </p>
      {split ? (
        <div
          className="split"
          style={split === "row" ? { gridTemplateColumns: template } : { gridTemplateRows: template }}
        >
          <div className="messages">{history}</div>
          <Splitter
            vertical={split === "row"}
            label={t.resizeSplit}
            sizes={splitSizes}
            index={0}
            min={120}
            onChange={setSplitSizes}
          />
          <div className="messages" onScroll={onMessagesScroll}>
            {history}
            {jump}
            <div ref={end} />
          </div>
        </div>
      ) : (
        <div className="messages" onScroll={onMessagesScroll}>
          {history}
          {jump}
          <div ref={end} />
        </div>
      )}

      <ErrorText error={error} />
      {otherWindow && (
        <p className="muted dock-note">
          {t.otherWindow}
          <button type="button" className="ghost" onClick={() => api.post(`/api/chats/${id}/window`, {}).catch(setError)}>
            {t.openItsWindow}
          </button>
        </p>
      )}
      {dragging && (
        <p className="drop-hint" aria-hidden="true">
          <Icon name="paperclip" /> {t.dropFiles}
        </p>
      )}
      {(pending.length > 0 || uploading) && (
        <ul className="attachments" aria-label={t.filesToSend}>
          {pending.map((attachment) => (
            <li key={attachment.name} className="attachment">
              <AttachmentLabel attachment={attachment} />
              <IconButton
                type="button"
                icon="x"
                label={t.remove(attachment.name)}
                disabled={running}
                onClick={() => unattach(attachment)}
              />
            </li>
          ))}
          {uploading && (
            <li className="muted" role="status">
              {t.uploading}
            </li>
          )}
        </ul>
      )}
      {openQuestion && (
        <QuestionCard
          key={openQuestion.id}
          message={openQuestion}
          disabled={running}
          onAnswer={(answers) => answerQuestion(openQuestion.id, answers)}
        />
      )}
      {queue.length > 0 && (
        <section className="queued" aria-label={t.queuedList}>
          <p className="muted">
            {running ? (detail?.chat.capabilities.steer ? t.queuedSteerNote : t.queuedNote) : t.queueHeld}
            {(!running || detail?.chat.capabilities.steer) && (
              <button type="button" className="ghost" onClick={sendQueue}>
                {t.sendQueued}
              </button>
            )}
          </p>
          <ol className="attachments queued-list">
            {queue.map((item) => (
              <li key={item.id} className="attachment">
                <span className="attachment-label">
                  <Icon name="clock" />
                  {item.from_chat_id && (
                    <span className="muted">
                      {t.fromChat} {item.from_chat_title || t.deletedChat}:
                    </span>
                  )}
                  <span className="attachment-name">{item.text || item.attachments.map((a) => a.name).join(", ")}</span>
                  {item.text && item.attachments.length > 0 && (
                    <span className="muted">
                      <Icon name="paperclip" /> {item.attachments.length}
                    </span>
                  )}
                </span>
                <IconButton
                  type="button"
                  icon="x"
                  label={t.cancelQueued(item.text || item.attachments.map((a) => a.name).join(", "))}
                  onClick={() => cancelQueued(item)}
                />
              </li>
            ))}
          </ol>
        </section>
      )}
      <form className="composer" onSubmit={send}>
        <input
          ref={filePicker}
          type="file"
          multiple
          hidden
          onChange={(event) => {
            void attach(Array.from(event.target.files ?? []));
            event.target.value = "";
          }}
        />
        <label className="sr-only" htmlFor={`${uid}-input`}>
          {t.message}
        </label>
        <textarea
          ref={input}
          id={`${uid}-input`}
          rows={compact ? 2 : 3}
          placeholder={
            connector ? t.placeholder : t.connectorRemovedPlaceholder
          }
          disabled={!connector || otherWindow}
          value={text}
          onChange={(event) => changeText(event.target.value)}
          onSelect={() => void mentions.update()}
          onBlur={mentions.close}
          aria-autocomplete="list"
          aria-expanded={mentions.open}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
          onDragOver={onDragOver}
          onDrop={onDrop}
        />
        {mentions.popup}
        <div className="composer-actions">
          <div className="composer-tools">
            <IconButton
              type="button"
              className="ghost tip-up"
              icon="paperclip"
              label={t.attachFiles}
              disabled={!connector || uploading || pending.length >= maxAttachments}
              onClick={() => filePicker.current?.click()}
            />
            {detail.messages.length === 0 && !running ? (
              <span className="tip-up" data-tip={t.folder(folderLabel)}>
                <Menu label={t.folder(folderLabel)} buttonClassName="icon ghost" button={<Icon name="folder" />}>
                  {(close) => (
                    <>
                      <button role="menuitem" onClick={() => (close(false), setChoosingFolder(true))}>
                        <Icon name="folder" /> {t.changeFolder}
                      </button>
                      <button role="menuitem" onClick={() => (close(), reveal(detail.chat.workdir))}>
                        <Icon name="externalLink" /> {t.fileManager}
                      </button>
                    </>
                  )}
                </Menu>
              </span>
            ) : (
              <IconButton
                type="button"
                className="ghost tip-up tip-wide"
                icon="folder"
                label={t.fileManagerHelp(folderLabel)}
                onClick={() => reveal(detail.chat.workdir)}
              />
            )}
            <span className="tip-up" data-tip={t.chatSettings}>
              <Menu
                label={detail.chat.auto_allow ? t.autoAllowOn : t.chatSettings}
                buttonClassName="icon ghost settings-trigger"
                button={
                  <>
                    <Icon name="settings" />
                    {detail.chat.auto_allow && <span className="settings-dot" aria-hidden="true" />}
                  </>
                }
              >
                {() => (
                  <div className="chat-settings">
                    <SettingRow
                      label={t.thinking}
                      hint={thinkingTitle(detail.chat, running)}
                      checked={detail.chat.capabilities.thinking.supported && detail.chat.thinking}
                      disabled={running || !detail.chat.capabilities.thinking.supported}
                      onChange={(thinking) => update({ thinking })}
                    />
                    <SettingRow
                      label={t.showThinking}
                      hint={
                        connector?.kind === "claude"
                          ? t.claudeThinking
                          : connector?.kind === "codex"
                            ? t.codexThinking
                            : t.modelThinking
                      }
                      checked={detail.chat.show_thinking}
                      onChange={(show_thinking) => update({ show_thinking })}
                    />
                    <SettingRow
                      label={t.autoAllow}
                      warning
                      hint={
                        detail.chat.auto_allow
                          ? t.autoAllowHint
                          : t.askEvery
                      }
                      checked={detail.chat.auto_allow}
                      onChange={changeAutoAllow}
                    />
                  </div>
                )}
              </Menu>
            </span>
            {context && (
              <button
                type="button"
                className="context-chip tip-up"
                aria-pressed={contextOn}
                aria-label={t.share(context.label)}
                data-tip={contextOn ? t.sharing(context.label) : t.clickShare(context.label)}
                onClick={() => setContextOff(contextOn ? context.line : undefined)}
              >
                <Icon name={context.icon} />
                <span className="context-label">{context.label}</span>
                {contextOn && <Icon name="x" />}
              </button>
            )}
          </div>
          <div className="composer-send">
            <div className="composer-model">
              {!compact && <span className="muted">{connector ? connector.name : t.connectorRemoved}</span>}
              <ModelPicker
                up
                pinKind={connector?.kind}
                models={models}
                value={detail.chat.model}
                loading={modelsLoading}
                disabled={!connector || running}
                onChange={(model) => update({ model })}
              />
            </div>
            {running ? (
              <>
                <button type="button" onClick={stop}>
                  <span className="spinner" aria-hidden="true" /> {t.stop}
                </button>
                <button className="primary" disabled={(!text.trim() && !pending.length) || uploading || !connector || otherWindow}>
                  <Icon name="clock" /> {t.queue}
                </button>
              </>
            ) : (
              <button className="primary" disabled={(!text.trim() && !pending.length) || uploading || !connector || otherWindow}>
                <Icon name="send" /> {t.send}
              </button>
            )}
          </div>
        </div>
      </form>
    </section>
  );
}

const maxAttachments = 10;
const imageTypes = ["image/png", "image/jpeg", "image/gif", "image/webp"];

function attachmentProblem(files: File[], alreadyAttached: number) {
  const t = labels[lang()];
  if (alreadyAttached + files.length > maxAttachments) return t.attachLimit(maxAttachments);
  for (const file of files) {
    const image = imageTypes.includes(file.type);
    if (image && file.size > 10 << 20) return t.imageLarge(file.name);
    if (!image && file.size > 1 << 30) return t.textLarge(file.name);
  }
  return "";
}

function pastedFile(file: File) {
  if (!file.type.startsWith("image/") || (file.name && file.name !== "image.png")) return file;
  const now = new Date();
  const pad = (value: number) => String(value).padStart(2, "0");
  const stamp = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}.${pad(now.getMinutes())}.${pad(now.getSeconds())}`;
  const extension = file.type.split("/")[1]?.replace("jpeg", "jpg") || "png";
  return new File([file], `pasted-${stamp}.${extension}`, { type: file.type });
}

function AttachmentLabel({ attachment }: { attachment: ChatAttachment }) {
  return (
    <span className="attachment-label">
      <Icon name={attachment.kind === "image" ? "image" : "file"} />
      <span className="attachment-name">{attachment.name}</span>
      <span className="muted">{formatBytes(attachment.size)}</span>
    </span>
  );
}

function BotTag({ id }: { id: string }) {
  const [bot, setBot] = useState<Bot>();
  useEffect(() => {
    api
      .get<Bot[]>("/api/bots")
      .then((bots) => setBot(bots.find((b) => b.id === id)))
      .catch(() => setBot(undefined));
  }, [id]);
  if (!bot) return null;
  return (
    <button type="button" className="ghost bot-tag" onClick={() => navigate(`/ai/bots/${bot.id}`)}>
      <span className="bot-dot" style={{ background: bot.color || "var(--muted)" }} aria-hidden="true" />
      {bot.name}
    </button>
  );
}

function SettingRow({
  label,
  hint,
  checked,
  disabled,
  warning,
  onChange,
}: {
  label: string;
  hint: string;
  checked: boolean;
  disabled?: boolean;
  warning?: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <button
      type="button"
      role="menuitemcheckbox"
      aria-checked={checked}
      disabled={disabled}
      className={warning ? "setting-row warning" : "setting-row"}
      onClick={() => onChange(!checked)}
    >
      <span className="setting-text">
        <span>{label}</span>
        <small className="muted">{hint}</small>
      </span>
      <span className="switch-track" aria-hidden="true" />
    </button>
  );
}

function thinkingTitle(chat: Chat, running: boolean) {
  const t = labels[lang()];
  const { supported, reason } = chat.capabilities.thinking;
  if (!supported) return reason;
  if (running) return t.stopThinking;
  return [t.thinkingHint, reason].filter(Boolean).join(" ");
}

function ThinkingBlock({ text, live }: { text: string; live?: boolean }) {
  const t = useText(labels);
  return (
    <details className="thinking">
      <summary>{live ? t.thinkingLive : t.thinking}</summary>
      <p>{text}</p>
    </details>
  );
}

function timeline(detail: ChatDetail): (ChatMessage | ChatApproval)[] {
  const auto = detail.auto_approvals ?? [];
  if (!auto.length) return detail.messages;
  return [...detail.messages, ...auto].sort((a, b) => a.created_at.localeCompare(b.created_at));
}

// Memoized so typing and streamed deltas don't re-render hundreds of past messages.
const Timeline = memo(function Timeline({
  chatId,
  detail,
  onError,
}: {
  chatId: string;
  detail: ChatDetail;
  onError: (reason: unknown) => void;
}) {
  const t = useText(labels);
  async function reveal(path: string) {
    try {
      await api.post(`/api/chats/${chatId}/reveal`, { path });
      onError(undefined);
    } catch (reason) {
      onError(reason);
    }
  }
  async function revealAttachment(attachment: ChatAttachment) {
    try {
      await api.post(`/api/chats/${chatId}/attachments/${encodeURIComponent(attachment.name)}/reveal`, {});
    } catch (reason) {
      onError(reason);
    }
  }
  const lastReply = [...detail.messages].reverse().find((message) => message.role === "assistant" && message.content)?.id;
  return (
    <>
    {timeline(detail).map((message) =>
      "tool_name" in message ? (
        <ToolLine key={message.id} body={() => `${message.tool_name}\n${prettyJSON(message.input_json)}`}>
          <Icon name="check" /> <code>{t.autoAllowed} {approvalSummary(message)}</code>{" "}
          <MessageTime iso={message.created_at} />
        </ToolLine>
      ) : message.role === "tool" ? (
        <ToolLine
          key={message.id}
          body={() => prettyJSON(message.content)}
          resultURL={
            message.tool_result_size || message.tool_error
              ? `/api/chats/${message.chat_id}/messages/${message.id}/tool-result`
              : undefined
          }
        >
          <Icon name="plug" /> <code>{message.content}</code>{" "}
          {(!!message.tool_result_size || message.tool_error) && (
            <ToolResultBadge size={message.tool_result_size ?? 0} failed={!!message.tool_error} />
          )}
          <MessageTime iso={message.created_at} />
        </ToolLine>
      ) : message.role === "question" ? (
        <QuestionLine key={message.id} message={message} />
      ) : message.role === "thinking" ? (
        detail.chat.show_thinking && <ThinkingBlock key={message.id} text={message.content} />
      ) : (
        <div key={message.id} className={`message-row ${message.role}`}>
          {message.from_chat_id && <ChatFrom id={message.from_chat_id} title={message.from_chat_title} />}
          {(message.role !== "user" || message.content) && (
            <div className={`message ${message.role}`}>
              {message.role === "assistant" || message.role === "user" ? (
                <Markdown text={message.content} onFile={reveal} />
              ) : (
                message.content
              )}
            </div>
          )}
          {message.attachments.length > 0 && (
            <ul className="attachments sent" aria-label={t.attachments}>
              {message.attachments.map((attachment) => (
                <li key={attachment.name}>
                  <button
                    type="button"
                    className={attachment.kind === "image" ? "attachment image" : "attachment"}
                    title={t.showFolder}
                    onClick={() => revealAttachment(attachment)}
                  >
                    {attachment.kind === "image" && (
                      <img
                        src={`/api/chats/${chatId}/attachments/${encodeURIComponent(attachment.name)}`}
                        alt=""
                        loading="lazy"
                      />
                    )}
                    <AttachmentLabel attachment={attachment} />
                  </button>
                </li>
              ))}
            </ul>
          )}
          {(message.role === "assistant" || message.role === "user") && message.content ? (
            <div className="message-meta">
              <CopyButton text={message.content} />
              {message.role === "assistant" && canSpeak && (
                <SpeakButton text={message.content} shortcut={message.id === lastReply} />
              )}
              <MessageTime iso={message.created_at} />
            </div>
          ) : (
            <MessageTime iso={message.created_at} />
          )}
        </div>
      ),
    )}
    </>
  );
});

const openChat = (id: string) => dispatchEvent(new CustomEvent("open-chat-link", { detail: id }));

function ChatFrom({ id, title }: { id: string; title?: string }) {
  const t = useText(labels);
  return (
    <button type="button" className="ghost chat-from" onClick={() => openChat(id)}>
      <Icon name="chat" /> {t.fromChat} {title || t.deletedChat}
    </button>
  );
}

// Formats the full input only when expanded; tool inputs can be 20k chars and chats have hundreds.
function ToolLine({ body, resultURL, children }: { body: () => string; resultURL?: string; children: ReactNode }) {
  const t = useText(labels);
  const [open, setOpen] = useState(false);
  const [result, setResult] = useState<string>();
  useEffect(() => {
    if (!open || !resultURL || result !== undefined) return;
    api
      .get<{ tool_result: string }>(resultURL)
      .then((data) => setResult(data.tool_result))
      .catch((reason) => setResult(t.resultLoadError(reason)));
  }, [open, resultURL, result]);
  return (
    <details className="tool-line" onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary>{children}</summary>
      {open && <pre>{body()}</pre>}
      {open && resultURL && <pre aria-label={t.toolResult}>{result === undefined ? t.resultLoading : prettyJSON(result)}</pre>}
    </details>
  );
}

// Results above this size are likely to crowd the model's context.
const largeToolResult = 25_000;

function ToolResultBadge({ size, failed }: { size: number; failed: boolean }) {
  const t = useText(labels);
  const label = size < 1000 ? t.chars(size) : t.charsK((size / 1000).toFixed(1));
  return (
    <span className={`tool-result-size${failed ? " failed" : size > largeToolResult ? " large" : ""}`}>
      {failed ? `${t.failed} · ${label}` : label}
    </span>
  );
}

function prettyJSON(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    // Tool lines are stored as "<tool name> <json>".
    const space = text.indexOf(" ");
    return space > 0 && text[space + 1] === "{" ? `${text.slice(0, space)}\n${prettyJSON(text.slice(space + 1))}` : text;
  }
}

function approvalSummary(approval: ChatApproval) {
  let input = approval.input_json;
  try {
    input = JSON.stringify(JSON.parse(approval.input_json));
  } catch {}
  return `${approval.tool_name} ${input.length > 80 ? `${input.slice(0, 80)}…` : input}`;
}

function ApprovalCard({ approval, onAnswer }: { approval: ChatApproval; onAnswer: (allow: boolean) => void }) {
  const t = useText(labels);
  let input = approval.input_json;
  try {
    input = JSON.stringify(JSON.parse(approval.input_json), null, 2);
  } catch {}
  return (
    <section className="panel approval" aria-label={t.approvalRequest(approval.tool_name)}>
      <p>
        <strong>{t.allow(approval.tool_name)}</strong>
      </p>
      <pre className="mcp-config">
        <code>{input}</code>
      </pre>
      <div className="connector-buttons">
        <button className="primary" onClick={() => onAnswer(true)}>
          {t.allowOnce}
        </button>
        <button onClick={() => onAnswer(false)}>{t.deny}</button>
      </div>
    </section>
  );
}

function QuestionLine({ message }: { message: ChatMessage }) {
  const t = useText(labels);
  const questions = parseQuestions(message.content);
  if (!questions.length)
    return (
      <ToolLine body={() => prettyJSON(message.content)}>
        <Icon name="plug" /> <code>ask_user</code> <MessageTime iso={message.created_at} />
      </ToolLine>
    );
  return (
    <p className="question-done muted">
      <Icon name="list" /> {questions.length === 1 ? t.askedOne : t.askedMany(questions.length)}{" "}
      {questions.map((q) => q.header).join(", ")}
    </p>
  );
}

// Docked above the composer while open; sending a normal message dismisses it (the backend closes open questions).
function QuestionCard({
  message,
  disabled,
  onAnswer,
}: {
  message: ChatMessage;
  disabled: boolean;
  onAnswer: (answers: ChatAnswer[]) => void;
}) {
  const t = useText(labels);
  const questions = parseQuestions(message.content);
  const [answers, setAnswers] = useState<ChatAnswer[]>(() =>
    questions.map((q) => ({ header: q.header, selected: [], other: "" })),
  );
  const [step, setStep] = useState(0);
  const name = useId();
  const filled = (a: ChatAnswer | undefined) => Boolean(a?.selected.length || a?.other?.trim());
  const answered = (index: number) => filled(answers[index]);
  const ready = answers.every(filled);
  const q = questions[step];
  const last = step === questions.length - 1;

  function submit(list: ChatAnswer[]) {
    const missing = list.findIndex((a) => !filled(a));
    if (missing >= 0) return setStep(missing);
    if (!disabled)
      onAnswer(list.map((a) => ({ header: a.header, selected: a.selected, ...(a.other?.trim() ? { other: a.other.trim() } : {}) })));
  }

  function select(label: string) {
    const choose = (a: ChatAnswer) =>
      !q.multiple ? [label] : a.selected.includes(label) ? a.selected.filter((l) => l !== label) : [...a.selected, label];
    setAnswers((current) => current.map((a, i) => (i === step ? { ...a, selected: choose(a) } : a)));
  }

  // Pointer picks on single-choice questions move on; keyboard users confirm with Enter so arrow keys don't skip ahead.
  function advance(label: string) {
    const next = answers.map((a, i) => (i === step ? { ...a, selected: [label] } : a));
    setAnswers(next);
    if (last) submit(next);
    else setStep(step + 1);
  }

  const selected = answers[step].selected.length;
  return (
    <form
      className="panel approval question-card"
      aria-label={t.questionsFromAssistant}
      onSubmit={(event) => {
        event.preventDefault();
        if (!last) return answered(step) && setStep(step + 1);
        submit(answers);
      }}
    >
      {questions.length > 1 && (
        <div className="question-steps" role="tablist" aria-label={t.questionTabs}>
          {questions.map((item, index) => (
            <button
              key={index}
              type="button"
              role="tab"
              aria-selected={index === step}
              className={index === step ? "active" : undefined}
              onClick={() => setStep(index)}
            >
              {answered(index) && <Icon name="check" />} {item.header}
            </button>
          ))}
          <span className="muted">
            {step + 1} / {questions.length}
          </span>
        </div>
      )}
      <fieldset key={step} disabled={disabled}>
        <legend>
          {q.question}
          {q.multiple && <span className="question-badge">{t.chooseMany}</span>}
        </legend>
        {q.multiple && (
          <span className="muted question-hint" aria-live="polite">
            {t.selectedCount(selected)}
          </span>
        )}
        {q.options.map((option) => {
          const checked = answers[step].selected.includes(option.label);
          return (
            <label
              key={option.label}
              className={`question-option${checked ? " selected" : ""}`}
              onPointerUp={() => !q.multiple && !disabled && advance(option.label)}
            >
              <input
                type={q.multiple ? "checkbox" : "radio"}
                name={`${name}-${step}`}
                checked={checked}
                onChange={() => select(option.label)}
              />
              <span>
                <span className="question-label">{option.label}</span>
                {option.description && <span className="question-description">{option.description}</span>}
              </span>
            </label>
          );
        })}
        <input
          className="question-other"
          aria-label={t.otherAnswer(q.header)}
          placeholder={t.otherOptional}
          value={answers[step].other}
          onChange={(event) =>
            setAnswers((current) => current.map((a, i) => (i === step ? { ...a, other: event.target.value } : a)))
          }
        />
      </fieldset>
      <div className="connector-buttons">
        {step > 0 && (
          <button type="button" onClick={() => setStep(step - 1)}>
            {t.back}
          </button>
        )}
        <button className="primary" type="submit" disabled={last ? !ready || disabled : !answered(step)}>
          {last ? t.sendAnswers : t.next}
        </button>
      </div>
    </form>
  );
}

function parseQuestions(content: string): ChatQuestion[] {
  try {
    const questions = (JSON.parse(content) as { questions?: ChatQuestion[] }).questions;
    return Array.isArray(questions) ? questions : [];
  } catch {
    return [];
  }
}

type BrowsedFolder = { name: string; path: string };

function FolderDialog({
  current,
  onPick,
  onClose,
  onSettings,
}: {
  current: string | null;
  onPick: (folder: string | null) => Promise<void>;
  onClose: () => void;
  onSettings: () => void;
}) {
  const t = useText(labels);
  const [roots, setRoots] = useState<AllowedFolder[]>();
  const [trail, setTrail] = useState<BrowsedFolder[]>([]);
  const [folders, setFolders] = useState<BrowsedFolder[]>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>();
  const dialog = useRef<HTMLDialogElement>(null);
  const at = trail.at(-1);

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
    api.get<AllowedFolder[]>("/api/settings/folders").then(setRoots).catch(setError);
  }, []);

  useEffect(() => {
    if (!at) return;
    let current = true;
    setFolders(undefined);
    api
      .get<{ folders: BrowsedFolder[] }>(`/api/settings/folders/browse?path=${encodeURIComponent(at.path)}`)
      .then((result) => current && setFolders(result.folders))
      .catch((reason) => current && setError(reason));
    return () => {
      current = false;
    };
  }, [at?.path]);

  async function pick(folder: string | null) {
    setSaving(true);
    try {
      await onPick(folder);
    } catch (reason) {
      setError(reason);
      setSaving(false);
    }
  }

  return (
    <dialog ref={dialog} className="create-dialog" aria-labelledby="folder-dialog-title" onClose={onClose}>
      <section className="panel create create-modal">
        <div className="page-heading">
          <h2 id="folder-dialog-title">{t.chooseFolder}</h2>
          <IconButton type="button" className="ghost" icon="x" label={t.close} onClick={() => dialog.current?.close()} />
        </div>
        <nav className="folder-trail" aria-label={t.folderPath}>
          <button type="button" className="ghost" aria-current={at ? undefined : "location"} onClick={() => setTrail([])}>
            {t.folders}
          </button>
          {trail.map((folder, index) => (
            <span key={folder.path}>
              <span aria-hidden="true">/</span>
              <button
                type="button"
                className="ghost"
                title={folder.path}
                aria-current={index === trail.length - 1 ? "location" : undefined}
                onClick={() => setTrail(trail.slice(0, index + 1))}
              >
                {folder.name}
              </button>
            </span>
          ))}
        </nav>
        <ul className="folder-browser" aria-busy={at ? !folders : !roots}>
          {!at && (
            <li>
              <button type="button" className="ghost" disabled={saving} onClick={() => pick(null)}>
                <Icon name="folder" /> {t.scratchFolder}
                {current === null && <span className="muted">{t.current}</span>}
              </button>
            </li>
          )}
          {(at ? folders : roots?.map((root) => ({ name: root.path, path: root.path })))?.map((folder) => (
            <li key={folder.path}>
              <button type="button" className="ghost" onClick={() => (setError(undefined), setTrail([...trail, folder]))}>
                <Icon name="folder" /> {folder.name}
                {current === folder.path && <span className="muted">{t.current}</span>}
              </button>
            </li>
          ))}
          {at && folders?.length === 0 && <li className="muted">{t.noFolders}</li>}
        </ul>
        <small className="muted">
          {t.wantFolders}{" "}
          <a
            href="/settings"
            onClick={(event) => {
              event.preventDefault();
              dialog.current?.close();
              onSettings();
            }}
          >
            {t.addSettings}
          </a>
        </small>
        <ErrorText error={error} />
        <div className="modal-actions">
          <button type="button" onClick={() => dialog.current?.close()}>
            {t.cancel}
          </button>
          <button
            type="button"
            className="primary"
            disabled={!at || saving || at.path === current}
            onClick={() => at && pick(at.path)}
          >
            {t.useFolder}
          </button>
        </div>
      </section>
    </dialog>
  );
}

const connectorModel = (connectors: Connector[], id: string) =>
  connectors.find((c) => c.id === id && c.kind === "local")?.model ?? "";

const createChat = (body: { connector_id: string; model: string; folder: string | null; title?: string }) =>
  api.post<Chat>("/api/chats", body);

export async function createChatWithDefaults(connector: Connector, title?: string) {
  const defaults = await api.get<ChatDefaults>("/api/settings/defaults");
  return createChat({ connector_id: connector.id, model: startModel([connector], defaults, connector.id), folder: defaults.folder || null, title });
}

// The default model only applies to the default connector; other connectors start on their own model.
export const startModel = (connectors: Connector[], defaults: ChatDefaults | undefined, id: string) =>
  (id === defaults?.connector_id && defaults.model) || connectorModel(connectors, id);

export function NewChatDialog({
  connectors,
  title,
  inline,
  onClose,
  onCreated,
}: {
  connectors: Connector[];
  title?: string;
  // Render just the form, inside another dialog (Ask AI tab), instead of a modal of its own.
  inline?: boolean;
  onClose: () => void;
  onCreated: (chat: Chat) => void;
}) {
  const t = useText(labels);
  const [defaults, setDefaults] = useState<ChatDefaults>();
  const defaultModel = (id: string) => startModel(connectors, defaults, id);
  const [connectorID, setConnectorID] = useState(connectors[0]?.id ?? "");
  const [model, setModel] = useState(() => defaultModel(connectors[0]?.id ?? ""));
  const { models, loading: modelsLoading } = useModels(connectorID);

  useEffect(() => {
    if (!modelsLoading && model && model === defaults?.model && !models.some((m) => m.id === model)) {
      setModel(connectorModel(connectors, connectorID));
    }
  }, [models, modelsLoading]);
  const [folder, setFolder] = useState("");
  const [folders, setFolders] = useState<AllowedFolder[]>([]);
  const [error, setError] = useState<unknown>();
  const dialog = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
    api.get<AllowedFolder[]>("/api/settings/folders").then(setFolders).catch(setError);
    api
      .get<ChatDefaults>("/api/settings/defaults")
      .then((defaults) => {
        setDefaults(defaults);
        if (defaults.connector_id && connectors.some((c) => c.id === defaults.connector_id)) {
          setConnectorID(defaults.connector_id);
          setModel(startModel(connectors, defaults, defaults.connector_id));
        }
        if (defaults.folder) setFolder(defaults.folder);
      })
      .catch(setError);
  }, []);

  const [starting, setStarting] = useState(false);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (starting) return;
    setStarting(true);
    try {
      onCreated(await createChat({ connector_id: connectorID, model, folder: folder || null, title }));
    } catch (reason) {
      setError(reason);
    } finally {
      setStarting(false);
    }
  }

  const cancel = () => (inline ? onClose() : dialog.current?.close());
  const form = (
    <>
        <form onSubmit={submit}>
          <label>
            {t.connector}
            <Picker
              label={t.connector}
              empty={connectors.length === 0 ? t.noConnector : undefined}
              options={connectors.map((c) => ({ id: c.id, name: `${c.name} · ${c.kind}` }))}
              value={connectorID}
              onChange={(id) => {
                setConnectorID(id);
                setModel(defaultModel(id));
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
              pinKind={connectors.find((c) => c.id === connectorID)?.kind}
              onChange={setModel}
            />
          </div>
          <label className="wide">
            {t.folderLabel}
            <Picker
              label={t.folderLabel}
              empty={t.scratchFolder}
              options={folders.map((f) => ({ id: f.path, name: f.path }))}
              value={folder}
              onChange={setFolder}
            />
            {folders.length === 0 && <small>{t.addFolders}</small>}
          </label>
          <div className="modal-actions">
            <button type="button" onClick={cancel}>
              {t.cancel}
            </button>
            <button className="primary" disabled={!connectorID || starting}>
              {t.startChatButton}
            </button>
          </div>
        </form>
        <ErrorText error={error} />
    </>
  );
  if (inline) return <div className="create">{form}</div>;
  return (
    <dialog ref={dialog} className="create-dialog" aria-labelledby="new-chat-title" onClose={onClose}>
      <section className="panel create create-modal">
        <div className="page-heading">
          <h2 id="new-chat-title">{t.newChat}</h2>
          <IconButton type="button" className="ghost" icon="x" label={t.close} onClick={cancel} />
        </div>
        {form}
      </section>
    </dialog>
  );
}
