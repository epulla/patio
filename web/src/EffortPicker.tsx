import Picker from "./Picker";
import { useText } from "./i18n";
import type { Effort } from "./api";

const text = {
  en: {
    label: "Reasoning effort",
    levels: { default: "Default effort", small: "Low effort", medium: "Medium effort", high: "High effort", xhigh: "Extra high effort" },
    hint: "Unsupported levels use the closest one the model has",
  },
  es: {
    label: "Esfuerzo de razonamiento",
    levels: { default: "Esfuerzo predeterminado", small: "Esfuerzo bajo", medium: "Esfuerzo medio", high: "Esfuerzo alto", xhigh: "Esfuerzo muy alto" },
    hint: "Los niveles no compatibles usan el más cercano del modelo",
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
