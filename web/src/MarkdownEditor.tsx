import { useLayoutEffect, useRef, useState, type ClipboardEvent, type DragEvent, type FocusEvent, type KeyboardEvent, type MouseEvent } from "react";
import { IconButton } from "./icons";
import Markdown from "./Markdown";
import { useMentions } from "./mentions";
import { lang, useText } from "./i18n";
import { hasPending, maxTicketAttachments, pastedFile, pendingLink, replacePending, shrinkImage, ticketFileProblem, uploadTicketFile } from "./attachments";

const text = {
  en: { formatting: "formatting", style: "Text style", normal: "Normal", title: "Title", heading: "Heading", subheading: "Subheading", bold: "Bold", italic: "Italic", code: "Code", link: "Link", bullets: "Bulleted list", numbered: "Numbered list", mention: "Mention a bot (@)", ticket: "Link a ticket (#)", write: "Write", preview: "Preview", nothing: "Nothing to preview", placeholder: "Markdown supported · @ to mention a bot · # to link a ticket", filePlaceholder: "Markdown supported · @ to mention a bot · # to link a ticket · paste or drop files", uploading: (name: string) => `Uploading ${name}…`, uploadingCount: (n: number) => `Uploading ${n} file${n === 1 ? "" : "s"}…`, pendingHint: "Pasted files upload when the ticket is created.", imageLarge: (name: string) => `${name} is larger than 10 MB.`, fileLarge: (name: string) => `${name} is larger than 20 MB.`, tooMany: (max: number) => `A ticket can have at most ${max} attachments.`, failed: (name: string, error: string) => `${name} failed to upload: ${error}` },
  es: { formatting: "formato", style: "Estilo de texto", normal: "Normal", title: "Título", heading: "Encabezado", subheading: "Subtítulo", bold: "Negrita", italic: "Cursiva", code: "Código", link: "Enlace", bullets: "Lista con viñetas", numbered: "Lista numerada", mention: "Mencionar un bot (@)", ticket: "Enlazar un ticket (#)", write: "Escribir", preview: "Vista previa", nothing: "Nada que previsualizar", placeholder: "Markdown disponible · @ para mencionar un bot · # para enlazar un ticket", filePlaceholder: "Markdown disponible · @ para mencionar un bot · # para enlazar un ticket · pega o suelta archivos", uploading: (name: string) => `Subiendo ${name}…`, uploadingCount: (n: number) => `Subiendo ${n} archivo${n === 1 ? "" : "s"}…`, pendingHint: "Los archivos pegados se suben al crear el ticket.", imageLarge: (name: string) => `${name} pesa más de 10 MB.`, fileLarge: (name: string) => `${name} pesa más de 20 MB.`, tooMany: (max: number) => `Un ticket admite como máximo ${max} adjuntos.`, failed: (name: string, error: string) => `${name} no se pudo subir: ${error}` },
};

