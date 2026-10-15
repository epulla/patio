import { useEffect, useId, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { Icon } from "./icons";

export type ModelOption = { id: string; label: string; description?: string };

const popWidth = 320;
const popHeight = 390;

const provider = (id: string) => (id.includes("/") ? id.slice(0, id.indexOf("/")) : "");
const shortLabel = (m: ModelOption) => (m.label === m.id && provider(m.id) ? m.id.slice(provider(m.id).length + 1) : m.label);

const pinsKey = "model-pins";

function loadPins(kind: string): string[] {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(pinsKey) ?? "{}")?.[kind];
    return Array.isArray(saved) ? saved.filter((id): id is string => typeof id === "string") : [];
  } catch {
    return [];
  }
}

function savePins(kind: string, ids: string[]) {
  let all: Record<string, string[]> = {};
  try {
    all = JSON.parse(localStorage.getItem(pinsKey) ?? "{}") ?? {};
  } catch {
    // Corrupt pins are dropped; they are a UI preference only.
  }
  localStorage.setItem(pinsKey, JSON.stringify({ ...all, [kind]: ids }));
}

export default function ModelPicker({
  models,
  value,
  loading,
  disabled,
  up,
  pinKind,
  onChange,
}: {
  models: ModelOption[];
  value: string;
  loading: boolean;
  disabled?: boolean;
  up?: boolean;
  /** Connector kind; pinned models are remembered per kind. */
  pinKind?: string;
  onChange: (id: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const [place, setPlace] = useState<CSSProperties>({});
  const [pins, setPins] = useState<string[]>([]);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const id = useId();

  const all = value && !models.some((m) => m.id === value) ? [...models, { id: value, label: value }] : models;
  const current = all.find((m) => m.id === value);
  const q = query.trim().toLowerCase();
  const visible = all.filter((m) => !q || [m.id, m.label, m.description ?? ""].some((text) => text.toLowerCase().includes(q)));
  const pinned = pins.flatMap((pin) => visible.filter((m) => m.id === pin));
  const providers = [...new Set(visible.map((m) => provider(m.id)))].map((name) => ({
    name,
    models: visible.filter((m) => provider(m.id) === name),
  }));
  // The unnamed group (e.g. "Default") has no header, so it stays above "Pinned".
  const groups = [
    ...providers.filter((group) => !group.name),
    ...(pinned.length ? [{ name: "Pinned", models: pinned }] : []),
    ...providers.filter((group) => group.name),
  ];
  const ordered = groups.flatMap((group) => group.models);
  const starts = groups.map((_, i) => groups.slice(0, i).reduce((n, group) => n + group.models.length, 0));
  const optionID = (index: number) => `${id}-option-${index}`;

  useEffect(() => {
    if (!open) return;
    const outside = (event: Event) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    const resize = () => setOpen(false);
    // The popup is fixed-positioned to escape dialog overflow, so any outside scroll would detach it.
    document.addEventListener("mousedown", outside);
    document.addEventListener("scroll", outside, true);
    addEventListener("resize", resize);
    return () => {
      document.removeEventListener("mousedown", outside);
      document.removeEventListener("scroll", outside, true);
      removeEventListener("resize", resize);
    };
  }, [open]);

  useEffect(() => {
    if (open) document.getElementById(optionID(active))?.scrollIntoView({ block: "nearest" });
  }, [open, active, query]);

  function show() {
    const rect = trigger.current!.getBoundingClientRect();
    const above = rect.top - 22;
    const below = innerHeight - rect.bottom - 22;
    const openUp = (up ? above < popHeight : below < popHeight) ? above > below : up;
    setPlace({
      ...(openUp ? { bottom: innerHeight - rect.top + 6 } : { top: rect.bottom + 6 }),
      ...(up
        ? { right: Math.max(16, innerWidth - rect.right) }
        : { left: Math.max(16, Math.min(rect.left, innerWidth - popWidth - 16)) }),
      maxHeight: openUp ? above : below,
    });
    setQuery("");
    setPins(pinKind ? loadPins(pinKind) : []);
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

  function togglePin(model: ModelOption) {
    if (!pinKind) return;
    const next = pins.includes(model.id) ? pins.filter((id) => id !== model.id) : [...pins, model.id];
    savePins(pinKind, next);
    setPins(next);
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
          className="model-pop"
          style={place}
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
            {groups.map((group, g) => (
              <li key={g} role="presentation">
                {group.name && <div className="model-group muted">{group.name}</div>}
                <ul role="group" aria-label={group.name || undefined}>
                  {group.models.map((model, m) => {
                    const index = starts[g] + m;
                    const isPinned = pins.includes(model.id);
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
                        <span className="model-text">
                          <span>{shortLabel(model)}</span>
                          {model.description && <span className="model-description muted">{model.description}</span>}
                        </span>
                        {pinKind && model.id && (
                          <button
                            type="button"
                            tabIndex={-1}
                            className="model-pin"
                            aria-label={`${isPinned ? "Unpin" : "Pin"} ${model.label}`}
                            aria-pressed={isPinned}
                            onClick={(event) => {
                              event.stopPropagation();
                              togglePin(model);
                            }}
                          >
                            <Icon name="pin" />
                          </button>
                        )}
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
