import type { DragEvent, ReactNode } from "react";
import type { Chat } from "./api";
import { statusIcons, statusLabels, type ChatStatus } from "./chatStatus";
import { Icon, IconButton } from "./icons";
import Menu from "./Menu";
import { groupAriaShortcuts, groupShortcuts, maxPins, newPinnedAriaShortcut, newPinnedShortcut, pinAriaShortcut, pinShortcut, pinStyle, requestNewPinnedChat, setActiveGroup, unpinAriaShortcut, unpinShortcut, usePinGroups } from "./pins";
import { useText } from "./i18n";

const text = {
  en: { pinnedChats: "Pinned chats", chat: "Chat", unpin: (title: string, shortcut: string) => `Unpin ${title} (${shortcut})`, switchGroup: (name: string, shortcut: string) => `Pin group: ${name} (${shortcut})`, newPinned: (shortcut: string) => `New pinned chat (${shortcut})`, groupFull: (name: string) => `${name} is full (max ${maxPins})`, closePreview: (title: string) => `Close ${title}`, preview: "not pinned" },
  es: { pinnedChats: "Chats fijados", chat: "Chat", unpin: (title: string, shortcut: string) => `Quitar ${title} de fijados (${shortcut})`, switchGroup: (name: string, shortcut: string) => `Grupo fijado: ${name} (${shortcut})`, newPinned: (shortcut: string) => `Nuevo chat fijado (${shortcut})`, groupFull: (name: string) => `${name} está lleno (máx. ${maxPins})`, closePreview: (title: string) => `Cerrar ${title}`, preview: "sin fijar" },
};

// Tabs of the active pin group: group switcher, one tab per pinned chat, then +. Extra buttons go in children.
// preview is an unpinned open chat, shown as an italic tab until another chat replaces it.
// placed marks tabs shown in a split pane; onTabDragStart makes tabs draggable onto panes.
export default function PinTabs({
  chats,
  status,
  current,
  preview,
  placed,
  onSelect,
  onUnpin,
  onGroup = setActiveGroup,
  onTabDragStart,
  onTabDragEnd,
  children,
}: {
  chats: Chat[];
  status: Record<string, ChatStatus | undefined>;
  current?: string;
  preview?: { title: string; status?: ChatStatus; onClose: () => void };
  placed?: string[];
  onSelect: (id: string) => void;
  onUnpin: (id: string) => void;
  onGroup?: (id: string) => void;
  onTabDragStart?: (id: string, event: DragEvent, title: string) => void;
  onTabDragEnd?: () => void;
  children?: ReactNode;
}) {
  const t = useText(text);
  const statusText = useText(statusLabels);
  const groups = usePinGroups();
  const group = groups.groups.find((group) => group.id === groups.active)!;
  const titleOf = (id: string) => chats.find((chat) => chat.id === id)?.title ?? t.chat;

  return (
    <div className="pin-tabs" style={pinStyle(group.color)}>
      {groups.groups.length > 1 && (
        <Menu
          label={t.switchGroup(group.name, groupShortcuts)}
          buttonClassName="ghost dock-group"
          button={
            <span title={t.switchGroup(group.name, groupShortcuts)} aria-keyshortcuts={groupAriaShortcuts}>
              <span className="pin-mark" aria-hidden="true"><Icon name="boards" /></span>
              {group.name} <Icon name="chevronDown" />
            </span>
          }
        >
          {(close) =>
            groups.groups.map((item) => (
              <button
                key={item.id}
                role="menuitemradio"
                aria-checked={item.id === groups.active}
                onClick={() => (close(), onGroup(item.id))}
              >
                <span className="menu-check">{item.id === groups.active && <Icon name="check" />}</span>
                <span className="pin-mark" style={pinStyle(item.color)} aria-hidden="true"><Icon name="boards" /></span>
                {item.name} ({item.ids.length})
              </button>
            ))
          }
        </Menu>
      )}
      <div className="pin-tab-list" aria-label={t.pinnedChats}>
        {group.ids.map((id, index) => {
          const title = titleOf(id);
          const state = status[id];
          return (
            <div key={id} className="pin-tab" data-active={id === current || undefined} data-placed={placed?.includes(id) || undefined}>
              <button
                className="pin-tab-button"
                draggable={!!onTabDragStart}
                onDragStart={onTabDragStart && ((event) => onTabDragStart(id, event, title))}
                onDragEnd={onTabDragEnd}
                aria-pressed={id === current}
                aria-label={state ? `${title}, ${statusText[state]}` : title}
                aria-keyshortcuts={pinAriaShortcut(index + 1)}
                title={title}
                onClick={() => onSelect(id)}
              >
                <kbd className="pin-kbd" aria-hidden="true">
                  {pinShortcut(index + 1)}
                </kbd>
                {state && (
                  <span className="dock-dot" data-state={state} aria-hidden="true">
                    <Icon name={statusIcons[state]} />
                  </span>
                )}
                <span className="dock-title">{title}</span>
              </button>
              <IconButton
                className="ghost pin-tab-close"
                icon="x"
                label={t.unpin(title, unpinShortcut)}
                aria-keyshortcuts={id === current ? unpinAriaShortcut : undefined}
                onClick={() => onUnpin(id)}
              />
            </div>
          );
        })}
        {preview && (
          <div className="pin-tab preview" data-active>
            <button
              className="pin-tab-button"
              aria-pressed
              aria-label={`${preview.title}, ${t.preview}${preview.status ? `, ${statusText[preview.status]}` : ""}`}
              title={preview.title}
            >
              {preview.status && (
                <span className="dock-dot" data-state={preview.status} aria-hidden="true">
                  <Icon name={statusIcons[preview.status]} />
                </span>
              )}
              <span className="dock-title">{preview.title}</span>
            </button>
            <IconButton className="ghost pin-tab-close" icon="x" label={t.closePreview(preview.title)} onClick={preview.onClose} />
          </div>
        )}
      </div>
      <IconButton
        className="ghost"
        icon="plus"
        label={group.ids.length >= maxPins ? t.groupFull(group.name) : t.newPinned(newPinnedShortcut)}
        aria-keyshortcuts={newPinnedAriaShortcut}
        disabled={group.ids.length >= maxPins}
        onClick={requestNewPinnedChat}
      />
      {children}
    </div>
  );
}
