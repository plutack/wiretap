import { html } from "../vendor/preact/index.js";
import { useEffect, useRef, useState } from "../vendor/preact/index.js";
import { Button, Input, Select } from "./ui.js";
import { CodeBlock } from "./code-block.js";
import { HeaderTable, StatusBadge } from "./badges.js";
import { fmtBytes } from "../lib/format.js";
import { pasteText } from "../lib/clipboard.js";

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"].map((value) => ({ value, label: value }));
const EMPTY = { method: "POST", url: "", headers: { "Content-Type": ["application/json"] }, body: "{\n  \n}", apply_transforms: true };

function normalizeHeaders(value) {
  const out = {};
  for (const [key, raw] of Object.entries(value || {})) {
    out[key] = Array.isArray(raw) ? raw.map(String) : [String(raw)];
  }
  return out;
}

function importJSON(text) {
  const parsed = JSON.parse(text);
  const envelope = parsed && typeof parsed === "object" && !Array.isArray(parsed) && "url" in parsed;
  if (!envelope) return { ...EMPTY, body: JSON.stringify(parsed, null, 2) };
  const body = parsed.body == null ? "" : typeof parsed.body === "string" ? parsed.body : JSON.stringify(parsed.body, null, 2);
  return {
    method: String(parsed.method || "POST").toUpperCase(),
    url: String(parsed.url || ""),
    headers: normalizeHeaders(parsed.headers || { "Content-Type": ["application/json"] }),
    body,
    apply_transforms: parsed.apply_transforms !== false,
  };
}

function responseContentType(headers) {
  const pair = Object.entries(headers || {}).find(([key]) => key.toLowerCase() === "content-type");
  return pair ? String(pair[1]?.[0] || "") : "";
}

