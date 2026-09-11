// Trigger a local download without asking the backend for filesystem access.
export function downloadText(filename, contents, contentType = "application/json") {
  const blob = new Blob([contents], { type: `${contentType};charset=utf-8` });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.style.display = "none";
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}
