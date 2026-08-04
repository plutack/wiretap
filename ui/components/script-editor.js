// ScriptEditor is the JS script editor from PLAN.md §6.1/§6.3. It uses
// CodeMirror 5 (loaded as the UMD global window.CodeMirror via a <script> tag in
// index.html, NOT an ES module) for the body, plus fields for name/trigger/
// priority/enabled and a test-run panel backed by TestScript.
//
// Props:
//   script   — the ScriptView being edited, or a blank {id:0,...} for New
//   onSave   — async (ScriptInput) → id
//   onDelete — async (id) → void
//   onTest   — async (ScriptTestRequest) → ScriptTestView
//   onClose  — () → void
import { html } from "../vendor/preact/index.js";
import { useEffect, useRef, useState } from "../vendor/preact/index.js";

const TRIGGERS = ["on_request", "on_response", "on_replay", "on_webhook"];

export function ScriptEditor({ script, onSave, onDelete, onTest, onClose }) {
  const [name, setName] = useState(script.name || "");
  const [trigger, setTrigger] = useState(script.trigger || "on_request");
  const [priority, setPriority] = useState(script.priority || 0);
  const [enabled, setEnabled] = useState(script.enabled ?? true);
  const [saveState, setSaveState] = useState(null); // {msg, error}
  const [testResult, setTestResult] = useState(null); // ScriptTestView | {error}

  const taRef = useRef(null);
  const cmRef = useRef(null);

  // Attach CodeMirror once, on mount. The body lives inside CodeMirror (not
  // React state) so we read it via cmRef.current.getValue() at save/test time.
  useEffect(() => {
    if (!taRef.current || cmRef.current) return;
    if (!window.CodeMirror) {
      // Graceful fallback: leave the plain textarea usable if CM failed to load.
      taRef.current.value = script.body || "";
      return;
    }
    cmRef.current = window.CodeMirror.fromTextArea(taRef.current, {
      mode: "javascript",
      theme: "default",
      lineNumbers: true,
      tabSize: 2,
      indentUnit: 2,
    });
    cmRef.current.setValue(script.body || "");
    cmRef.current.setSize("100%", 260);
    return () => {
      if (cmRef.current) {
        cmRef.current.toTextArea();
        cmRef.current = null;
      }
    };
  }, []);

  // When switching to a different script, push its body into the editor.
  useEffect(() => {
    if (cmRef.current) {
      cmRef.current.setValue(script.body || "");
    } else if (taRef.current) {
      taRef.current.value = script.body || "";
    }
    setName(script.name || "");
    setTrigger(script.trigger || "on_request");
    setPriority(script.priority || 0);
    setEnabled(script.enabled ?? true);
    setSaveState(null);
    setTestResult(null);
  }, [script.id]);

  const currentBody = () =>
    cmRef.current ? cmRef.current.getValue() : taRef.current?.value || "";

  const handleSave = async () => {
    setSaveState({ msg: "saving…" });
    try {
      const id = await onSave({
        id: script.id || 0,
        name,
        trigger,
        body: currentBody(),
        priority: Number(priority) || 0,
        enabled,
      });
      setSaveState({ msg: `saved (id ${id})` });
    } catch (e) {
      setSaveState({ error: String(e) });
    }
  };

  const handleDelete = async () => {
    if (!script.id) {
      onClose();
      return;
    }
    try {
      await onDelete(script.id);
      onClose();
    } catch (e) {
      setSaveState({ error: String(e) });
    }
  };

  const handleTest = async () => {
    setTestResult({ pending: true });
    try {
      // A minimal sample exchange; the script body decides what it inspects.
      const result = await onTest({
        body: currentBody(),
        method: "POST",
        url: "https://example.com/webhook",
        headers: { "Content-Type": "application/json" },
        req_body: '{"hello":"world"}',
        status: 200,
      });
      setTestResult(result);
    } catch (e) {
      // Only ErrScriptEngineUnavailable is thrown; script exceptions come back
      // in result.error instead.
      setTestResult({ error: String(e) });
    }
  };

  return html`<aside
    class="flex w-1/2 min-w-[32rem] flex-col overflow-auto border-l border-neutral-800 bg-neutral-900/40"
  >
    <div class="flex items-center gap-2 border-b border-neutral-800 px-4 py-2">
      <h2 class="text-sm font-semibold text-neutral-100">
        ${script.id ? `Script #${script.id}` : "New script"}
      </h2>
      <button
        onClick=${onClose}
        class="ml-auto text-neutral-500 hover:text-neutral-200"
      >
        ✕
      </button>
    </div>

    <div class="flex-1 overflow-auto px-4 py-3 text-sm">
      <div class="mb-3 grid grid-cols-2 gap-3">
        <label class="block text-xs">
          <span class="mb-1 block text-neutral-400">Name</span>
          <input
            type="text"
            value=${name}
            onInput=${(e) => setName(e.target.value)}
            class="w-full rounded border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm"
          />
        </label>
        <label class="block text-xs">
          <span class="mb-1 block text-neutral-400">Trigger</span>
          <select
            value=${trigger}
            onChange=${(e) => setTrigger(e.target.value)}
            class="w-full rounded border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm"
          >
            ${TRIGGERS.map((t) => html`<option value=${t}>${t}</option>`)}
          </select>
        </label>
        <label class="block text-xs">
          <span class="mb-1 block text-neutral-400">Priority</span>
          <input
            type="number"
            value=${priority}
            onInput=${(e) => setPriority(e.target.value)}
            class="w-full rounded border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm"
          />
        </label>
        <label class="flex items-end gap-2 text-xs">
          <input
            type="checkbox"
            checked=${enabled}
            onChange=${(e) => setEnabled(e.target.checked)}
          />
          <span class="text-neutral-400">Enabled</span>
        </label>
      </div>

      <div class="mb-3">
        <span class="mb-1 block text-xs text-neutral-400">Body</span>
        <div class="overflow-hidden rounded border border-neutral-700">
          <textarea ref=${taRef}></textarea>
        </div>
      </div>

      <div class="mb-3 flex items-center gap-2">
        <button
          onClick=${handleSave}
          class="rounded bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500"
        >
          Save
        </button>
        <button
          onClick=${handleTest}
          class="rounded border border-neutral-700 px-3 py-1 text-sm hover:bg-neutral-800"
        >
          Test run
        </button>
        <button
          onClick=${handleDelete}
          class="ml-auto rounded border border-rose-800 px-3 py-1 text-sm text-rose-400 hover:bg-rose-900/40"
        >
          ${script.id ? "Delete" : "Cancel"}
        </button>
      </div>

      ${saveState &&
      html`<p
        class="mb-3 text-xs ${saveState.error
          ? "text-rose-400"
          : "text-emerald-400"}"
      >
        ${saveState.error || saveState.msg}
      </p>`}

      ${testResult && html`<${TestPanel} result=${testResult} />`}
    </div>
  </aside>`;
}

function TestPanel({ result }) {
  if (result.pending) {
    return html`<p class="text-xs text-neutral-400">running…</p>`;
  }
  if (result.error) {
    return html`<div class="rounded border border-rose-800 bg-rose-900/20 p-2">
      <p class="text-xs text-rose-400">engine error: ${result.error}</p>
    </div>`;
  }
  return html`<section
    class="rounded border border-neutral-800 bg-neutral-950 p-2 text-xs"
  >
    <h3 class="mb-2 text-xs uppercase text-neutral-500">Test result</h3>
    ${result.rejected &&
    html`<p class="mb-2 text-rose-400">
      rejected${result.reject_reason ? `: ${result.reject_reason}` : ""}
    </p>`}
    ${result.error &&
    html`<p class="mb-2 text-rose-400">script error: ${result.error}</p>`}
    <dl class="mb-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 font-mono">
      <dt class="text-neutral-500">method</dt>
      <dd class="text-neutral-300">${result.method}</dd>
      <dt class="text-neutral-500">url</dt>
      <dd class="text-neutral-300 break-all">${result.url}</dd>
      <dt class="text-neutral-500">status</dt>
      <dd class="text-neutral-300">${result.status}</dd>
    </dl>
    ${result.req_body &&
    html`<div class="mb-2">
      <span class="text-neutral-500">req body</span>
      <pre
        class="mt-1 max-h-32 overflow-auto rounded bg-neutral-900 p-1.5 font-mono break-all whitespace-pre-wrap"
        >${result.req_body}</pre
      >
    </div>`}
    ${result.resp_body &&
    html`<div class="mb-2">
      <span class="text-neutral-500">resp body</span>
      <pre
        class="mt-1 max-h-32 overflow-auto rounded bg-neutral-900 p-1.5 font-mono break-all whitespace-pre-wrap"
        >${result.resp_body}</pre
      >
    </div>`}
    ${result.logs &&
    result.logs.length > 0 &&
    html`<div>
      <span class="text-neutral-500">console</span>
      <pre
        class="mt-1 max-h-32 overflow-auto rounded bg-neutral-900 p-1.5 font-mono whitespace-pre-wrap"
        >${result.logs.join("\n")}</pre
      >
    </div>`}
  </section>`;
}
