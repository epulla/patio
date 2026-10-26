import { useEffect, useRef, useState, type ReactNode } from "react";
import { api, type Chat, type Connector } from "./api";
import { ErrorText } from "./App";
import { Conversation, createChatWithDefaults, NewChatDialog, type PageContext } from "./Chats";
import { Icon } from "./icons";
import { useText } from "./i18n";

const text = {
  en: { form: "Form", ai: "Ask AI", starting: "Starting chat…", created: "Created", open: "Open", done: "Done" },
  es: { form: "Formulario", ai: "Preguntar a la IA", starting: "Iniciando chat…", created: "Creado", open: "Abrir", done: "Listo" },
};

export type CreatedItem = { id: string; label: string; href: string };

// Shared state of a create dialog's AI tab: the chat lives as long as the dialog, not the tab.
export function useAiCreate() {
  const [connectors, setConnectors] = useState<Connector[]>();
  const [chat, setChat] = useState<Chat>();
  const chatId = useRef<string | undefined>(undefined);
  const openedAt = useRef(new Date().toISOString());
  useEffect(() => {
    api.get<Connector[]>("/api/connectors").then(setConnectors).catch(() => setConnectors([]));
  }, []);
  // Called when the dialog closes: a chat nobody wrote in is deleted, any other stays like a normal chat.
  async function cleanup() {
    const id = chatId.current;
    if (!id) return;
    localStorage.removeItem(`chat-draft:${id}`);
    try {
      const detail = await api.get<{ messages: unknown[]; chat: Chat }>(`/api/chats/${id}`);
      if (!detail.messages.length && !detail.chat.running) await api.delete(`/api/chats/${id}`);
    } catch {
      // burnedout: assumed a failed cleanup just leaves the empty chat behind
    }
  }
  return {
    connectors,
    chat,
    openedAt: openedAt.current,
    setChat: (next: Chat) => {
      chatId.current = next.id;
      setChat(next);
    },
    cleanup,
  };
}

type AiCreate = ReturnType<typeof useAiCreate>;

// Items the AI created while the dialog is open, refreshed every few seconds while its chat exists.
export function useCreated(ai: AiCreate, load: (since: string) => Promise<CreatedItem[]>) {
  const [items, setItems] = useState<CreatedItem[]>([]);
  useEffect(() => {
    if (!ai.chat) return;
    const run = () => load(ai.openedAt).then(setItems).catch(() => undefined);
    run();
    const timer = setInterval(run, 3000);
    return () => clearInterval(timer);
  }, [ai.chat?.id]);
  return items;
}

// Form and Ask AI tabs; with no connector only the form shows. The last tab used is remembered per kind.
export function CreateTabs({ kind, ai, form, children }: { kind: string; ai: AiCreate; form: ReactNode; children: (back: () => void) => ReactNode }) {
  const t = useText(text);
  const key = `create-tab:${kind}`;
  const [tab, setTab] = useState(() => (localStorage.getItem(key) === "ai" ? "ai" : "form"));
  if (!ai.connectors?.length) return <>{form}</>;
  function choose(next: "form" | "ai") {
    setTab(next);
    localStorage.setItem(key, next);
  }
  return (
    <>
      <div className="tabs" role="tablist">
        <button type="button" role="tab" aria-selected={tab === "form"} onClick={() => choose("form")}>
          {t.form}
        </button>
        <button type="button" role="tab" aria-selected={tab === "ai"} onClick={() => choose("ai")}>
          <Icon name="sparkles" /> {t.ai}
        </button>
      </div>
      {tab === "form" ? form : children(() => choose("form"))}
    </>
  );
}

export function AskAI({
  ai,
  title,
  draft,
  context,
  created,
  onBack,
  onOpen,
  onDone,
}: {
  ai: AiCreate;
  title: string;
  draft: string;
  context?: PageContext;
  created: CreatedItem[];
  onBack: () => void;
  onOpen: (href: string) => void;
  onDone: () => void;
}) {
  const t = useText(text);
  const connectors = ai.connectors ?? [];
  const [choosing, setChoosing] = useState(false);
  const [error, setError] = useState<unknown>();
  const starting = useRef(false);

  function start(chat: Chat) {
    localStorage.setItem(`chat-draft:${chat.id}`, draft);
    ai.setChat(chat);
  }

  // One connector starts right away; several open the usual picker with the default connector selected.
  useEffect(() => {
    if (ai.chat || starting.current || !connectors.length) return;
    starting.current = true;
    if (connectors.length > 1) return setChoosing(true);
    createChatWithDefaults(connectors[0], title).then(start).catch(setError);
  }, [ai.chat, connectors.length]);

  return (
    <div className="ai-create">
      {created.length > 0 && (
        <ul className="ai-created" role="status">
          {created.map((item) => (
            <li key={item.id}>
              <Icon name="check" /> {t.created} {item.label} ·{" "}
              <button type="button" className="link" onClick={() => onOpen(item.href)}>
                {t.open}
              </button>
            </li>
          ))}
        </ul>
      )}
      {ai.chat ? (
        <div className="ai-create-chat">
          <Conversation key={ai.chat.id} id={ai.chat.id} refresh={0} connectors={connectors} compact focused context={context} onChanged={() => undefined} />
        </div>
      ) : (
        !error && <p className="muted" role="status">{t.starting}</p>
      )}
      <ErrorText error={error} />
      <div className="modal-actions">
        <button type="button" className="primary" onClick={onDone}>
          {t.done}
        </button>
      </div>
      {choosing && (
        <NewChatDialog
          connectors={connectors}
          title={title}
          onClose={() => {
            setChoosing(false);
            if (!ai.chat) {
              starting.current = false;
              onBack();
            }
          }}
          onCreated={(chat) => {
            setChoosing(false);
            start(chat);
          }}
        />
      )}
    </div>
  );
}
