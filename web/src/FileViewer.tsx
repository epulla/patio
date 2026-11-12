import { useEffect, useRef, useState, type ReactNode } from "react";
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
    loading: "Loading…",
    tooLarge: "This file is too large to preview.",
    noPreview: "No preview for this file type.",
    rowsCut: (shown: number, total: number) => `Showing the first ${shown} of ${total} rows.`,
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
    loading: "Cargando…",
    tooLarge: "Este archivo es demasiado grande para previsualizarlo.",
    noPreview: "No hay vista previa para este tipo de archivo.",
    rowsCut: (shown: number, total: number) => `Se muestran las primeras ${shown} de ${total} filas.`,
    sheets: "Hojas",
    entries: (n: number) => `${n} elemento${n === 1 ? "" : "s"}`,
  },
};

const previewLimit = 50 * 1024 * 1024;
const rowLimit = 2000;
// Same rules as artifacts: scripts run, but nothing loads from the network or reaches /api.
const htmlCSP = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:">`;

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

class TooLarge extends Error {}

async function fetchFile(url: string) {
  const response = await fetch(url);
  if (!response.ok) throw new Error((await response.json().catch(() => ({}))).error || `${response.status}`);
  if (Number(response.headers.get("content-length")) > previewLimit) throw new TooLarge();
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
      {kind === "markdown" && (
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

function Preview({ chatId, path, kind, source, fallback, onOfficeError }: { chatId: string; path: string; kind: string; source: boolean; fallback: ReactNode; onOfficeError: () => void }) {
  const url = rawURL(chatId, path);
  const name = path.split("/").pop() ?? path;
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
      return <TextPreview url={url} kind={kind} source={source} name={name} fallback={fallback} />;
    case "quicklook":
      return <TextPreview url={`/api/chats/${chatId}/files/quicklook?path=${encodeURIComponent(path)}`} kind="quicklook" source={false} name={name} fallback={fallback} />;
    case "sheet":
      return <SheetPreview url={url} csv={/\.(csv|tsv)$/i.test(name)} fallback={fallback} />;
    case "docx":
    case "pptx":
      return <OfficePreview url={url} kind={kind} name={name} fallback={fallback} onError={onOfficeError} />;
    case "zip":
      return <ZipPreview chatId={chatId} path={path} />;
    default:
      return fallback;
  }
}

function Loading({ error, fallback }: { error: unknown; fallback: ReactNode }) {
  const t = useText(text);
  if (error instanceof TooLarge)
    return (
      <>
        <p className="file-viewer-note">{t.tooLarge}</p>
        {fallback}
      </>
    );
  if (error) return <ErrorText error={error} />;
  return <p className="muted file-viewer-note">{t.loading}</p>;
}

function TextPreview({ url, kind, source, name, fallback }: { url: string; kind: string; source: boolean; name: string; fallback: ReactNode }) {
  const { data, error } = useLoad(() => fetchFile(url).then((response) => response.text()), [url]);
  if (data === undefined) return <Loading error={error} fallback={fallback} />;
  if (kind === "html") return <iframe className="file-viewer-frame" sandbox="allow-scripts" srcDoc={htmlCSP + data} title={name} />;
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
  return <pre className="file-viewer-text">{shown}</pre>;
}

function SheetPreview({ url, csv, fallback }: { url: string; csv: boolean; fallback: ReactNode }) {
  const t = useText(text);
  const [sheet, setSheet] = useState(0);
  const { data, error } = useLoad(async () => {
    const [response, XLSX] = await Promise.all([fetchFile(url), import("xlsx")]);
    const book = csv ? XLSX.read(await response.text(), { type: "string", raw: true }) : XLSX.read(await response.arrayBuffer(), { dense: true });
    return book.SheetNames.map((name) => ({
      name,
      rows: XLSX.utils.sheet_to_json<unknown[]>(book.Sheets[name], { header: 1, raw: false, defval: "" }),
    }));
  }, [url, csv]);
  if (!data) return <Loading error={error} fallback={fallback} />;
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
      {rows.length > rowLimit && <p className="muted">{t.rowsCut(rowLimit, rows.length)}</p>}
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
function OfficePreview({ url, kind, name, fallback, onError }: { url: string; kind: string; name: string; fallback: ReactNode; onError: () => void }) {
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
  if (!data) return <Loading error={error} fallback={fallback} />;
  if (failed) return <ErrorText error={failed} />;
  return <iframe ref={frame} className="file-viewer-frame" sandbox="allow-scripts allow-same-origin" src="/office.html" title={name} />;
}

function ZipPreview({ chatId, path }: { chatId: string; path: string }) {
  const t = useText(text);
  const { data, error } = useLoad(
    () => api.get<{ name: string; size: number; dir: boolean }[]>(`/api/chats/${chatId}/files/zip?path=${encodeURIComponent(path)}`),
    [chatId, path],
  );
  if (!data) return <Loading error={error} fallback={null} />;
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
