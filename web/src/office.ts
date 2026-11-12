// Runs inside office.html, an iframe whose CSP blocks script injected through the rendered documents.
const root = document.getElementById("root")!;

addEventListener("message", async (event: MessageEvent<{ kind: "docx" | "pptx"; data: ArrayBuffer }>) => {
  if (event.source !== parent) return;
  const { kind, data } = event.data;
  try {
    if (kind === "docx") {
      const { renderAsync } = await import("docx-preview");
      await renderAsync(data, root, undefined, { ignoreLastRenderedPageBreak: true });
    } else {
      const { init } = await import("pptx-preview");
      const width = Math.min(960, innerWidth - 32);
      await init(root, { width, height: Math.round((width * 9) / 16), mode: "list" }).preview(data);
    }
    parent.postMessage({ ok: true }, "*");
  } catch (error) {
    root.replaceChildren(Object.assign(document.createElement("p"), { className: "error", textContent: String(error) }));
    parent.postMessage({ error: String(error) }, "*");
  }
});

parent.postMessage({ ready: true }, "*");
