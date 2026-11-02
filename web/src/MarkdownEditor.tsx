import { useEffect, useLayoutEffect, useRef, useState, type ClipboardEvent, type DragEvent, type FocusEvent, type KeyboardEvent, type MouseEvent } from "react";
import { IconButton } from "./icons";
import Markdown from "./Markdown";
import { useMentions } from "./mentions";
import { lang, useText } from "./i18n";
import { finishPending, hasPending, maxTicketAttachments, registerEditor, type TicketField, pastedFile, pendingLink, replacePending, shrinkImage, ticketFileProblem, uploadTicketFile } from "./attachments";

const text = {
  en: { formatting: "formatting", style: "Text style", normal: "Normal", title: "Title", heading: "Heading", subheading: "Subheading", bold: "Bold", italic: "Italic", code: "Code", link: "Link", bullets: "Bulleted list", numbered: "Numbered list", mention: "Mention a bot (@)", ticket: "Link a ticket (#)", write: "Write", preview: "Preview", nothing: "Nothing to preview", placeholder: "Markdown supported · @ to mention a bot · # to link a ticket", filePlaceholder: "Markdown supported · @ to mention a bot · # to link a ticket · paste or drop files", uploading: (name: string) => `Uploading ${name}…`, uploadingCount: (n: number) => `Uploading ${n} file${n === 1 ? "" : "s"}…`, pendingHint: "Pasted files upload when the ticket is created.", imageLarge: (name: string) => `${name} is larger than 10 MB.`, fileLarge: (name: string) => `${name} is larger than 20 MB.`, tooMany: (max: number) => `A ticket can have at most ${max} attachments.`, failed: (name: string, error: string) => `${name} failed to upload: ${error}` },
  es: { formatting: "formato", style: "Estilo de texto", normal: "Normal", title: "Título", heading: "Encabezado", subheading: "Subtítulo", bold: "Negrita", italic: "Cursiva", code: "Código", link: "Enlace", bullets: "Lista con viñetas", numbered: "Lista numerada", mention: "Mencionar un bot (@)", ticket: "Enlazar un ticket (#)", write: "Escribir", preview: "Vista previa", nothing: "Nada que previsualizar", placeholder: "Markdown disponible · @ para mencionar un bot · # para enlazar un ticket", filePlaceholder: "Markdown disponible · @ para mencionar un bot · # para enlazar un ticket · pega o suelta archivos", uploading: (name: string) => `Subiendo ${name}…`, uploadingCount: (n: number) => `Subiendo ${n} archivo${n === 1 ? "" : "s"}…`, pendingHint: "Los archivos pegados se suben al crear el ticket.", imageLarge: (name: string) => `${name} pesa más de 10 MB.`, fileLarge: (name: string) => `${name} pesa más de 20 MB.`, tooMany: (max: number) => `Un ticket admite como máximo ${max} adjuntos.`, failed: (name: string, error: string) => `${name} no se pudo subir: ${error}` },
};

