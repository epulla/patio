import { useEffect, useRef } from "react";
import { IconButton } from "./icons";
import { useText } from "./i18n";

const mac = /Mac|iPhone|iPad/.test(navigator.platform);
const keys = (macKeys: string, otherKeys: string) => (mac ? macKeys : otherKeys);

export const shortcutsShortcut = keys("⌘/", "Ctrl+/");
export const shortcutsAriaShortcut = keys("Meta+/", "Control+/");
// By key, not code: on layouts like Spanish, / is Shift+7 and the US slash key types something else.
export const isShortcutsKey = (event: KeyboardEvent) => (event.metaKey || event.ctrlKey) && !event.altKey && event.key === "/";
// Asks App to open the shortcuts sheet.
export const openShortcuts = () => dispatchEvent(new Event("open-shortcuts"));

const text = {
  en: {
    title: "Keyboard shortcuts",
    close: "Close",
    sections: {
      general: "General",
      tabs: "Pinned tabs",
      groups: "Pin groups",
      message: "Message box",
    },
    items: {
      shortcuts: "Show keyboard shortcuts",
      newChat: "New chat",
      newPinned: "New pinned chat in the current group",
      dock: "Open the side panel, or jump to its message box",
      tabN: "Go to tab 1–9",
      tabStep: "Previous / next tab",
      closeTab: "Close tab (unpin)",
      groupStep: "Previous / next group",
      send: "Send",
      newline: "New line",
      speak: "Read the last reply aloud",
      pinModel: "Pin or unpin a model in the model picker",
      leaveDock: "Leave the side panel's message box",
    },
  },
  es: {
    title: "Atajos de teclado",
    close: "Cerrar",
    sections: {
      general: "General",
      tabs: "Pestañas fijadas",
      groups: "Grupos fijados",
      message: "Cuadro de mensaje",
    },
    items: {
      shortcuts: "Mostrar atajos de teclado",
      newChat: "Nuevo chat",
      newPinned: "Nuevo chat fijado en el grupo actual",
      dock: "Abrir el panel lateral o ir a su cuadro de mensaje",
      tabN: "Ir a la pestaña 1–9",
      tabStep: "Pestaña anterior / siguiente",
      closeTab: "Cerrar pestaña (quitar de fijados)",
      groupStep: "Grupo anterior / siguiente",
      send: "Enviar",
      newline: "Nueva línea",
      speak: "Leer en voz alta la última respuesta",
      pinModel: "Fijar o quitar un modelo en el selector de modelos",
      leaveDock: "Salir del cuadro de mensaje del panel lateral",
    },
  },
};

type Item = keyof (typeof text)["en"]["items"];
const sections: { id: keyof (typeof text)["en"]["sections"]; rows: [Item, string[]][] }[] = [
  {
    id: "general",
    rows: [
      ["shortcuts", [shortcutsShortcut]],
      ["newChat", [keys("⌘N", "Ctrl+N")]],
      ["newPinned", [keys("⌘⇧N", "Ctrl+Shift+N")]],
      ["dock", [keys("⌘J", "Ctrl+J")]],
    ],
  },
  {
    id: "tabs",
    rows: [
      ["tabN", [keys("⌘1 … ⌘9", "Ctrl+1 … Ctrl+9")]],
      ["tabStep", [keys("⌘⇧[", "Ctrl+Shift+["), keys("⌘⇧]", "Ctrl+Shift+]")]],
      ["closeTab", [keys("⌘W", "Ctrl+W"), keys("⌘⇧U", "Ctrl+Shift+U")]],
    ],
  },
  { id: "groups", rows: [["groupStep", [keys("⌘⌥↑", "Ctrl+Alt+↑"), keys("⌘⌥↓", "Ctrl+Alt+↓")]]] },
  {
    id: "message",
    rows: [
      ["send", ["Enter"]],
      ["newline", ["Shift+Enter"]],
      ["leaveDock", ["Esc"]],
      ["speak", [keys("⌘⇧L", "Ctrl+Shift+L")]],
      ["pinModel", [keys("⌥P", "Alt+P")]],
    ],
  },
];

export function ShortcutList() {
  const t = useText(text);
  return (
    <div className="shortcut-list">
      {sections.map((section) => (
        <section key={section.id}>
          <h3>{t.sections[section.id]}</h3>
          <dl>
            {section.rows.map(([item, combos]) => (
              <div key={item} className="shortcut-row">
                <dt>{t.items[item]}</dt>
                <dd>
                  {combos.map((combo) => (
                    <kbd key={combo}>{combo}</kbd>
                  ))}
                </dd>
              </div>
            ))}
          </dl>
        </section>
      ))}
    </div>
  );
}

export function ShortcutsDialog({ onClose }: { onClose: () => void }) {
  const t = useText(text);
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
  }, []);
  return (
    <dialog ref={dialog} className="create-dialog" aria-labelledby="shortcuts-title" onClose={onClose}>
      <section className="panel create create-modal shortcuts-modal">
        <div className="page-heading">
          <h2 id="shortcuts-title">{t.title}</h2>
          <IconButton type="button" className="ghost" icon="x" label={t.close} onClick={() => dialog.current?.close()} />
        </div>
        <ShortcutList />
      </section>
    </dialog>
  );
}
