import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import hljs from "highlight.js/lib/common";
import { api } from "./api";
import { ErrorText } from "./App";
import { Icon, IconButton } from "./icons";
import { useText } from "./i18n";
import Markdown from "./Markdown";
import { formatBytes } from "./Settings";

const text = {
  en: {
    close: "Close",
    newWindow: "Open in new window",
    defaultApp: "Open in default app",
    source: "Show source",
    rendered: "Show rendered",
    quickLook: "Quick Look preview",
    webContent: "Allow web content",
    webContentHint: "Lets this page load scripts, styles, fonts and images from the internet. It still can't reach Patio.",
    loading: "Loading…",
    tooLarge: "This file is too large to preview.",
    noPreview: "No preview for this file type.",
    rowsCut: (shown: number) => `Showing the first ${shown} rows.`,
    sheets: "Sheets",
    entries: (n: number) => `${n} item${n === 1 ? "" : "s"}`,
  },
  es: {
    close: "Cerrar",
    newWindow: "Abrir en ventana nueva",
    defaultApp: "Abrir en la app predeterminada",
    source: "Ver código fuente",
    rendered: "Ver renderizado",
    quickLook: "Vista previa con Quick Look",
    webContent: "Permitir contenido web",
    webContentHint: "Permite que esta página cargue scripts, estilos, fuentes e imágenes de internet. Aun así no puede acceder a Patio.",
    loading: "Cargando…",
    tooLarge: "Este archivo es demasiado grande para previsualizarlo.",
    noPreview: "No hay vista previa para este tipo de archivo.",
    rowsCut: (shown: number) => `Se muestran las primeras ${shown} filas.`,
    sheets: "Hojas",
    entries: (n: number) => `${n} elemento${n === 1 ? "" : "s"}`,
  },
};

const previewLimit = 50 * 1024 * 1024;
const rowLimit = 2000;
// Same rules as artifacts: scripts run, but nothing loads from the network or reaches /api.
const htmlCSP = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:">`;
// Opt-in per view: https only, so Patio's own origin (wails:/http) stays unreachable; the sandbox still has no same-origin.
const webCSP = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline' https:; style-src 'unsafe-inline' https:; img-src data: blob: https:; font-src data: https:; media-src data: blob: https:; connect-src https:">`;
const highlightLimit = 500 * 1024;

const kinds: Record<string, string> = {
  png: "image", jpg: "image", jpeg: "image", gif: "image", webp: "image", svg: "image", bmp: "image", ico: "image", avif: "image",
  pdf: "pdf", html: "html", htm: "html", json: "json", md: "markdown", markdown: "markdown",
  xlsx: "sheet", xls: "sheet", ods: "sheet", csv: "sheet", tsv: "sheet", docx: "docx", pptx: "pptx", zip: "zip",
  mp3: "audio", wav: "audio", m4a: "audio", ogg: "audio", flac: "audio", mp4: "video", mov: "video", webm: "video", m4v: "video",
};
const textExts =
  "txt log yaml yml xml toml ini conf cfg env sh bash zsh py js mjs cjs ts tsx jsx go rs java kt swift c h cpp hpp cs rb php sql css scss less diff patch gitignore dockerfile makefile";
for (const ext of textExts.split(" ")) kinds[ext] = "text";

export function fileKind(name: string) {
  const dot = name.lastIndexOf(".");
  return kinds[(dot < 0 ? name : name.slice(dot + 1)).toLowerCase()] ?? "other";
}

export function rawURL(chatId: string, path: string) {
  return `/api/chats/${chatId}/files/raw?path=${encodeURIComponent(path)}`;
}

async function fetchFile(url: string, method = "GET") {
  const response = await fetch(url, { method });
  if (!response.ok) throw new Error((await response.json().catch(() => ({}))).error || `${response.status}`);
  return response;
}

