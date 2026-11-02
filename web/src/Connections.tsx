import type { ReactNode } from "react";
import { navigate } from "./App";
import { useText } from "./i18n";
import Telegram from "./Telegram";

const text = {
  en: {
    title: "Connections", connectors: "AI", mcp: "Tools (MCP)", channels: "Channels",
    channelsIntro: "Talk to Patio from messaging apps. Messages go into normal chats with your default bot.",
  },
  es: {
    title: "Conexiones", connectors: "IA", mcp: "Herramientas (MCP)", channels: "Canales",
    channelsIntro: "Habla con Patio desde apps de mensajería. Los mensajes llegan a chats normales con tu bot por defecto.",
  },
};

type ConnectionsTab = "connectors" | "mcp" | "channels";

export function ConnectionsHeading({ actions, tab }: { actions?: ReactNode; tab: ConnectionsTab }) {
  const t = useText(text);
  return (
    <>
      <div className="page-heading">
        <div>
          <p className="eyebrow">AI</p>
          <h1>{t.title}</h1>
        </div>
        {actions && <div className="actions">{actions}</div>}
      </div>
      <div className="tabs" role="tablist">
        {(["connectors", "mcp", "channels"] as const).map((value) => (
          <button key={value} role="tab" aria-selected={tab === value} data-tour={`tab-${value}`} onClick={() => navigate(`/ai/${value}`)}>
            {t[value]}
          </button>
        ))}
      </div>
    </>
  );
}

export function Channels() {
  const t = useText(text);
  return (
    <div className="stack">
      <ConnectionsHeading tab="channels" />
      <p className="muted">{t.channelsIntro}</p>
      <Telegram />
    </div>
  );
}
