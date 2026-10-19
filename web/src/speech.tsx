import { useEffect, useRef, useSyncExternalStore } from "react";
import { marked } from "marked";
import { IconButton } from "./icons";
import { useText } from "./i18n";

const textLabels = {
  en: { stopReading: "Stop reading", readAloud: "Read aloud" },
  es: { stopReading: "Dejar de leer", readAloud: "Leer en voz alta" },
};

export const canSpeak = typeof speechSynthesis !== "undefined" && typeof SpeechSynthesisUtterance !== "undefined";

const mac = /Mac|iPhone|iPad/.test(navigator.platform);
export const speakShortcut = mac ? "⌘⇧L" : "Ctrl+Shift+L";
const speakAriaShortcut = mac ? "Meta+Shift+L" : "Control+Shift+L";

// Reads the latest reply of the focused chat (or the only chat on screen).
if (canSpeak) {
  addEventListener("keydown", (event) => {
    if (!(event.metaKey || event.ctrlKey) || !event.shiftKey || event.altKey || event.key.toLowerCase() !== "l") return;
    const conversations = document.querySelectorAll(".conversation");
    const conversation =
      document.activeElement?.closest(".conversation") ?? (conversations.length === 1 ? conversations[0] : null);
    const buttons = conversation?.querySelectorAll<HTMLButtonElement>("[data-speak]");
    if (!buttons?.length) return;
    event.preventDefault();
    buttons[buttons.length - 1].click();
  });
}

// Tracked here instead of via utterance events: WebKit doesn't always fire `end` after `cancel()`.
let active: SpeechSynthesisUtterance | null = null;
const listeners = new Set<() => void>();

function setActive(utterance: SpeechSynthesisUtterance | null) {
  active = utterance;
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

// Reads the rendered text, not the Markdown source, and skips code blocks.
// Each block becomes a sentence so headings and list items get a pause.
export function speechText(markdown: string) {
  const html = marked.parse(markdown, { async: false, gfm: true });
  const doc = new DOMParser().parseFromString(html, "text/html");
  doc.querySelectorAll("pre").forEach((block) => block.remove());
  return (doc.body.textContent ?? "")
    .split("\n")
    .map((line) => line.replace(/\s+/g, " ").trim())
    .filter(Boolean)
    .map((line) => (/[.!?:;,]$/.test(line) ? line : `${line}.`))
    .join(" ");
}

// Browsers have no language detection API in WebKit, so score common function words.
const stopwords: Record<string, string[]> = {
  en: ["the", "and", "is", "are", "you", "that", "this", "with", "for", "have", "it", "of", "to", "not", "be", "was", "what"],
  es: ["el", "los", "las", "y", "es", "por", "para", "pero", "como", "está", "del", "al", "se", "lo", "muy", "también", "hay", "esto", "eso"],
  pt: ["os", "um", "não", "você", "mas", "também", "isso", "são", "do", "da", "em", "é", "ao", "uma"],
  fr: ["le", "les", "des", "et", "est", "pour", "avec", "pas", "qui", "dans", "vous", "je", "ce", "sur", "une"],
  de: ["der", "die", "das", "und", "ist", "nicht", "ein", "eine", "mit", "zu", "den", "von", "für", "auf", "ich", "auch"],
  it: ["il", "gli", "di", "che", "è", "per", "non", "della", "sono", "ma", "anche", "questo"],
};
const stopwordSets = Object.entries(stopwords).map(([lang, words]) => [lang, new Set(words)] as const);

export function guessLanguage(text: string): string | undefined {
  const words = text.toLowerCase().match(/\p{L}+/gu) ?? [];
  const scores = stopwordSets.map(([lang, set]) => [lang, words.filter((word) => set.has(word)).length] as const);
  const ranked = scores
    .map(([lang, score]) => [lang, score + (lang === "es" && /[ñ¿¡]/i.test(text) ? 3 : 0)] as const)
    .sort((a, b) => b[1] - a[1]);
  const [best, second] = ranked;
  return best[1] >= 2 && best[1] > second[1] ? best[0] : undefined;
}

const normalize = (lang: string) => lang.toLowerCase().replace("_", "-");

// Keeps the system voice when it already speaks the language; otherwise prefers the user's
// region and higher-quality voices, and avoids macOS novelty/Eloquence voices.
function pickVoice(lang: string) {
  const voices = speechSynthesis.getVoices().filter((voice) => normalize(voice.lang).split("-")[0] === lang);
  const system = voices.find((voice) => voice.default);
  if (system) return system;
  const regions = navigator.languages.map(normalize).filter((preferred) => preferred.split("-")[0] === lang);
  const rank = (voice: SpeechSynthesisVoice) => {
    const id = `${voice.voiceURI} ${voice.name}`.toLowerCase();
    return (
      (regions.includes(normalize(voice.lang)) ? 4 : 0) +
      (id.includes("premium") ? 3 : id.includes("enhanced") || id.includes("natural") ? 2 : 0) -
      (id.includes("speech.synthesis") || id.includes("eloquence") ? 5 : 0)
    );
  };
  return voices.sort((a, b) => rank(b) - rank(a))[0];
}

export function SpeakButton({ text, shortcut }: { text: string; shortcut?: boolean }) {
  const t = useText(textLabels);
  const mine = useRef<SpeechSynthesisUtterance | null>(null);
  const current = useSyncExternalStore(subscribe, () => active);
  const speaking = current !== null && current === mine.current;

  useEffect(
    () => () => {
      if (mine.current && active === mine.current) {
        speechSynthesis.cancel();
        setActive(null);
      }
    },
    [],
  );

  function toggle() {
    speechSynthesis.cancel();
    if (speaking) {
      setActive(null);
      return;
    }
    const spoken = speechText(text);
    if (!spoken) return;
    const utterance = new SpeechSynthesisUtterance(spoken);
    const lang = guessLanguage(spoken);
    if (lang) {
      const voice = pickVoice(lang);
      utterance.lang = voice?.lang ?? lang;
      if (voice) utterance.voice = voice;
    }
    const done = () => {
      if (active === utterance) setActive(null);
    };
    utterance.onend = done;
    utterance.onerror = done;
    mine.current = utterance;
    setActive(utterance);
    speechSynthesis.speak(utterance);
  }

  const label = speaking ? t.stopReading : t.readAloud;
  return (
    <IconButton
      type="button"
      className="ghost"
      icon={speaking ? "volumeX" : "volume"}
      label={shortcut ? `${label} (${speakShortcut})` : label}
      aria-keyshortcuts={shortcut ? speakAriaShortcut : undefined}
      aria-pressed={speaking}
      data-speak=""
      onClick={toggle}
    />
  );
}