export function RequestComposer({ initialRequest, recipes = [], onApplyRecipe, onSend, onToast }) {
  const [mode, setMode] = useState("manual");
  const [request, setRequest] = useState(initialRequest || EMPTY);
  const [headersText, setHeadersText] = useState(JSON.stringify((initialRequest || EMPTY).headers, null, 2));
  const [response, setResponse] = useState(null);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const [recipeID, setRecipeID] = useState("");
  const [baseURL, setBaseURL] = useState("http://localhost:1700");
  const [source, setSource] = useState("");
  const [preparing, setPreparing] = useState(false);
  const fileRef = useRef(null);
  const sourceFileRef = useRef(null);

  useEffect(() => {
    const next = initialRequest || EMPTY;
    setRequest(next);
    setHeadersText(JSON.stringify(next.headers || {}, null, 2));
    setResponse(null);
    setError("");
    if (initialRequest) setMode("manual");
  }, [initialRequest]);

  useEffect(() => {
    if (!recipes.some((recipe) => recipe.id === recipeID)) setRecipeID(recipes[0]?.id || "");
  }, [recipes, recipeID]);

  const applyImported = (text) => {
    try {
      const next = importJSON(text);
      setRequest(next);
      setHeadersText(JSON.stringify(next.headers, null, 2));
      setResponse(null);
      setError("");
      onToast("Request JSON imported");
    } catch (e) {
      setError("Invalid JSON: " + e);
    }
  };

  const importFile = async (event) => {
    const file = event.target.files?.[0];
    if (file) applyImported(await file.text());
    event.target.value = "";
  };

  const importSourceFile = async (event) => {
    const file = event.target.files?.[0];
    if (file) {
      setSource(await file.text());
      setError("");
    }
    event.target.value = "";
  };

  const pasteSource = async () => {
    try {
      setSource(await pasteText());
      setError("");
    } catch (e) {
      setError("Paste failed: " + e);
    }
  };

  const prepare = async () => {
    setError("");
    if (!recipeID) {
      setError("Create and enable an on_compose recipe first.");
      return;
    }
    if (!source.trim()) {
      setError("Paste or import source JSON first.");
      return;
    }
    setPreparing(true);
    try {
      const next = await onApplyRecipe({ recipe_id: recipeID, source, base_url: baseURL });
      setRequest(next);
      setHeadersText(JSON.stringify(next.headers || {}, null, 2));
      setResponse(null);
      setMode("manual");
      onToast("Recipe applied. Review the request before sending.");
    } catch (e) {
      setError(String(e));
    } finally {
      setPreparing(false);
    }
  };

  const send = async () => {
    setError("");
    let headers;
    try {
      headers = normalizeHeaders(JSON.parse(headersText || "{}"));
    } catch (e) {
      setError("Headers must be a JSON object: " + e);
      return;
    }
    setSending(true);
    try {
      setResponse(await onSend({ ...request, headers }));
    } catch (e) {
      setError(String(e));
    } finally {
      setSending(false);
    }
  };

  const recipeOptions = recipes.map((recipe) => ({ value: recipe.id, label: recipe.name }));
  const selectedRecipe = recipes.find((recipe) => recipe.id === recipeID);

  return html`<div class="composer-shell">
    <header class="composer-header">
      <div>
        <h2>Request composer</h2>
        <p>Build directly or turn captured source data into an editable request.</p>
      </div>
      <div class="composer-mode-switch" role="tablist" aria-label="Composer input mode">
        <button role="tab" aria-selected=${mode === "manual"} class=${mode === "manual" ? "active" : ""} onClick=${() => { setMode("manual"); setError(""); }}>Manual request</button>
        <button role="tab" aria-selected=${mode === "recipe"} class=${mode === "recipe" ? "active" : ""} onClick=${() => { setMode("recipe"); setError(""); }}>Source recipe</button>
      </div>
    </header>

    ${mode === "recipe" ? html`
      <section class="recipe-workspace">
        <div class="recipe-toolbar">
          <div class="recipe-field recipe-select-field">
            <label>Recipe</label>
            <${Select} value=${recipeID} options=${recipeOptions} onChange=${(e) => setRecipeID(e.target.value)} aria-label="Source recipe" />
          </div>
          <div class="recipe-field recipe-base-field">
            <label>Target base URL</label>
            <${Input} class="font-mono" value=${baseURL} onInput=${(e) => setBaseURL(e.target.value)} placeholder="http://localhost:1700" />
          </div>
          <${Button} variant="primary" disabled=${preparing || !recipeID || !source.trim()} onClick=${prepare}>${preparing ? "Preparing..." : "Transform request"}</>
        </div>
        ${recipes.length ? html`<div class="recipe-description">
          <strong>${selectedRecipe?.name || "Select a recipe"}</strong>
          <span>${selectedRecipe?.description || "Local source-to-request recipe"}</span>
        </div>` : html`<div class="recipe-empty">
          <strong>No compose recipes yet</strong>
          <span>Create a transform with the <code>on_compose</code> trigger, then enable it.</span>
        </div>`}
        <div class="recipe-source-head">
          <label>Source JSON</label>
          <div>
            <input ref=${sourceFileRef} type="file" accept="application/json,.json" class="hidden" onChange=${importSourceFile} />
            <${Button} onClick=${() => sourceFileRef.current?.click()}>Import file</>
            <${Button} onClick=${pasteSource}>Paste</>
          </div>
        </div>
        <textarea class="composer-editor recipe-source" spellcheck="false" placeholder="Paste the complete log or provider payload here" value=${source} onInput=${(e) => setSource(e.target.value)}></textarea>
        ${error ? html`<div class="composer-error">${error}</div>` : null}
        <p class="recipe-safety">A recipe prepares a draft only. Review the generated URL, headers, and body before sending.</p>
      </section>
    ` : html`
      <div class="composer-manual-actions">
        <span>Edit every part of the request before delivery.</span>
        <div>
          <input ref=${fileRef} type="file" accept="application/json,.json" class="hidden" onChange=${importFile} />
          <${Button} onClick=${() => fileRef.current?.click()}>Import request JSON</>
          <${Button} onClick=${() => applyImported(JSON.stringify({ hello: "world" }))}>Body example</>
        </div>
      </div>
      <section class="composer-grid ${response ? "has-response" : "no-response"}">
        <div class="composer-panel composer-request">
          <div class="composer-url-row">
            <${Select} value=${request.method} options=${METHODS} onChange=${(e) => setRequest({ ...request, method: e.target.value })} />
            <${Input} class="font-mono" placeholder="http://127.0.0.1:8080/webhook" value=${request.url} onInput=${(e) => setRequest({ ...request, url: e.target.value })} />
            <${Button} variant="primary" disabled=${sending || !request.url.trim()} onClick=${send}>${sending ? "Sending..." : "Send"}</>
          </div>
          <div class="composer-editors">
            <div class="composer-editor-field">
              <label class="composer-label">Headers <span>JSON object</span></label>
              <textarea class="composer-editor composer-headers" spellcheck="false" value=${headersText} onInput=${(e) => setHeadersText(e.target.value)}></textarea>
            </div>
            <div class="composer-editor-field">
              <label class="composer-label">Body <span>text or JSON</span></label>
              <textarea class="composer-editor composer-body" spellcheck="false" value=${request.body} onInput=${(e) => setRequest({ ...request, body: e.target.value })}></textarea>
            </div>
          </div>
          <label class="composer-transform-toggle">
            <input class="checkbox" type="checkbox" checked=${request.apply_transforms} onChange=${(e) => setRequest({ ...request, apply_transforms: e.target.checked })} />
            Apply enabled <code>on_replay</code> transforms before sending
          </label>
          ${error ? html`<div class="composer-error">${error}</div>` : null}
        </div>

        <div class="composer-panel composer-response">
          ${response ? html`
            <div class="composer-response-summary">
              <${StatusBadge} status=${response.status} />
              <strong>${response.duration_ms} ms</strong>
              <span>${fmtBytes(response.body_len)}</span>
              ${response.truncated ? html`<span class="text-amber-400">preview truncated</span>` : null}
            </div>
            <div class="composer-label">Response headers</div>
            <${HeaderTable} headers=${response.headers} />
            <div class="composer-label">Response body</div>
            <${CodeBlock} bodyBase64=${response.body_base64} bodyLength=${response.body_len} truncated=${response.truncated} contentType=${responseContentType(response.headers)} />
          ` : html`<div class="composer-response-empty"><span>↗</span><strong>Response inspector</strong><p>Send the request to inspect status, headers, body, and timing.</p></div>`}
        </div>
      </section>
    `}
  </div>`;
}
