import { useEffect, useMemo, useRef, useState, type MouseEvent } from "react";
import DOMPurify from "dompurify";
import { marked } from "marked";
import { navigate } from "./App";
import { iconSvg } from "./icons";
import { useText } from "./i18n";
import LinkPreview, { previewRoute } from "./LinkPreview";

const appRoute =
  /^(?:\/(?:tickets|boards)\/[^/]+|\/print\/tickets\/[^/]+|\/(?:tickets|calendar|settings|dashboard)|\/ai\/(?:connectors|skills|mcp|chats(?:\/[^/]+)?))$/;

// Added after sanitizing; clicks are handled in onClick below.
const copyCodeButton = (copy: string) => `<button type="button" class="icon ghost code-copy" aria-label="${copy}" data-tip="${copy}">${iconSvg("copy")}${iconSvg("check")}</button>`;

function markdownHtml(text: string) {
  return DOMPurify.sanitize(marked.parse(text, { async: false, gfm: true, breaks: true }), { FORBID_TAGS: ["img"] });
}

// Copies rich text for apps that paste HTML (Docs, Notes, Slack) and the Markdown source for the rest.
// In-app links (e.g. /tickets/<id>) mean nothing outside Patio, so they become plain text.
export async function copyMarkdown(text: string) {
  if (typeof ClipboardItem === "undefined") return navigator.clipboard.writeText(text);
  const doc = new DOMParser().parseFromString(markdownHtml(text), "text/html");
  doc.querySelectorAll("a").forEach((link) => {
    if (!/^(?:https?|mailto):/i.test(link.getAttribute("href") ?? "")) link.replaceWith(...link.childNodes);
  });
  try {
    await navigator.clipboard.write([
      new ClipboardItem({
        "text/html": new Blob([doc.body.innerHTML], { type: "text/html" }),
        "text/plain": new Blob([text], { type: "text/plain" }),
      }),
    ]);
  } catch {
    await navigator.clipboard.writeText(text);
  }
}

export default function Markdown({ text, onFile }: { text: string; onFile?: (path: string) => void }) {
  const t = useText({ en: { copy: "Copy code", copied: "Copied" }, es: { copy: "Copiar código", copied: "Copiado" } });
  const html = useMemo(
    () =>
      markdownHtml(text)
        .replace(/<pre[\s>]/g, (open) => `<div class="code-block">${copyCodeButton(t.copy)}${open}`)
        .replaceAll("</pre>", "</pre></div>"),
    [text, t.copy],
  );
  const [preview, setPreview] = useState<{ href: string; anchor: DOMRect } | null>(null);
  const timer = useRef<number>(undefined);
  const schedule = (update: () => void, delay: number) => {
    clearTimeout(timer.current);
    timer.current = window.setTimeout(update, delay);
  };
  const hidePreview = () => schedule(() => setPreview(null), 200);
  useEffect(() => () => clearTimeout(timer.current), []);
  useEffect(() => {
    if (!preview) return;
    const close = () => setPreview(null);
    const onKey = (event: KeyboardEvent) => event.key === "Escape" && close();
    addEventListener("keydown", onKey);
    addEventListener("scroll", close, true);
    return () => {
      removeEventListener("keydown", onKey);
      removeEventListener("scroll", close, true);
    };
  }, [preview]);
  // mouseover/mouseout also fire when moving between a link's children; relatedTarget filters those out.
  function onMouseOver(event: MouseEvent<HTMLDivElement>) {
    const link = (event.target as HTMLElement).closest("a");
    if (!link || link.contains(event.relatedTarget as Node | null)) return;
    const href = link.getAttribute("href")?.split(/[?#]/, 1)[0] ?? "";
    if (previewRoute.test(href)) schedule(() => setPreview({ href, anchor: link.getBoundingClientRect() }), 400);
  }
  function onMouseOut(event: MouseEvent<HTMLDivElement>) {
    const link = (event.target as HTMLElement).closest("a");
    if (link && !link.contains(event.relatedTarget as Node | null)) hidePreview();
  }
  function onClick(event: MouseEvent<HTMLDivElement>) {
    const copy = (event.target as HTMLElement).closest<HTMLButtonElement>(".code-copy");
    if (copy) {
      const code = copy.nextElementSibling?.textContent ?? "";
      void navigator.clipboard.writeText(code).then(() => {
        const label = (text: string) => {
          copy.dataset.tip = text;
          copy.setAttribute("aria-label", text);
        };
        copy.classList.add("copied");
        label(t.copied);
        setTimeout(() => {
          copy.classList.remove("copied");
          label(t.copy);
        }, 1500);
      }, () => undefined);
      return;
    }
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
  return (
    <>
      <div className="markdown" onClick={onClick} onMouseOver={onMouseOver} onMouseOut={onMouseOut} dangerouslySetInnerHTML={{ __html: html }} />
      {preview && (
        <LinkPreview key={preview.href} {...preview} onEnter={() => clearTimeout(timer.current)} onLeave={hidePreview} />
      )}
    </>
  );
}
