type DueTier = "far" | "near" | "today" | "overdue";

const plural = (n: number) => `${n} day${n === 1 ? "" : "s"}`;

export function dueInfo(date: string, today = new Date()): { tier: DueTier; label: string; tip: string } {
  const [year, month, day] = date.slice(0, 10).split("-").map(Number);
  const due = new Date(year, month - 1, day);
  const start = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  // Rounded so a DST shift (23 or 25 hour day) still counts as one day.
  const days = Math.round((due.getTime() - start.getTime()) / 86_400_000);
  const when = due.toLocaleDateString("en-US", { weekday: "short", month: "short", day: "numeric" });
  if (days < 0) return { tier: "overdue", label: `${-days}d overdue`, tip: `Due ${when} · ${plural(-days)} overdue` };
  if (days === 0) return { tier: "today", label: "Today", tip: `Due today (${when})` };
  if (days === 1) return { tier: "near", label: "Tomorrow", tip: `Due tomorrow (${when})` };
  return { tier: days <= 3 ? "near" : "far", label: `${days}d left`, tip: `Due ${when} · ${plural(days)} left` };
}

export default function DueBadge({ date }: { date: string }) {
  const { tier, label, tip } = dueInfo(date);
  return (
    <span className={`due-badge ${tier} tip-up`} data-tip={tip}>
      {label}
      <span className="sr-only"> ({tip})</span>
    </span>
  );
}
