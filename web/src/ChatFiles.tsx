import { lazy, Suspense, useEffect, useState } from "react";
import { api } from "./api";
import { ErrorText, shortDate } from "./App";
import { confirmDialog } from "./confirm";
import { Icon, IconButton } from "./icons";
import { locale, useText } from "./i18n";
import Menu from "./Menu";
import { formatBytes } from "./Settings";

const FileViewer = lazy(() => import("./FileViewer"));

const text = {
  en: {
    files: "Files",
    chatFiles: "Chat files",
    changeFolder: "Change folder…",
    changeFolderLocked: "Start a new chat to use another folder",
    fileManager: "Open in file manager",
    close: "Close files",
    path: "Folder path",
    empty: "No files yet.",
    actions: (name: string) => `${name} actions`,
    open: "Open",
    newWindow: "Open in new window",
    reference: "Reference in chat",
    delete: "Delete",
    deleteConfirm: (name: string) => `Delete "${name}"? This can't be undone.`,
  },
  es: {
    files: "Archivos",
    chatFiles: "Archivos del chat",
    changeFolder: "Cambiar carpeta…",
    changeFolderLocked: "Inicia un chat nuevo para usar otra carpeta",
    fileManager: "Abrir en el administrador de archivos",
    close: "Cerrar archivos",
    path: "Ruta de la carpeta",
    empty: "Aún no hay archivos.",
    actions: (name: string) => `Acciones de ${name}`,
    open: "Abrir",
    newWindow: "Abrir en ventana nueva",
    reference: "Mencionar en el chat",
    delete: "Eliminar",
    deleteConfirm: (name: string) => `¿Eliminar "${name}"? No se puede deshacer.`,
  },
};

type Entry = { name: string; path: string; dir: boolean; size: number; modified: string };


export default function ChatFiles({
  chatId,
  workdir,
  scratch,
  refreshKey,
  canChangeFolder,
  onChangeFolder,
  onReveal,
  onReference,
  onClose,
}: {
  chatId: string;
  workdir: string;
  scratch: boolean;
  refreshKey: unknown;
  canChangeFolder: boolean;
  onChangeFolder: () => void;
  onReveal: () => void;
  onReference: (path: string) => void;
  onClose: () => void;
}) {
  const t = useText(text);
  // Tagged with what they belong to, so a chat or folder switch shows nothing stale and fetches once.
  const place = `${chatId}\n${workdir}`;
  const [at, setAt] = useState({ place, dir: "" });
  const dir = at.place === place ? at.dir : "";
  const setDir = (next: string) => setAt({ place, dir: next });
  const listing = `${place}\n${dir}`;
  const [listed, setListed] = useState<{ listing: string; entries: Entry[] }>();
  const entries = listed?.listing === listing ? listed.entries : undefined;
  const [error, setError] = useState<unknown>();
  const [viewing, setViewing] = useState<string>();
  const [reload, setReload] = useState(0);
  // Scratch folders are named after the chat id, which means nothing to the user.
  const folderName = scratch ? t.chatFiles : (workdir.split(/[\\/]/).filter(Boolean).pop() ?? workdir);
  const crumbs = dir ? dir.split("/") : [];

  useEffect(() => {
    let live = true;
    api.get<Entry[]>(`/api/chats/${chatId}/files?path=${encodeURIComponent(dir)}`).then(
      (list) => live && (setListed({ listing, entries: list }), setError(undefined)),
      (reason) => live && setError(reason),
    );
    return () => {
      live = false;
    };
  }, [listing, refreshKey, reload]);

  function open(entry: Entry) {
    if (entry.dir) setDir(entry.path);
    else setViewing(entry.path);
  }

  async function remove(entry: Entry) {
    if (!(await confirmDialog(t.deleteConfirm(entry.name)))) return;
    try {
      await api.delete(`/api/chats/${chatId}/files?path=${encodeURIComponent(entry.path)}`);
      setReload((n) => n + 1);
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <aside className="chat-files" aria-label={t.files}>
      <header className="chat-files-head">
        <h2 title={workdir}>
          <Icon name="folder" /> <span className="attachment-name">{folderName}</span>
        </h2>
        <IconButton
          type="button"
          className="ghost"
          icon="pencil"
          label={canChangeFolder ? t.changeFolder : t.changeFolderLocked}
          disabled={!canChangeFolder}
          onClick={onChangeFolder}
        />
        <IconButton type="button" className="ghost" icon="externalLink" label={t.fileManager} onClick={onReveal} />
        <IconButton type="button" className="ghost" icon="x" label={t.close} onClick={onClose} />
      </header>
      {crumbs.length > 0 && (
        <nav className="chat-files-crumbs" aria-label={t.path}>
          <IconButton type="button" className="ghost" icon="folder" label={folderName} onClick={() => setDir("")} />
          {crumbs.map((crumb, i) => (
            <span key={i}>
              <Icon name="chevronRight" />
              <button type="button" className="ghost attachment-name" title={crumb} aria-current={i === crumbs.length - 1 ? "page" : undefined} onClick={() => setDir(crumbs.slice(0, i + 1).join("/"))}>
                {crumb}
              </button>
            </span>
          ))}
        </nav>
      )}
      <ErrorText error={error} />
      {entries?.length === 0 && <p className="muted">{t.empty}</p>}
      <ul className="chat-files-list">
        {entries?.map((entry) => (
          <li key={entry.path}>
            <button type="button" className="ghost chat-files-row" onClick={() => open(entry)}>
              <Icon name={entry.dir ? "folder" : "file"} />
              <span className="attachment-name" title={entry.name}>{entry.name}</span>
              <span className="muted" title={new Date(entry.modified).toLocaleString(locale())}>
                {entry.dir ? "" : `${formatBytes(entry.size)} · `}
                {shortDate(entry.modified)}
              </span>
            </button>
            <Menu label={t.actions(entry.name)} buttonClassName="icon ghost" button={<Icon name="more" />}>
              {(close) => (
                <>
                  <button role="menuitem" onClick={() => (close(false), open(entry))}>
                    <Icon name={entry.dir ? "folder" : "eye"} /> {t.open}
                  </button>
                  {!entry.dir && (
                    <button
                      role="menuitem"
                      onClick={() => (close(), api.post(`/api/chats/${chatId}/files/window`, { path: entry.path }).catch(setError))}
                    >
                      <Icon name="externalLink" /> {t.newWindow}
                    </button>
                  )}
                  <button role="menuitem" onClick={() => (close(false), onReference(entry.path))}>
                    <Icon name="paperclip" /> {t.reference}
                  </button>
                  <button role="menuitem" className="danger-text" onClick={() => (close(), remove(entry))}>
                    <Icon name="trash" /> {t.delete}
                  </button>
                </>
              )}
            </Menu>
          </li>
        ))}
      </ul>
      {viewing && (
        <Suspense fallback={null}>
          <FileViewer chatId={chatId} path={viewing} onClose={() => setViewing(undefined)} />
        </Suspense>
      )}
    </aside>
  );
}
