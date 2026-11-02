import type { ReactNode } from "react";
import { navigate } from "./App";
import { useText } from "./i18n";

const text = {
  en: { title: "Connections", connectors: "AI", mcp: "Tools (MCP)", channels: "Channels" },
  es: { title: "Conexiones", connectors: "IA", mcp: "Herramientas (MCP)", channels: "Canales" },
};

export type ConnectionsTab = keyof Omit<typeof text.en, "title">;

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
          <button key={value} role="tab" aria-selected={tab === value} onClick={() => navigate(`/ai/${value}`)}>
            {t[value]}
          </button>
        ))}
      </div>
    </>
  );
}
