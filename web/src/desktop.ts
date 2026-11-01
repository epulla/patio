import { Browser, Events } from "@wailsio/runtime";

type Bridge = { chrome?: { webview?: unknown }; webkit?: { messageHandlers?: { external?: unknown } } };
const bridge = window as unknown as Bridge;

// Same native message channels the Wails runtime uses; absent in a plain browser.
export const isDesktop = Boolean(bridge.chrome?.webview || bridge.webkit?.messageHandlers?.external);

export function onEvent(name: string, callback: (data: unknown) => void): () => void {
  return Events.On(name, (event) => callback(event.data));
}

export function openURL(url: string) {
  Browser.OpenURL(url).catch(console.error);
}