function useLoad<T>(load: () => Promise<T>, deps: unknown[]) {
  const [state, setState] = useState<{ data?: T; error?: unknown }>({});
  useEffect(() => {
    let live = true;
    setState({});
    load().then(
      (data) => live && setState({ data }),
      (error) => live && setState({ error }),
    );
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return state;
}

export default function FileViewer({ chatId, path, standalone, onClose }: { chatId: string; path: string; standalone?: boolean; onClose?: () => void }) {
  const t = useText(text);
  const dialog = useRef<HTMLDialogElement>(null);
  const [error, setError] = useState<unknown>();
  const [source, setSource] = useState(false);
  const [quickLook, setQuickLook] = useState(false);
  const [web, setWeb] = useState(false);
  const name = path.split("/").pop() ?? path;
  const kind = fileKind(name);
  useEffect(() => {
    if (!standalone && !dialog.current?.open) dialog.current?.showModal();
  }, [standalone]);

  const openDefault = () => api.post(`/api/chats/${chatId}/files/open`, { path }).catch(setError);
  const head = (
    <header className="file-viewer-head">
      <h2 className="attachment-name" id="file-viewer-title" title={path}>
        {name}
      </h2>
      {kind === "html" && !source && (
        <button type="button" className="ghost" aria-pressed={web} data-tip={t.webContentHint} onClick={() => setWeb(!web)}>
          <Icon name="link" /> {t.webContent}
        </button>
      )}
      {(kind === "markdown" || kind === "html") && (
        <button type="button" className="ghost" onClick={() => setSource(!source)}>
          <Icon name={source ? "eye" : "code"} /> {source ? t.rendered : t.source}
        </button>
      )}
      {kind === "pptx" && (
        <button type="button" className="ghost" aria-pressed={quickLook} onClick={() => setQuickLook(!quickLook)}>
          <Icon name="eye" /> {t.quickLook}
        </button>
      )}
      <IconButton type="button" className="ghost" icon="file" label={t.defaultApp} onClick={openDefault} />
      {!standalone && (
        <>
          <IconButton
            type="button"
            className="ghost"
            icon="externalLink"
            label={t.newWindow}
            onClick={() => api.post(`/api/chats/${chatId}/files/window`, { path }).catch(setError)}
          />
          <IconButton type="button" className="ghost" icon="x" label={t.close} onClick={() => dialog.current?.close()} />
        </>
      )}
    </header>
  );
  const body = (
    <>
      {head}
      <ErrorText error={error} />
      <div className="file-viewer-body">
        <Preview
          chatId={chatId}
          path={path}
          kind={quickLook ? "quicklook" : kind}
          source={source}
          web={web}
          onOfficeError={() => kind === "pptx" && setQuickLook(true)}
          fallback={
            <p className="file-viewer-note">
              {t.noPreview}{" "}
              <button type="button" className="ghost" onClick={openDefault}>
                <Icon name="file" /> {t.defaultApp}
              </button>
            </p>
          }
        />
      </div>
    </>
  );
  if (standalone) return <main className="file-viewer standalone">{body}</main>;
  return (
    <dialog ref={dialog} className="file-viewer" aria-labelledby="file-viewer-title" onClose={onClose}>
      {body}
    </dialog>
  );
}

function Preview({ chatId, path, kind, source, web, fallback, onOfficeError }: { chatId: string; path: string; kind: string; source: boolean; web: boolean; fallback: ReactNode; onOfficeError: () => void }) {
  const t = useText(text);
  const url = rawURL(chatId, path);
  const name = path.split("/").pop() ?? path;
  const streamed = kind === "audio" || kind === "video";
  const { data: size, error } = useLoad(
    () => (streamed ? Promise.resolve(0) : fetchFile(url, "HEAD").then((response) => Number(response.headers.get("content-length")))),
    [url, streamed],
  );
  if (size === undefined) return <Loading error={error} />;
  if (size > previewLimit)
    return (
      <>
        <p className="file-viewer-note">{t.tooLarge}</p>
        {fallback}
      </>
    );
  switch (kind) {
    case "image":
      return <img className="file-viewer-image" src={url} alt={name} />;
    case "pdf":
      return <iframe className="file-viewer-frame" src={url} title={name} />;
    case "audio":
      return <audio controls src={url} />;
    case "video":
      return <video className="file-viewer-image" controls src={url} />;
    case "html":
    case "json":
    case "markdown":
    case "text":
      return <TextPreview url={url} kind={kind} source={source} web={web} name={name} />;
    case "quicklook":
      return <TextPreview url={`/api/chats/${chatId}/files/quicklook?path=${encodeURIComponent(path)}`} kind="quicklook" source={false} web={false} name={name} />;
    case "sheet":
      return <SheetPreview url={url} csv={/\.(csv|tsv)$/i.test(name)} />;
    case "docx":
    case "pptx":
      return <OfficePreview url={url} kind={kind} name={name} onError={onOfficeError} />;
    case "zip":
      return <ZipPreview chatId={chatId} path={path} />;
    default:
      return fallback;
  }
}

function Loading({ error }: { error: unknown }) {
  const t = useText(text);
  if (error) return <ErrorText error={error} />;
  return <p className="muted file-viewer-note">{t.loading}</p>;
}

function TextPreview({ url, kind, source, web, name }: { url: string; kind: string; source: boolean; web: boolean; name: string }) {
  const { data, error } = useLoad(() => fetchFile(url).then((response) => response.text()), [url]);
  if (data === undefined) return <Loading error={error} />;
  if (kind === "html" && !source)
    return <iframe key={String(web)} className="file-viewer-frame" sandbox="allow-scripts" srcDoc={(web ? webCSP : htmlCSP) + data} title={name} />;
  if (kind === "quicklook") return <iframe className="file-viewer-frame" sandbox="" srcDoc={data} title={name} />;
  if (kind === "markdown" && !source)
    return (
      <div className="file-viewer-doc">
        <Markdown text={data} />
      </div>
    );
  let shown = data;
  if (kind === "json")
    try {
      shown = JSON.stringify(JSON.parse(data), null, 2);
    } catch {
      // Invalid JSON still shows as text.
    }
  const ext = name.slice(name.lastIndexOf(".") + 1).toLowerCase();
  const language = kind === "json" ? "json" : kind === "markdown" ? "markdown" : kind === "html" ? "xml" : ext;
  return <CodeBlock code={shown} language={language} />;
}

function CodeBlock({ code, language }: { code: string; language: string }) {
  const html = useMemo(() => {
    if (code.length > highlightLimit || !hljs.getLanguage(language)) return undefined;
    return hljs.highlight(code, { language, ignoreIllegals: true }).value;
  }, [code, language]);
  const lines = code.split("\n").length;
  return (
    <div className="file-viewer-code">
      <pre className="file-viewer-gutter" aria-hidden="true">
        {Array.from({ length: lines }, (_, i) => i + 1).join("\n")}
      </pre>
      {/* highlight.js escapes the source; its output is only spans with classes. */}
      {html !== undefined ? <pre className="file-viewer-text hljs" dangerouslySetInnerHTML={{ __html: html }} /> : <pre className="file-viewer-text">{code}</pre>}
    </div>
  );
}

function SheetPreview({ url, csv }: { url: string; csv: boolean }) {
  const t = useText(text);
  const [sheet, setSheet] = useState(0);
  const { data, error } = useLoad(async () => {
    const [response, XLSX] = await Promise.all([fetchFile(url), import("xlsx")]);
    const book = csv ? XLSX.read(await response.text(), { type: "string", raw: true, sheetRows: rowLimit + 1 }) : XLSX.read(await response.arrayBuffer(), { dense: true, sheetRows: rowLimit + 1 });
    return book.SheetNames.map((name) => ({
      name,
      rows: XLSX.utils.sheet_to_json<unknown[]>(book.Sheets[name], { header: 1, raw: false, defval: "" }),
    }));
  }, [url, csv]);
  if (!data) return <Loading error={error} />;
  const rows = data[sheet]?.rows ?? [];
  return (
    <div className="file-viewer-sheet">
      {data.length > 1 && (
        <div className="file-viewer-tabs" role="tablist" aria-label={t.sheets}>
          {data.map((s, i) => (
            <button key={s.name} type="button" role="tab" className="ghost" aria-selected={i === sheet} onClick={() => setSheet(i)}>
              {s.name}
            </button>
          ))}
        </div>
      )}
      {rows.length > rowLimit && <p className="muted">{t.rowsCut(rowLimit)}</p>}
      <table className="file-viewer-table">
        <tbody>
          {rows.slice(0, rowLimit).map((row, r) => (
            <tr key={r}>
              <th scope="row">{r + 1}</th>
              {row.map((cell, c) => (
                <td key={c}>{String(cell)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// docx/pptx render in office.html, whose CSP stops script smuggled in through document text.
function OfficePreview({ url, kind, name, onError }: { url: string; kind: string; name: string; onError: () => void }) {
  const frame = useRef<HTMLIFrameElement>(null);
  const { data, error } = useLoad(() => fetchFile(url).then((response) => response.arrayBuffer()), [url]);
  const [failed, setFailed] = useState<string>();
  useEffect(() => {
    if (!data) return;
    const onMessage = (event: MessageEvent) => {
      if (event.source !== frame.current?.contentWindow) return;
      if (event.data?.ready) frame.current?.contentWindow?.postMessage({ kind, data }, "*");
      if (event.data?.error) {
        setFailed(event.data.error);
        onError();
      }
    };
    addEventListener("message", onMessage);
    return () => removeEventListener("message", onMessage);
  }, [data, kind, onError]);
  if (!data) return <Loading error={error} />;
  if (failed) return <ErrorText error={failed} />;
  return <iframe ref={frame} className="file-viewer-frame" sandbox="allow-scripts allow-same-origin" src="/office.html" title={name} />;
}

function ZipPreview({ chatId, path }: { chatId: string; path: string }) {
  const t = useText(text);
  const { data, error } = useLoad(
    () => api.get<{ name: string; size: number; dir: boolean }[]>(`/api/chats/${chatId}/files/zip?path=${encodeURIComponent(path)}`),
    [chatId, path],
  );
  if (!data) return <Loading error={error} />;
  return (
    <div className="file-viewer-doc">
      <p className="muted">{t.entries(data.length)}</p>
      <table className="file-viewer-table">
        <tbody>
          {data.map((entry) => (
            <tr key={entry.name}>
              <td>
                <Icon name={entry.dir ? "folder" : "file"} /> {entry.name}
              </td>
              <td className="muted">{entry.dir ? "" : formatBytes(entry.size)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
