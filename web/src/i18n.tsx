import { useSyncExternalStore } from "react";
import { api, type ChatDefaults } from "./api";

export type Language = ChatDefaults["language"];
export type Lang = "en" | "es";

const listeners = new Set<() => void>();
let language: Language = "system";

export const lang = (): Lang =>
  language === "system" ? (navigator.language.toLowerCase().startsWith("es") ? "es" : "en") : language;

// Keeps the user's region (es-EC, en-GB) when it matches the chosen language.
export const locale = () => (navigator.language.toLowerCase().startsWith(lang()) ? navigator.language : lang());

export function setLanguage(next: Language) {
  language = next;
  document.documentElement.lang = lang();
  listeners.forEach((listener) => listener());
}

export function initLanguage() {
  setLanguage("system");
  api
    .get<ChatDefaults>("/api/settings/defaults")
    .then((defaults) => setLanguage(defaults.language))
    .catch((reason) => console.error("load language", reason));
}

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

export function useLanguage() {
  useSyncExternalStore(subscribe, () => language);
  return language;
}

// Each component keeps its own strings: useText({ en: {...}, es: {...} }). Spanish must match the English keys.
export function useText<T>(text: { en: T; es: NoInfer<T> }): T {
  useLanguage();
  return text[lang()];
}
