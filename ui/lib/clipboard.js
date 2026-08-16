// Clipboard helpers. Inside the Wails webview the v3 runtime module (served
// at /wails/runtime.js, exposed as window.wails) provides the OS clipboard;
// elsewhere we degrade to the browser APIs. The runtime import is dynamic so
// a plain-browser preview of the UI never fails on a missing module.

function hasWailsRuntime() {
  return typeof window !== "undefined" && Boolean(window.wails);
}

async function wailsClipboard() {
  return import("/wails/runtime.js").then((runtime) => runtime.Clipboard);
}

export async function copyText(text) {
  const value = String(text ?? "");

  if (hasWailsRuntime()) {
    const clipboard = await wailsClipboard();
    await clipboard.SetText(value);
    return;
  }

  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }

  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("clipboard access is unavailable");
}

export async function pasteText() {
  if (hasWailsRuntime()) {
    const clipboard = await wailsClipboard();
    return clipboard.Text();
  }
  if (navigator.clipboard?.readText) return navigator.clipboard.readText();
  throw new Error("clipboard access is unavailable");
}
