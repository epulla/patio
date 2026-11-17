import Picker from "./Picker";
import { useText } from "./i18n";
import type { ModelOption } from "./ModelPicker";

const text = {
  en: {
    label: "Reasoning effort",
    default: "Default",
    hint: "The model decides",
    hintLevel: (level: string) => `The model decides (${level})`,
    levels: { none: "None", minimal: "Minimal", low: "Low", medium: "Medium", high: "High", xhigh: "Extra high", max: "Max", ultra: "Ultra", thinking: "Thinking" } as Record<string, string>,
  },
  es: {
    label: "Esfuerzo de razonamiento",
    default: "Predeterminado",
    hint: "El modelo decide",
    hintLevel: (level: string) => `El modelo decide (${level})`,
    levels: { none: "Ninguno", minimal: "Mínimo", low: "Bajo", medium: "Medio", high: "Alto", xhigh: "Muy alto", max: "Máximo", ultra: "Ultra", thinking: "Razonamiento" } as Record<string, string>,
  },
};

/** Lists the model's own effort levels; renders nothing when the model has none or they are unknown. */
export default function EffortPicker({ id, label, model, value, disabled, onChange }: { id?: string; label?: string; model?: ModelOption; value: string; disabled?: boolean; onChange: (effort: string) => void }) {
  const t = useText(text);
  const efforts = model?.efforts;
  if (!efforts?.length) return null;
  const name = (level: string) => t.levels[level] ?? level.charAt(0).toUpperCase() + level.slice(1);
  const fallback = model?.default_effort ? t.hintLevel(name(model.default_effort)) : t.hint;
  const picker = (
    <Picker
      id={id}
      label={t.label}
      options={[{ id: "", name: t.default, description: fallback }, ...efforts.map((level) => ({ id: level, name: name(level) }))]}
      value={efforts.includes(value) ? value : ""}
      disabled={disabled}
      onChange={onChange}
    />
  );
  return label ? (
    <label>
      {label}
      {picker}
    </label>
  ) : (
    picker
  );
}
