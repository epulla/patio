import { useEffect, useId, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import type { TicketType } from "./api";
import { Icon } from "./icons";

const popHeight = 280;

const swatch = (color?: string) => ({ "--type-color": color ?? "transparent" }) as CSSProperties;

export default function TypePicker({
  types,
  value,
  onChange,
}: {
  types: TicketType[];
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

  const options = [{ id: "", name: "No type", color: "" }, ...types];
  const current = types.find((type) => type.id === value);
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
    setActive(Math.max(0, options.findIndex((type) => type.id === value)));
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
      className="type-picker"
      ref={root}
      onBlur={(event) => {
        if (!root.current?.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <button
        ref={trigger}
        type="button"
        className="type-trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => (open ? close() : show())}
        onKeyDown={triggerKeyDown}
      >
        <span className={current ? "type" : "type type-none"} style={swatch(current?.color)}>
          {current ? current.name : "No type"}
        </span>
        <Icon name="chevronDown" />
      </button>
      {open && (
        <ul
          ref={list}
          id={`${id}-list`}
          className="type-pop"
          role="listbox"
          tabIndex={-1}
          aria-label="Ticket type"
          aria-activedescendant={optionID(active)}
          style={place}
          onKeyDown={listKeyDown}
        >
          {options.map((type, index) => (
            <li
              key={type.id}
              id={optionID(index)}
              role="option"
              aria-selected={type.id === value}
              data-active={index === active || undefined}
              onMouseMove={() => setActive(index)}
              onClick={() => pick(type.id)}
            >
              <span className={type.id ? "type" : "type type-none"} style={swatch(type.color)}>
                {type.name}
              </span>
              {type.id === value && <Icon name="check" />}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
