import { useText } from "./i18n";

const text = {
  en: { label: "AI agent" },
  es: { label: "Agente de IA" },
};

export default function AIBadge({ isAI }: { isAI: boolean }) {
  const t = useText(text);
  if (!isAI) return null;
  return (
    <span className="ai-badge" title={t.label} aria-label={t.label}>
      AI
    </span>
  );
}
