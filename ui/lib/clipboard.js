import {
  ClipboardGetText,
  ClipboardSetText,
} from "../wailsjs/runtime/runtime.js";

function hasWailsRuntime() {
  return typeof window !== "undefined" && Boolean(window.runtime);
}

export async function copyText(text) {
  const value = String(text ?? "");

  if (hasWailsRuntime()) {
    const copied = await ClipboardSetText(value);
    if (!copied) throw new Error("the desktop clipboard rejected the copy");
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
  if (hasWailsRuntime()) return ClipboardGetText();
  if (navigator.clipboard?.readText) return navigator.clipboard.readText();
  throw new Error("clipboard access is unavailable");
}
