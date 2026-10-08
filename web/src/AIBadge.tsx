export default function AIBadge({ isAI }: { isAI: boolean }) {
  if (!isAI) return null;
  return (
    <span className="ai-badge" title="AI agent" aria-label="AI agent">
      AI
    </span>
  );
}
