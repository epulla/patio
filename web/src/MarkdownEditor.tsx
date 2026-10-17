import { useLayoutEffect, useRef, useState, type FocusEvent, type KeyboardEvent, type MouseEvent } from "react";
import { IconButton } from "./icons";
import Markdown from "./Markdown";
import { useMentions } from "./mentions";

const linePrefix = /^(#{1,3} |[-*] |\d+\. )/;

export default function MarkdownEditor({
  value,
  onChange,
  onBlur,
  label,
}: {
  value: string;
  onChange: (value: string) => void;
  onBlur?: () => void;
  label: string;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const [preview, setPreview] = useState(false);
  const mentions = useMentions(ref, onChange);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [value, preview]);

  function edit(start: number, end: number, text: string, selectFrom: number, selectTo: number) {
    onChange(value.slice(0, start) + text + value.slice(end));
    requestAnimationFrame(() => {
      ref.current?.focus();
      ref.current?.setSelectionRange(selectFrom, selectTo);
    });
  }

  function wrap(before: string, after: string, placeholder: string) {
    const el = ref.current;
    if (!el) return;
    const { selectionStart: start, selectionEnd: end } = el;
    const selected = value.slice(start, end) || placeholder;
    const from = start + before.length;
    edit(start, end, before + selected + after, from, from + selected.length);
  }

  function prefixLines(prefix: (index: number) => string) {
    const el = ref.current;
    if (!el) return;
    const start = value.lastIndexOf("\n", el.selectionStart - 1) + 1;
    const end = el.selectionEnd;
    const text = value
      .slice(start, end)
      .split("\n")
      .map((line, index) => prefix(index) + line.replace(linePrefix, ""))
      .join("\n");
    edit(start, end, text, start + text.length, start + text.length);
  }

  // Insert `@`/`#` at the caret (after a space if needed) and open its suggestions.
  function insertTrigger(char: "@" | "#") {
    const el = ref.current;
    if (!el) return;
    const { selectionStart: start, selectionEnd: end } = el;
    const text = (start > 0 && !/\s/.test(value[start - 1]) ? " " : "") + char;
    edit(start, end, text, start + text.length, start + text.length);
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (mentions.onKeyDown(event)) return;
    if (!(event.metaKey || event.ctrlKey)) return;
    const key = event.key.toLowerCase();
    if (key === "b") wrap("**", "**", "bold");
    else if (key === "i") wrap("_", "_", "italic");
    else if (key === "k") wrap("[", "](https://)", "link");
    else return;
    event.preventDefault();
  }

  // Keep focus in the textarea so toolbar clicks don't trigger onBlur saves.
  const keepFocus = (event: MouseEvent) => event.preventDefault();

  function blur(event: FocusEvent<HTMLDivElement>) {
    if (!event.currentTarget.contains(event.relatedTarget)) onBlur?.();
  }

  return (
    <div className="md-editor" onBlur={blur}>
      <div className="md-toolbar" role="toolbar" aria-label={`${label} formatting`}>
        {!preview && (
          <>
            <select
              className="md-heading"
              aria-label="Text style"
              value=""
              onChange={(event) => {
                const level = Number(event.target.value);
                prefixLines(() => (level ? "#".repeat(level) + " " : ""));
              }}
            >
              <option value="" disabled hidden>
                Text style
              </option>
              <option value="0">Normal</option>
              <option value="1">Title</option>
              <option value="2">Heading</option>
              <option value="3">Subheading</option>
            </select>
            <span className="md-sep" aria-hidden="true" />
            <IconButton type="button" className="ghost" icon="bold" label="Bold ⌘B" onMouseDown={keepFocus} onClick={() => wrap("**", "**", "bold")} />
            <IconButton type="button" className="ghost" icon="italic" label="Italic ⌘I" onMouseDown={keepFocus} onClick={() => wrap("_", "_", "italic")} />
            <IconButton type="button" className="ghost" icon="code" label="Code" onMouseDown={keepFocus} onClick={() => wrap("`", "`", "code")} />
            <IconButton type="button" className="ghost" icon="link" label="Link ⌘K" onMouseDown={keepFocus} onClick={() => wrap("[", "](https://)", "link")} />
            <span className="md-sep" aria-hidden="true" />
            <IconButton type="button" className="ghost" icon="list" label="Bulleted list" onMouseDown={keepFocus} onClick={() => prefixLines(() => "- ")} />
            <IconButton type="button" className="ghost" icon="listOrdered" label="Numbered list" onMouseDown={keepFocus} onClick={() => prefixLines((index) => `${index + 1}. `)} />
            <span className="md-sep" aria-hidden="true" />
            <IconButton type="button" className="ghost" icon="sparkles" label="Mention a bot (@)" onMouseDown={keepFocus} onClick={() => insertTrigger("@")} />
            <IconButton type="button" className="ghost" icon="ticket" label="Link a ticket (#)" onMouseDown={keepFocus} onClick={() => insertTrigger("#")} />
          </>
        )}
        <div className="md-tabs">
          <button type="button" aria-pressed={!preview} onMouseDown={keepFocus} onClick={() => setPreview(false)}>
            Write
          </button>
          <button type="button" aria-pressed={preview} onMouseDown={keepFocus} onClick={() => setPreview(true)}>
            Preview
          </button>
        </div>
      </div>
      {preview ? (
        <div className="md-preview">
          {value.trim() ? <Markdown text={value} /> : <p>Nothing to preview</p>}
        </div>
      ) : (
        <textarea
          ref={ref}
          aria-label={label}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={onKeyDown}
          onSelect={() => void mentions.update()}
          onBlur={mentions.close}
          placeholder="Markdown supported · @ to mention a bot · # to link a ticket"
          aria-autocomplete="list"
          aria-expanded={mentions.open}
        />
      )}
      {!preview && mentions.popup}
    </div>
  );
}
