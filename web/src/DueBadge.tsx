import { lang, locale, useText } from "./i18n";

const text = {
  en: {
    day: (n: number) => `${n} day${n === 1 ? "" : "s"}`,
    overdue: (n: number) => `${n}d overdue`,
    overdueTip: (when: string, n: number) => `Due ${when} · ${n} overdue`,
    today: "Today",
    dueToday: (when: string) => `Due today (${when})`,
    tomorrow: "Tomorrow",
    dueTomorrow: (when: string) => `Due tomorrow (${when})`,
    left: (n: number) => `${n}d left`,
    leftTip: (when: string, n: number) => `Due ${when} · ${n} left`,
    noDueDate: "No due date",
  },
  es: {
    day: (n: number) => `${n} día${n === 1 ? "" : "s"}`,
    overdue: (n: number) => `Vencido hace ${n} d`,
    overdueTip: (when: string, n: number) => `Vence el ${when} · ${n} de atraso`,
    today: "Hoy",
    dueToday: (when: string) => `Vence hoy (${when})`,
    tomorrow: "Mañana",
    dueTomorrow: (when: string) => `Vence mañana (${when})`,
    left: (n: number) => `Quedan ${n} d`,
    leftTip: (when: string, n: number) => `Vence el ${when} · quedan ${n}`,
    noDueDate: "Sin fecha de vencimiento",
  },
};

type DueTier = "far" | "near" | "today" | "overdue";

export function dueInfo(date: string, today = new Date()): { tier: DueTier; label: string; tip: string } {
  const [year, month, day] = date.slice(0, 10).split("-").map(Number);
  const due = new Date(year, month - 1, day);
  const start = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  // Rounded so a DST shift (23 or 25 hour day) still counts as one day.
  const days = Math.round((due.getTime() - start.getTime()) / 86_400_000);
  const when = due.toLocaleDateString(locale(), { weekday: "short", month: "short", day: "numeric" });
  const t = text[lang()];
  if (days < 0) return { tier: "overdue", label: t.overdue(-days), tip: t.overdueTip(when, -days) };
  if (days === 0) return { tier: "today", label: t.today, tip: t.dueToday(when) };
  if (days === 1) return { tier: "near", label: t.tomorrow, tip: t.dueTomorrow(when) };
  return { tier: days <= 3 ? "near" : "far", label: t.left(days), tip: t.leftTip(when, days) };
}

// An empty native date input shows a faded placeholder date, which reads like a real value.
export function DueDateInput({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const t = useText(text);
  return (
    <span className="date-input" data-empty={value ? undefined : ""}>
      <input type="date" value={value} onChange={(event) => onChange(event.target.value)} />
      {!value && (
        <span className="date-empty" aria-hidden="true">
          {t.noDueDate}
        </span>
      )}
    </span>
  );
}

export default function DueBadge({ date }: { date: string }) {
  useText(text);
  const { tier, label, tip } = dueInfo(date);
  return (
    <span className={`due-badge ${tier} tip-up`} data-tip={tip}>
      {label}
      <span className="sr-only"> ({tip})</span>
    </span>
  );
}
