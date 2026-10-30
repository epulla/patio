import { useEffect, useRef } from "react";
import { Icon, IconButton } from "./icons";
import { useText } from "./i18n";

const text = {
  en: { close: "Close", showFolder: "Show in folder" },
  es: { close: "Cerrar", showFolder: "Mostrar en carpeta" },
};

// Full-size image viewer; the native modal dialog handles Esc, focus and the backdrop click.
export default function Lightbox({ src, name, onReveal, onClose }: { src: string; name: string; onReveal: () => void; onClose: () => void }) {
  const t = useText(text);
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
  }, []);
  return (
    <dialog ref={dialog} className="create-dialog lightbox" aria-labelledby="lightbox-title" onClose={onClose}>
      <section className="panel create-modal lightbox-panel">
        <div className="page-heading">
          <h2 id="lightbox-title" className="attachment-name">{name}</h2>
          <button type="button" className="ghost" onClick={onReveal}>
            <Icon name="folder" /> {t.showFolder}
          </button>
          <IconButton type="button" className="ghost" icon="x" label={t.close} onClick={() => dialog.current?.close()} />
        </div>
        <img src={src} alt={name} />
      </section>
    </dialog>
  );
}
