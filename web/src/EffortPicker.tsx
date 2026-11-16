import Picker from "./Picker";
import { useText } from "./i18n";
import type { Effort } from "./api";

const text = {
  en: {
    label: "Reasoning effort",
    levels: { default: "Default", small: "Low", medium: "Medium", high: "High", xhigh: "Extra high" },
    hint: "The model decides",
  },
  es: {
    label: "Esfuerzo de razonamiento",
    levels: { default: "Predeterminado", small: "Bajo", medium: "Medio", high: "Alto", xhigh: "Muy alto" },
    hint: "El modelo decide",
  },
};

const levels: Effort[] = ["default", "small", "medium", "high", "xhigh"];

export default function EffortPicker({ id, value, disabled, onChange }: { id?: string; value: Effort; disabled?: boolean; onChange: (effort: Effort) => void }) {
  const t = useText(text);
  return (
    <Picker
      id={id}
      label={t.label}
      options={levels.map((level) => ({ id: level, name: t.levels[level], description: level === "default" ? t.hint : undefined }))}
      value={value}
      disabled={disabled}
      onChange={(level) => onChange(level as Effort)}
    />
  );
}
