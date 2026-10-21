import { useLayoutEffect, useRef, useState, type FocusEvent, type KeyboardEvent, type MouseEvent } from "react";
import { IconButton } from "./icons";
import Markdown from "./Markdown";
import { useMentions } from "./mentions";
import { useText } from "./i18n";

const text = {
  en: { formatting: "formatting", style: "Text style", normal: "Normal", title: "Title", heading: "Heading", subheading: "Subheading", bold: "Bold", italic: "Italic", code: "Code", link: "Link", bullets: "Bulleted list", numbered: "Numbered list", mention: "Mention a bot (@)", ticket: "Link a ticket (#)", write: "Write", preview: "Preview", nothing: "Nothing to preview", placeholder: "Markdown supported · @ to mention a bot · # to link a ticket" },
  es: { formatting: "formato", style: "Estilo de texto", normal: "Normal", title: "Título", heading: "Encabezado", subheading: "Subtítulo", bold: "Negrita", italic: "Cursiva", code: "Código", link: "Enlace", bullets: "Lista con viñetas", numbered: "Lista numerada", mention: "Mencionar un bot (@)", ticket: "Enlazar un ticket (#)", write: "Escribir", preview: "Vista previa", nothing: "Nada que previsualizar", placeholder: "Markdown disponible · @ para mencionar un bot · # para enlazar un ticket" },
};

const linePrefix = /^(#{1,3} |[-*] |\d+\. )/;

export default function MarkdownEditor({
  value,
  onChange,
  onBlur,
  onSubmit,
  label,
}: {
  value: string;
  onChange: (value: string) => void;
  onBlur?: () => void;
  onSubmit?: () => void;
  label: string;
}) {
  const t = useText(text);
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
    else if (key === "enter" && onSubmit) onSubmit();
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
      <div className="md-toolbar" role="toolbar" aria-label={`${label} ${t.formatting}`}>
        {!preview && (
          <>
            <select
              className="md-heading"
              aria-label={t.style}
              value=""
              onChange={(event) => {
                const level = Number(event.target.value);
                prefixLines(() => (level ? "#".repeat(level) + " " : ""));
              }}
            >
              <option value="" disabled hidden>
                {t.style}
              </option>
              <option value="0">{t.normal}</option>
              <option value="1">{t.title}</option>
              <option value="2">{t.heading}</option>
              <option value="3">{t.subheading}</option>
            </select>
            <span className="md-sep" aria-hidden="true" />
            <IconButton type="button" className="ghost" icon="bold" label={`${t.bold} ⌘B`} onMouseDown={keepFocus} onClick={() => wrap("**", "**", "bold")} />
            <IconButton type="button" className="ghost" icon="italic" label={`${t.italic} ⌘I`} onMouseDown={keepFocus} onClick={() => wrap("_", "_", "italic")} />
            <IconButton type="button" className="ghost" icon="code" label={t.code} onMouseDown={keepFocus} onClick={() => wrap("`", "`", "code")} />
            <IconButton type="button" className="ghost" icon="link" label={`${t.link} ⌘K`} onMouseDown={keepFocus} onClick={() => wrap("[", "](https://)", "link")} />
            <span className="md-sep" aria-hidden="true" />
            <IconButton type="button" className="ghost" icon="list" label={t.bullets} onMouseDown={keepFocus} onClick={() => prefixLines(() => "- ")} />
            <IconButton type="button" className="ghost" icon="listOrdered" label={t.numbered} onMouseDown={keepFocus} onClick={() => prefixLines((index) => `${index + 1}. `)} />
            <span className="md-sep" aria-hidden="true" />
            <IconButton type="button" className="ghost" icon="sparkles" label={t.mention} onMouseDown={keepFocus} onClick={() => insertTrigger("@")} />
            <IconButton type="button" className="ghost" icon="ticket" label={t.ticket} onMouseDown={keepFocus} onClick={() => insertTrigger("#")} />
          </>
        )}
        <div className="md-tabs">
          <button type="button" aria-pressed={!preview} onMouseDown={keepFocus} onClick={() => setPreview(false)}>
            {t.write}
          </button>
          <button type="button" aria-pressed={preview} onMouseDown={keepFocus} onClick={() => setPreview(true)}>
            {t.preview}
          </button>
        </div>
      </div>
      {preview ? (
        <div className="md-preview">
          {value.trim() ? <Markdown text={value} /> : <p>{t.nothing}</p>}
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
          placeholder={t.placeholder}
          aria-autocomplete="list"
          aria-expanded={mentions.open}
        />
      )}
      {!preview && mentions.popup}
    </div>
  );
}
