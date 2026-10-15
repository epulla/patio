import { useEffect, useId, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { Icon } from "./icons";

const popHeight = 280;

export type PickerOption = { id: string; name: string; color?: string };

export default function Picker({
  label,
  options: items,
  empty,
  value,
  onChange,
}: {
  label: string;
  options: PickerOption[];
  /** Label of an extra option with id "", e.g. "No type". */
  empty?: string;
  value: string;
  onChange: (id: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [place, setPlace] = useState<CSSProperties>({});
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const id = useId();

  const options: PickerOption[] = empty === undefined ? items : [{ id: "", name: empty }, ...items];
  const current = options.find((option) => option.id === value);
  const dots = items.some((option) => option.color);
  const text = (option?: PickerOption) =>
    dots ? (
      <span
        className={option?.color ? "type" : "type type-none"}
        style={{ "--type-color": option?.color ?? "transparent" } as CSSProperties}
      >
        {option?.name}
      </span>
    ) : (
      <span>{option?.name}</span>
    );
  const optionID = (index: number) => `${id}-option-${index}`;

  useEffect(() => {
    if (!open) return;
    const outside = (event: Event) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    const resize = () => setOpen(false);
    // Fixed-positioned to escape dialog overflow, so any outside scroll would detach it.
    document.addEventListener("mousedown", outside);
    document.addEventListener("scroll", outside, true);
    addEventListener("resize", resize);
    list.current?.focus();
    return () => {
      document.removeEventListener("mousedown", outside);
      document.removeEventListener("scroll", outside, true);
      removeEventListener("resize", resize);
    };
  }, [open]);

  useEffect(() => {
    if (open) document.getElementById(optionID(active))?.scrollIntoView({ block: "nearest" });
  }, [open, active]);

  function show() {
    const rect = trigger.current!.getBoundingClientRect();
    const above = rect.top - 22;
    const below = innerHeight - rect.bottom - 22;
    const up = below < popHeight && above > below;
    setPlace({
      ...(up ? { bottom: innerHeight - rect.top + 6 } : { top: rect.bottom + 6 }),
      left: rect.left,
      width: rect.width,
      maxHeight: Math.min(popHeight, up ? above : below),
    });
    setActive(Math.max(0, options.findIndex((option) => option.id === value)));
    setOpen(true);
  }

  function close() {
    setOpen(false);
    trigger.current?.focus();
  }

  function pick(typeID: string) {
    if (typeID !== value) onChange(typeID);
    close();
  }

  function triggerKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    event.preventDefault();
    show();
  }

  function listKeyDown(event: KeyboardEvent<HTMLUListElement>) {
    if (event.key === "Escape") {
      event.stopPropagation();
      return close();
    }
    if (event.key === "Tab") return setOpen(false);
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      return pick(options[active].id);
    }
    const moves: Record<string, number> = {
      ArrowDown: (active + 1) % options.length,
      ArrowUp: (active - 1 + options.length) % options.length,
      Home: 0,
      End: options.length - 1,
    };
    if (!(event.key in moves)) return;
    event.preventDefault();
    setActive(moves[event.key]);
  }

  return (
    <div
      className="picker"
      ref={root}
      onBlur={(event) => {
        if (!root.current?.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <button
        ref={trigger}
        type="button"
        className="picker-trigger"
        aria-label={`${label}: ${current?.name ?? "none"}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => (open ? close() : show())}
        onKeyDown={triggerKeyDown}
      >
        {text(current)}
        <Icon name="chevronDown" />
      </button>
      {open && (
        <ul
          ref={list}
          id={`${id}-list`}
          className="picker-pop"
          role="listbox"
          tabIndex={-1}
          aria-label={label}
          aria-activedescendant={optionID(active)}
          style={place}
          onKeyDown={listKeyDown}
        >
          {options.map((option, index) => (
            <li
              key={option.id}
              id={optionID(index)}
              role="option"
              aria-selected={option.id === value}
              data-active={index === active || undefined}
              onMouseMove={() => setActive(index)}
              onClick={() => pick(option.id)}
            >
              {text(option)}
              {option.id === value && <Icon name="check" />}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