const linePrefix = /^(#{1,3} |[-*] |\d+\. )/;

// Files pasted before the ticket exists, keyed by their placeholder id; see uploadPendingFiles.
export type PendingFiles = Map<string, File>;

// Uploads the files whose placeholders are still in `description` and swaps each placeholder for its link,
// or for a visible error note when the upload fails. Returns the new description.
export async function uploadPendingFiles(ticketId: string, description: string, pending: PendingFiles) {
  const t = text[lang()];
  for (const [id, file] of pending) {
    if (!hasPending(description, id)) continue;
    let replacement: string;
    try {
      replacement = await uploadTicketFile(ticketId, file);
    } catch (reason) {
      replacement = `_⚠️ ${t.failed(file.name, reason instanceof Error ? reason.message : String(reason))}_`;
    }
    description = replacePending(description, id, replacement);
  }
  return description;
}

export default function MarkdownEditor({
  value,
  onChange,
  onBlur,
  onSubmit,
  label,
  ticketId,
  pendingFiles,
}: {
  value: string;
  onChange: (value: string) => void;
  onBlur?: (value: string) => void;
  onSubmit?: () => void;
  label: string;
  // Pasted or dropped files upload to this ticket right away.
  ticketId?: string;
  // Without a ticket yet, files wait here until the ticket is created.
  pendingFiles?: PendingFiles;
}) {
  const t = useText(text);
  const ref = useRef<HTMLTextAreaElement>(null);
  const [preview, setPreview] = useState(false);
  const mentions = useMentions(ref, onChange);
  const [uploading, setUploading] = useState(0);
  const [uploadError, setUploadError] = useState("");
  // Uploads finish after later renders, so they read the latest value and callbacks from refs.
  const latest = useRef({ value, onChange, onBlur });
  latest.current = { value, onChange, onBlur };
  const acceptsFiles = Boolean(ticketId || pendingFiles);

  function replaceLatest(id: string, replacement: string) {
    const next = replacePending(latest.current.value, id, replacement);
    latest.current.value = next;
    latest.current.onChange(next);
    return next;
  }

  async function addFiles(picked: File[]) {
    setUploadError("");
    const files = await Promise.all(picked.map(shrinkImage));
    for (const file of files) {
      const problem = ticketFileProblem(file);
      if (problem) return setUploadError(t[problem](file.name));
    }
    if (pendingFiles && pendingFiles.size + files.length > maxTicketAttachments) return setUploadError(t.tooMany(maxTicketAttachments));
    const ids = files.map(() => crypto.randomUUID());
    const el = ref.current;
    const current = latest.current.value;
    const start = el?.selectionStart ?? current.length;
    const end = el?.selectionEnd ?? current.length;
    const before = current.slice(0, start);
    const inserted = (before && !before.endsWith("\n") ? "\n" : "") + files.map((file, i) => pendingLink(ids[i], ticketId ? t.uploading(file.name) : file.name)).join("\n") + "\n";
    const next = before + inserted + current.slice(end);
    latest.current.value = next;
    latest.current.onChange(next);
    const caret = start + inserted.length;
    requestAnimationFrame(() => ref.current?.setSelectionRange(caret, caret));
    if (!ticketId) {
      files.forEach((file, i) => pendingFiles!.set(ids[i], file));
      return;
    }
    setUploading((n) => n + files.length);
    let saved = next;
    for (const [i, file] of files.entries()) {
      try {
        saved = replaceLatest(ids[i], await uploadTicketFile(ticketId, file));
      } catch (reason) {
        saved = replaceLatest(ids[i], "");
        setUploadError(t.failed(file.name, reason instanceof Error ? reason.message : String(reason)));
      } finally {
        setUploading((n) => n - 1);
      }
    }
    // The editor saves on blur; if it already lost focus, save the finished links now.
    if (!ref.current?.closest(".md-editor")?.contains(document.activeElement)) latest.current.onBlur?.(saved);
  }

  function onPaste(event: ClipboardEvent<HTMLTextAreaElement>) {
    const files = Array.from(event.clipboardData.files);
    if (!acceptsFiles || !files.length) return;
    event.preventDefault();
    void addFiles(files.map(pastedFile));
  }

  function onDragOver(event: DragEvent<HTMLTextAreaElement>) {
    if (!acceptsFiles || !event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  }

  function onDrop(event: DragEvent<HTMLTextAreaElement>) {
    if (!acceptsFiles || !event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
    void addFiles(Array.from(event.dataTransfer.files));
  }

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
    if (!event.currentTarget.contains(event.relatedTarget)) onBlur?.(value);
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
          onPaste={onPaste}
          onDragOver={onDragOver}
          onDrop={onDrop}
          placeholder={acceptsFiles ? t.filePlaceholder : t.placeholder}
          aria-autocomplete="list"
          aria-expanded={mentions.open}
        />
      )}
      {!preview && mentions.popup}
      {uploading > 0 && (
        <p className="muted md-status" role="status">
          <span className="spinner" aria-hidden="true" /> {t.uploadingCount(uploading)}
        </p>
      )}
      {!ticketId && pendingFiles && value.includes("](pending:") && <p className="muted md-status">{t.pendingHint}</p>}
      {uploadError && (
        <p className="error md-status" role="alert">
          {uploadError}
        </p>
      )}
    </div>
  );
}
