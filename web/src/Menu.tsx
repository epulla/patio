import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

export default function Menu({
  label,
  button,
  buttonClassName,
  children,
}: {
  label: string;
  button: ReactNode;
  buttonClassName?: string;
  children: (close: (focusTrigger?: boolean) => void) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const items = () => [
    ...(root.current?.querySelectorAll<HTMLElement>('[role="menuitem"], [role="menuitemradio"], [role="menuitemcheckbox"]') ?? []),
  ];

  useEffect(() => {
    if (!open) return;
    (items().find((item) => item.getAttribute("aria-checked") === "true") ?? items()[0])?.focus();
    const outside = (event: globalThis.MouseEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", outside);
    return () => document.removeEventListener("mousedown", outside);
  }, [open]);

  useLayoutEffect(() => {
    const el = list.current;
    if (!open || !el) return;
    const rect = el.getBoundingClientRect();
    const overflow = rect.right - (document.documentElement.clientWidth - 8);
    if (overflow > 0) el.style.translate = `${-overflow}px 0`;
    else if (rect.left < 8) el.style.translate = `${8 - rect.left}px 0`;
  }, [open]);

  function close(focusTrigger = true) {
    setOpen(false);
    if (focusTrigger) trigger.current?.focus();
  }

  function keyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (!open) return;
    if (event.key === "Escape") return close();
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    event.preventDefault();
    const list = items();
    const index = list.indexOf(document.activeElement as HTMLElement);
    list[(index + (event.key === "ArrowDown" ? 1 : -1) + list.length) % list.length].focus();
  }

  return (
    <div
      className="menu"
      ref={root}
      onKeyDown={keyDown}
      onBlur={(event) => {
        if (!root.current?.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <button
        ref={trigger}
        type="button"
        className={buttonClassName}
        aria-label={buttonClassName?.includes("icon") ? label : undefined}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        {button}
      </button>
      {open && (
        <div
          ref={list}
          className="menu-list compact"
          role="menu"
          aria-label={label}
          // WebKit doesn't focus clicked buttons, so the press would blur the menu and close it before the click.
          onMouseDown={(event) => event.preventDefault()}
        >
          {children(close)}
        </div>
      )}
    </div>
  );
}
