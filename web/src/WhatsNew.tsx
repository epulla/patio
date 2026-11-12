import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import { ErrorText } from "./App";
import { IconButton } from "./icons";
import { useText } from "./i18n";
import Markdown from "./Markdown";

export type ReleaseNotes = { version: string; notes: string; url: string };

const text = {
  en: { title: (v: string) => `What's new in Patio ${v}`, close: "Close", loading: "Loading…", empty: "This release has no notes." },
  es: { title: (v: string) => `Novedades de Patio ${v}`, close: "Cerrar", loading: "Cargando…", empty: "Esta versión no tiene notas." },
};

export function WhatsNewDialog({ version, onClose }: { version: string; onClose: () => void }) {
  const t = useText(text);
  const dialog = useRef<HTMLDialogElement>(null);
  const [release, setRelease] = useState<ReleaseNotes>();
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
    api.get<ReleaseNotes>("/api/release-notes").then(setRelease).catch(setError);
  }, []);
  return (
    <dialog ref={dialog} className="create-dialog" aria-labelledby="whats-new-title" onClose={onClose}>
      <section className="panel create create-modal shortcuts-modal">
        <div className="page-heading">
          <h2 id="whats-new-title">{t.title(version)}</h2>
          <IconButton type="button" className="ghost" icon="x" label={t.close} onClick={() => dialog.current?.close()} />
        </div>
        <ErrorText error={error} />
        {release ? <Markdown text={release.notes || t.empty} /> : !error && <p className="muted">{t.loading}</p>}
      </section>
    </dialog>
  );
}