const linePrefix = /^(#{1,3} |[-*] |\d+\. )/;

// Files pasted before the ticket exists, keyed by their placeholder id; see uploadPendingFiles.
// Values are promises because images are still being shrunk when the placeholder goes in.
export type PendingFiles = Map<string, Promise<File>>;

const failure = (reason: unknown) => (reason instanceof Error ? reason.message : String(reason));

// Uploads the files whose placeholders are still in `description` and swaps each placeholder for its link,
// or for a visible error note when the upload fails. Returns the new description.
export async function uploadPendingFiles(ticketId: string, description: string, pending: PendingFiles) {
  const t = text[lang()];
  for (const [id, prepared] of pending) {
    if (!hasPending(description, id)) continue;
    const file = await prepared;
    let replacement: string;
    try {
      const problem = ticketFileProblem(file);
      if (problem) throw new Error(t[problem](file.name));
      replacement = await uploadTicketFile(ticketId, file);
    } catch (reason) {
      replacement = `_⚠️ ${t.failed(file.name, failure(reason))}_`;
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
  field,
  pendingFiles,
}: {
  value: string;
  onChange: (value: string) => void;
  onBlur?: (value: string) => void;
  onSubmit?: () => void;
  label: string;
  // Pasted or dropped files upload to this ticket right away.
  ticketId?: string;
  // Which ticket text this edits; an upload that ends after this editor closes finishes in the one open for the same field.
  field?: TicketField;
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

  // Swaps one placeholder in the current text; returns false when the user already deleted it.
  function replaceLatest(id: string, replacement: string) {
    if (!hasPending(latest.current.value, id)) return false;
    latest.current.value = replacePending(latest.current.value, id, replacement);
    latest.current.onChange(latest.current.value);
    return true;
  }

  useEffect(() => {
    if (!ticketId || !field) return;
    // The editor saves on blur; if it isn't focused, save the finished link now.
    return registerEditor(ticketId, field, (id, replacement) => {
      if (replaceLatest(id, replacement) && !ref.current?.closest(".md-editor")?.contains(document.activeElement))
        latest.current.onBlur?.(latest.current.value);
    });
  }, [ticketId, field]);

  // Finishes in whichever editor is open for this ticket text now, which may not be this one.
  const finish = async (id: string, replacement: string) =>
    ticketId && field ? finishPending(ticketId, field, id, replacement) : replaceLatest(id, replacement);

  // Placeholders go in right away, at the caret, so a submit or blur during shrinking or upload sees them.
  function addFiles(picked: File[]) {
    setUploadError("");
    if (pendingFiles && pendingFiles.size + picked.length > maxTicketAttachments) return setUploadError(t.tooMany(maxTicketAttachments));
    const ids = picked.map(() => crypto.randomUUID());
    const el = ref.current;
    const current = latest.current.value;
    const start = el?.selectionStart ?? current.length;
    const end = el?.selectionEnd ?? current.length;
    const before = current.slice(0, start);
    const inserted = (before && !before.endsWith("\n") ? "\n" : "") + picked.map((file, i) => pendingLink(ids[i], ticketId ? t.uploading(file.name) : file.name)).join("\n") + "\n";
    latest.current.value = before + inserted + current.slice(end);
    latest.current.onChange(latest.current.value);
    const caret = start + inserted.length;
    requestAnimationFrame(() => ref.current?.setSelectionRange(caret, caret));
    const prepared = picked.map(async (file, i) => {
      const shrunk = await shrinkImage(file);
      const problem = ticketFileProblem(shrunk);
      if (!problem) return shrunk;
      pendingFiles?.delete(ids[i]);
      void finish(ids[i], "");
      setUploadError(t[problem](file.name));
      throw new Error(t[problem](file.name));
    });
    if (!ticketId) {
      // A rejected file already removed its placeholder; uploadPendingFiles skips it.
      prepared.forEach((file, i) => pendingFiles!.set(ids[i], file.catch(() => picked[i])));
      return;
    }
    void upload(ticketId, ids, prepared);
  }

  async function upload(ticketId: string, ids: string[], prepared: Promise<File>[]) {
    setUploading((n) => n + ids.length);
    // A rejected file already removed its placeholder and showed its error.
    const files = prepared.map((file) => file.catch(() => undefined));
    for (const [i, file] of files.entries()) {
      const ready = await file;
      try {
        if (ready) await finish(ids[i], await uploadTicketFile(ticketId, ready));
      } catch (reason) {
        await finish(ids[i], "").catch(() => undefined);
        setUploadError(t.failed(ready!.name, failure(reason)));
      } finally {
        setUploading((n) => n - 1);
      }
    }
  }

  function onPaste(event: ClipboardEvent<HTMLTextAreaElement>) {
    const files = Array.from(event.clipboardData.files);
    if (!acceptsFiles || !files.length) return;
    event.preventDefault();
    addFiles(files.map(pastedFile));
  }

  function onDragOver(event: DragEvent<HTMLTextAreaElement>) {
    if (!acceptsFiles || !event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  }

  function onDrop(event: DragEvent<HTMLTextAreaElement>) {
    if (!acceptsFiles || !event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
    addFiles(Array.from(event.dataTransfer.files));
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
