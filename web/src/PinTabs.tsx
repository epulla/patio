import { useState, type DragEvent, type ReactNode } from "react";
import type { Chat } from "./api";
import { statusIcons, statusLabels, type ChatStatus } from "./chatStatus";
import { Icon, IconButton } from "./icons";
import Menu from "./Menu";
import { openShortcuts, shortcutsAriaShortcut, shortcutsShortcut } from "./Shortcuts";
import { groupAriaShortcuts, groupShortcuts, maxPins, moveTab, newPinnedAriaShortcut, newPinnedShortcut, pinAriaShortcut, pinShortcut, pinStyle, requestNewPinnedChat, setActiveGroup, unpinAriaShortcut, unpinShortcut, usePinGroups } from "./pins";
import { useText } from "./i18n";

const text = {
  en: { pinnedChats: "Pinned chats", chat: "Chat", unpin: (title: string, shortcut: string) => `Unpin ${title} (${shortcut})`, switchGroup: (name: string, shortcut: string) => `Pin group: ${name} (${shortcut})`, newPinned: (shortcut: string) => `New pinned chat (${shortcut})`, groupFull: (name: string) => `${name} is full (max ${maxPins})`, closePreview: (title: string) => `Close ${title}`, preview: "not pinned", shortcuts: (shortcut: string) => `Keyboard shortcuts (${shortcut})` },
  es: { pinnedChats: "Chats fijados", chat: "Chat", unpin: (title: string, shortcut: string) => `Quitar ${title} de fijados (${shortcut})`, switchGroup: (name: string, shortcut: string) => `Grupo fijado: ${name} (${shortcut})`, newPinned: (shortcut: string) => `Nuevo chat fijado (${shortcut})`, groupFull: (name: string) => `${name} está lleno (máx. ${maxPins})`, closePreview: (title: string) => `Cerrar ${title}`, preview: "sin fijar", shortcuts: (shortcut: string) => `Atajos de teclado (${shortcut})` },
};

// Tabs of the active pin group: group switcher, one tab per pinned chat, then +. Extra buttons go in children.
// preview is an unpinned open chat, shown as an italic tab until another chat replaces it.
// placed marks tabs shown in a split pane; onTabDragStart also lets tabs be dropped onto panes.

const reorderType = "application/x-pin-tab";

// WebKit snapshots only painted pixels, so drag a solid card instead of the transparent tab or header.
export function dragGhost(event: DragEvent, title: string) {
  const ghost = document.createElement("div");
  ghost.className = "pane-drag-ghost";
  ghost.textContent = title;
  document.body.append(ghost);
  event.dataTransfer.setDragImage(ghost, 20, 20);
  setTimeout(() => ghost.remove());
}

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
  // Insertion point while a tab is dragged over the bar: before the tab at this index.
  const [dropAt, setDropAt] = useState<number>();

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
      <div
        className="pin-tab-list"
        aria-label={t.pinnedChats}
        onDragLeave={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropAt(undefined);
        }}
        onDrop={(event) => {
          const id = event.dataTransfer.getData(reorderType);
          if (!id || dropAt === undefined) return;
          event.preventDefault();
          moveTab(id, dropAt);
          setDropAt(undefined);
        }}
      >
        {group.ids.map((id, index) => {
          const title = titleOf(id);
          const state = status[id];
          return (
            <div
              key={id}
              className="pin-tab"
              data-active={id === current || undefined}
              data-placed={placed?.includes(id) || undefined}
              data-drop={dropAt === index ? "before" : dropAt === index + 1 && index === group.ids.length - 1 ? "after" : undefined}
              onDragOver={(event) => {
                if (!event.dataTransfer.types.includes(reorderType)) return;
                event.preventDefault();
                const rect = event.currentTarget.getBoundingClientRect();
                setDropAt(event.clientX < rect.left + rect.width / 2 ? index : index + 1);
              }}
            >
              <button
                className="pin-tab-button"
                draggable
                onDragStart={(event) => {
                  event.dataTransfer.setData(reorderType, id);
                  event.dataTransfer.effectAllowed = "move";
                  if (onTabDragStart) onTabDragStart(id, event, title);
                  else dragGhost(event, title);
                }}
                onDragEnd={() => {
                  setDropAt(undefined);
                  onTabDragEnd?.();
                }}
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
      <IconButton
        className="ghost"
        icon="keyboard"
        label={t.shortcuts(shortcutsShortcut)}
        aria-keyshortcuts={shortcutsAriaShortcut}
        onClick={openShortcuts}
      />
      {children}
    </div>
  );
}
