import { useMemo, type MouseEvent } from "react";
import DOMPurify from "dompurify";
import { marked } from "marked";
import { navigate } from "./App";

const appRoute =
  /^(?:\/(?:tickets|boards)\/[^/]+|\/print\/tickets\/[^/]+|\/(?:tickets|calendar|settings)|\/ai\/(?:connectors|skills|mcp|chats(?:\/[^/]+)?))$/;

export default function Markdown({ text, onFile }: { text: string; onFile?: (path: string) => void }) {
  const html = useMemo(
    () => DOMPurify.sanitize(marked.parse(text, { async: false, gfm: true, breaks: true }), { FORBID_TAGS: ["img"] }),
    [text],
  );
  function onClick(event: MouseEvent<HTMLDivElement>) {
    const link = (event.target as HTMLElement).closest("a");
    const href = link?.getAttribute("href");
    if (!href) return;
    event.preventDefault();
    const route = href.split(/[?#]/, 1)[0];
    const chat = route.match(/^\/ai\/chats\/([^/]+)$/);
    if (chat) dispatchEvent(new CustomEvent("open-chat-link", { detail: chat[1] }));
    else if (route === "/" || appRoute.test(route)) navigate(href);
    else if (/^https?:\/\//i.test(href)) {
      if (window.runtime) window.runtime.BrowserOpenURL(href);
      else window.open(href, "_blank", "noopener");
    } else if (/^file:\/\//i.test(href)) {
      onFile?.(href.split("#", 1)[0]);
    } else if (
      !href.startsWith("//") &&
      !href.startsWith("#") &&
      !href.startsWith("?") &&
      (!/^[a-z][a-z0-9+.-]*:/i.test(href) || /^[a-z]:[\\/]/i.test(href))
    ) {
      const path = href.split(/[?#]/, 1)[0];
      try {
        onFile?.(decodeURIComponent(path));
      } catch {
        onFile?.(path);
      }
    }
  }
  return <div className="markdown" onClick={onClick} dangerouslySetInnerHTML={{ __html: html }} />;
}
