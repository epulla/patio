import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { Icon } from "./icons";

export type ModelOption = { id: string; label: string; description?: string };

const provider = (id: string) => (id.includes("/") ? id.slice(0, id.indexOf("/")) : "");
const shortLabel = (m: ModelOption) => (m.label === m.id && provider(m.id) ? m.id.slice(provider(m.id).length + 1) : m.label);

export default function ModelPicker({
  models,
  value,
  loading,
  disabled,
  up,
  onChange,
}: {
  models: ModelOption[];
  value: string;
  loading: boolean;
  disabled?: boolean;
  up?: boolean;
  onChange: (id: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const id = useId();

  const all = value && !models.some((m) => m.id === value) ? [...models, { id: value, label: value }] : models;
  const current = all.find((m) => m.id === value);
  const q = query.trim().toLowerCase();
  const visible = all.filter((m) => !q || [m.id, m.label, m.description ?? ""].some((text) => text.toLowerCase().includes(q)));
  const groups = [...new Set(visible.map((m) => provider(m.id)))].map((name) => ({
    name,
    models: visible.filter((m) => provider(m.id) === name),
  }));
  const ordered = groups.flatMap((group) => group.models);
  const optionID = (index: number) => `${id}-option-${index}`;

  useEffect(() => {
    if (!open) return;
    const outside = (event: MouseEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", outside);
    return () => document.removeEventListener("mousedown", outside);
  }, [open]);

  useEffect(() => {
    if (open) document.getElementById(optionID(active))?.scrollIntoView({ block: "nearest" });
  }, [open, active, query]);

  function show() {
    setQuery("");
    setActive(Math.max(0, ordered.findIndex((m) => m.id === value)));
    setOpen(true);
  }

  function close() {
    setOpen(false);
    trigger.current?.focus();
  }

  function pick(model: ModelOption) {
    if (model.id !== value) onChange(model.id);
    close();
  }

  function keyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape") {
      event.stopPropagation();
      return close();
    }
    if (event.key === "Enter") {
      event.preventDefault();
      if (ordered[active]) pick(ordered[active]);
      return;
    }
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    event.preventDefault();
    if (ordered.length) setActive((active + (event.key === "ArrowDown" ? 1 : -1) + ordered.length) % ordered.length);
  }

  return (
    <div
      className="model-picker"
      ref={root}
      onBlur={(event) => {
        if (!root.current?.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <button
        ref={trigger}
        type="button"
        className={up ? "model-trigger tip-up" : "model-trigger"}
        aria-label={`Model: ${loading ? "loading" : current ? current.label : "none"}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-busy={loading}
        data-tip={open ? undefined : current?.id || undefined}
        disabled={disabled || loading}
        onClick={() => (open ? close() : show())}
      >
        <span className="model-trigger-label">{loading ? "Loading models…" : current ? shortLabel(current) : "Choose a model"}</span>
        <Icon name="chevronDown" />
      </button>
      {open && (
        <div
          className={up ? "model-pop up" : "model-pop"}
          // Keep focus in the search box when clicking options or padding.
          onMouseDown={(event) => event.target instanceof HTMLInputElement || event.preventDefault()}
        >
          <input
            autoFocus
            role="combobox"
            aria-label="Search models"
            aria-expanded="true"
            aria-controls={`${id}-list`}
            aria-activedescendant={ordered[active] ? optionID(active) : undefined}
            placeholder="Search models"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setActive(0);
            }}
            onKeyDown={keyDown}
          />
          <ul id={`${id}-list`} role="listbox" aria-label="Models">
            {ordered.length === 0 && <li className="model-empty muted">No models match “{query.trim()}”</li>}
            {groups.map((group) => (
              <li key={group.name} role="presentation">
                {group.name && <div className="model-group muted">{group.name}</div>}
                <ul role="group" aria-label={group.name || undefined}>
                  {group.models.map((model) => {
                    const index = ordered.indexOf(model);
                    return (
                      <li
                        key={model.id}
                        id={optionID(index)}
                        role="option"
                        aria-selected={model.id === value}
                        data-active={index === active || undefined}
                        onMouseMove={() => setActive(index)}
                        onClick={() => pick(model)}
                      >
                        <span className="model-check">{model.id === value && <Icon name="check" />}</span>
                        <span className="model-name">{shortLabel(model)}</span>
                        {model.description && <span className="model-description muted">{model.description}</span>}
                      </li>
                    );
                  })}
                </ul>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
