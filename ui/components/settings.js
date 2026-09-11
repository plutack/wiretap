// Settings renders the full-workspace configuration screen: everything that
// previously required editing config.yaml or running CLI commands (relay
// endpoint, relay registration, interception addresses, storage path, TUI
// theme, desktop window title bar) is editable here. GetSettings re-reads
// config.yaml from disk on every mount so the form always reflects what is
// actually configured — including values set via `wiretap config init` or a
// text editor.
//
// The relay admin token is deliberately ephemeral: it is sent once with the
// register call and never persisted (OS-keychain storage is a planned
// follow-up), which mirrors the CLI's flag-only handling.
import { html } from "../vendor/preact/index.js";
import { useEffect, useState } from "../vendor/preact/index.js";
import { api } from "../lib/api.js";
import { Button, Input, Select, Field, Section } from "./ui.js";
import { Dropdown } from "./dropdown.js";
import { RelayAdmin, SettingsNav } from "./relay-admin.js";
import { DENSITIES, FONT_SCALES, loadDisplayPrefs, saveDisplayPrefs } from "../lib/prefs.js";

const SHELL_OPTIONS = [
  { value: "", label: "auto-detect ($SHELL)" },
  { value: "bash", label: "bash" },
  { value: "fish", label: "fish" },
  { value: "powershell", label: "powershell" },
  { value: "gitbash", label: "git bash" },
];

const THEME_OPTIONS = [
  { value: "dark", label: "dark" },
  { value: "light", label: "light" },
];

const TITLEBAR_OPTIONS = [
  { value: "auto", label: "auto (native bar)" },
  { value: "always", label: "always show" },
  { value: "never", label: "never (frameless)" },
];

function SettingsCard({ title, hint, children }) {
  return html`<section class="card p-4">
    <h3 class="section-label">${title}</h3>
    ${hint ? html`<p class="mt-1 text-xs text-neutral-500">${hint}</p>` : null}
    <div class="mt-3 space-y-3">${children}</div>
  </section>`;
}

export function Settings({ onToast, onSaved }) {
  const [section, setSection] = useState("desktop");
  const [view, setView] = useState(null); // last GetSettings payload
  const [form, setForm] = useState(null); // editable SettingsInput
  const [saving, setSaving] = useState(false);

  // Registration form (separate from the config form; nothing here persists
  // except through RegisterRelay's own save path).
  const [reg, setReg] = useState({ url: "", token: "", projects: "", name: "" });
  const [registering, setRegistering] = useState(false);
  const [projectDraft, setProjectDraft] = useState("");
  const [projectBusy, setProjectBusy] = useState(false);
  const [displayPrefs, setDisplayPrefs] = useState(loadDisplayPrefs());
  const updateDisplay = (key) => (event) => {
    const next = { ...displayPrefs, [key]: event.target.value };
    setDisplayPrefs(next);
    saveDisplayPrefs(next);
  };

  const load = async () => {
    try {
      const s = await api.getSettings();
      setView(s);
      setForm({
        relay_url: s.relay_url || "",
        forward_url: s.forward_url || "",
        store_path: s.store_path || "",
        tui_theme: s.tui_theme || "dark",
        native_titlebar: s.native_titlebar || "auto",
        proxy_addr: s.proxy_addr || "",
        local_api_addr: s.local_api_addr || "",
        shell: s.shell || "",
      });
      setReg((r) => ({ ...r, url: r.url || s.relay_url || "" }));
    } catch (e) {
      onToast("load settings: " + e);
    }
  };
  useEffect(() => {
    load();
  }, []);

  if (!view || !form) {
    return html`<div class="p-6 text-sm text-neutral-500">Loading settings…</div>`;
  }

  if (section === "relay") {
    return html`<div class="h-full overflow-y-auto">
      <div class="relay-admin-shell">
        <${SettingsNav} active=${section} onChange=${setSection} />
        <${RelayAdmin} defaultURL=${view.relay_url} localClientID=${view.client_id} onToast=${onToast} />
      </div>
    </div>`;
  }

  const set = (key) => (e) => setForm({ ...form, [key]: e.target.value });

  const save = async () => {
    setSaving(true);
    try {
      const s = await api.saveSettings(form);
      setView(s);
      onToast("Settings saved");
      onSaved && onSaved();
    } catch (e) {
      onToast("save settings: " + e, 5000);
    } finally {
      setSaving(false);
    }
  };

  const register = async () => {
    setRegistering(true);
    try {
      const result = await api.registerRelay({
        relay_url: reg.url,
        admin_token: reg.token,
        projects: reg.projects
          .split(",")
          .map((p) => p.trim())
          .filter(Boolean),
        display_name: reg.name,
      });
      setReg({ ...reg, token: "" }); // never keep the secret around
      onToast(`Registered as ${result.client_id}`);
      await load();
      onSaved && onSaved();
    } catch (e) {
      onToast("register: " + e, 6000);
    } finally {
      setRegistering(false);
    }
  };

  const addProject = async () => {
    const project = projectDraft.trim().replace(/^\/+|\/+$/g, "");
    if (!project) return;
    setProjectBusy(true);
    try {
      const s = await api.addRelayProject(project);
      setView(s);
      setProjectDraft("");
      onToast(`Added project ${project}`);
      onSaved && onSaved();
    } catch (e) {
      onToast("add project: " + e, 6000);
    } finally {
      setProjectBusy(false);
    }
  };

  const removeProject = async (project) => {
    if (!window.confirm(`Remove ${project}? This deletes its queued webhook history from the relay. Local deliveries remain available.`)) return;
    setProjectBusy(true);
    try {
      const s = await api.removeRelayProject(project);
      setView(s);
      onToast(`Removed project ${project}`);
      onSaved && onSaved();
    } catch (e) {
      onToast("remove project: " + e, 6000);
    } finally {
      setProjectBusy(false);
    }
  };

  return html`<div class="h-full overflow-y-auto">
    <div class="mx-auto flex max-w-2xl flex-col gap-4 p-4 pb-16">
      <${SettingsNav} active=${section} onChange=${setSection} />
      <${SettingsCard}
        title="Relay"
        hint="Tunnel endpoint for receiving public webhooks. Accepts the wss:// tunnel URL or the relay's https:// base URL."
      >
        <${Field} label="Relay tunnel URL">
          <${Input}
            class="font-mono"
            placeholder="wss://relay.example.com/tunnel (empty = disabled)"
            value=${form.relay_url}
            onInput=${set("relay_url")}
          />
        </>
        <${Field} label="Default forward URL">
          <${Input}
            class="font-mono"
            placeholder="http://127.0.0.1:8080/webhooks (empty = store only)"
            value=${form.forward_url}
            onInput=${set("forward_url")}
          />
        </>
        <p class="text-xs text-neutral-500">
          When set, every incoming webhook is automatically POSTed to this URL
          right after it is stored. Transforms with the on_replay trigger run
          first, just like a manual replay.
        </p>
        <div class="settings-connection-status">
          <span class="settings-status-line">
            <span class="live-dot ${view.tunnel_running ? "online" : ""} status-line-dot"></span>
            <span>tunnel ${view.tunnel_running ? "running" : "stopped"}</span>
            ${view.registered
              ? html`<span>· registered as</span>
                  <span class="font-mono text-neutral-300">${view.client_id}</span>
                  ${view.projects && view.projects.length
                    ? html`<span>· projects</span>
                        <span class="font-mono text-neutral-300">${view.projects.join(", ")}</span>`
                    : null}`
              : html`<span>· not registered</span>`}
          </span>
        </div>
      </>

      ${view.registered ? html`<${SettingsCard}
        title="Projects"
        hint="Add or remove webhook paths for this desktop. These changes keep the existing client ID and token."
      >
        <div class="space-y-2">
          ${(view.projects || []).map((project) => html`<div class="flex items-center justify-between gap-3 rounded-md border border-neutral-800 bg-neutral-950 px-3 py-2">
            <span class="font-mono text-sm text-neutral-200">${project}</span>
            <${Button} class="btn-xs" variant="danger" disabled=${projectBusy} onClick=${() => removeProject(project)}>Remove</>
          </div>`)}
          ${(view.projects || []).length === 0 ? html`<p class="text-xs text-neutral-500">No projects are currently assigned.</p>` : null}
        </div>
        <div class="flex gap-2">
          <${Input}
            class="font-mono"
            placeholder="new-project"
            value=${projectDraft}
            disabled=${projectBusy}
            onInput=${(e) => setProjectDraft(e.target.value)}
            onKeyDown=${(e) => e.key === "Enter" && addProject()}
          />
          <${Button} variant="primary" disabled=${projectBusy || !projectDraft.trim()} onClick=${addProject}>
            ${projectBusy ? "Updating…" : "Add project"}
          </>
        </div>
      </>` : null}

      <${SettingsCard} title="Display" hint="Adjust readability and row density for this desktop. These preferences are stored locally.">
        <div class="grid grid-cols-2 gap-3">
          <${Field} label="Text size">
            <${Dropdown} value=${displayPrefs.fontScale} onChange=${updateDisplay("fontScale")} options=${FONT_SCALES} aria-label="Text size" />
          </>
          <${Field} label="Row density">
            <${Dropdown} value=${displayPrefs.density} onChange=${updateDisplay("density")} options=${DENSITIES} aria-label="Row density" />
          </>
        </div>
      </>

      <${SettingsCard}
        title="Desktop window"
        hint="Controls the platform title bar and window buttons. Auto keeps the native bar. Never uses a frameless Linux window. Changes apply immediately."
      >
        <div class="grid grid-cols-2 gap-3">
          <${Field} label="Native title bar">
            <${Select}
              value=${form.native_titlebar}
              onChange=${set("native_titlebar")}
              options=${TITLEBAR_OPTIONS}
            />
          </>
        </div>
      </>

      ${!view.registered ? html`<${SettingsCard}
        title="Register this desktop"
        hint="One-time setup that creates this desktop's client ID and token. After registration, use the separate Projects section to add paths without rotating credentials."
      >
        <${Field} label="Relay URL">
          <${Input}
            class="font-mono"
            placeholder="https://relay.example.com"
            value=${reg.url}
            onInput=${(e) => setReg({ ...reg, url: e.target.value })}
          />
        </>
        <${Field} label="Admin token">
          <${Input}
            type="password"
            class="font-mono"
            placeholder="admin token (not saved)"
            value=${reg.token}
            onInput=${(e) => setReg({ ...reg, token: e.target.value })}
          />
        </>
        <div class="grid grid-cols-2 gap-3">
          <${Field} label="Initial projects (optional)">
            <${Input}
              class="font-mono"
              placeholder="project-a, project-b (or add later)"
              value=${reg.projects}
              onInput=${(e) => setReg({ ...reg, projects: e.target.value })}
            />
          </>
          <${Field} label="Display name (optional)">
            <${Input}
              placeholder="laptop"
              value=${reg.name}
              onInput=${(e) => setReg({ ...reg, name: e.target.value })}
            />
          </>
        </div>
        <div>
          <${Button} variant="primary" disabled=${registering} onClick=${register}>
            ${registering ? "Registering…" : "Register desktop"}
          </>
        </div>
      </>` : html`<${SettingsCard}
        title="Desktop registration"
        hint="Registration is complete. Project changes above preserve this identity; registering again is not used to add projects."
      >
        <p class="text-sm text-neutral-300">Registered as <span class="font-mono">${view.client_id}</span>.</p>
        <p class="text-xs text-neutral-600">Credentials live in <span class="font-mono">${view.creds_path}</span>.</p>
      </>`}

      <${SettingsCard}
        title="Interception"
        hint="Applies to the next 'wiretap intercept start'; running sessions keep their addresses."
      >
        <div class="grid grid-cols-2 gap-3">
          <${Field} label="Proxy address">
            <${Input}
              class="font-mono"
              placeholder="127.0.0.1:8888"
              value=${form.proxy_addr}
              onInput=${set("proxy_addr")}
            />
          </>
          <${Field} label="Local API address">
            <${Input}
              class="font-mono"
              placeholder="127.0.0.1:9876"
              value=${form.local_api_addr}
              onInput=${set("local_api_addr")}
            />
          </>
        </div>
        <${Field} label="Shell">
          <${Select}
            value=${form.shell}
            onChange=${set("shell")}
            options=${SHELL_OPTIONS}
          />
        </>
      </>

      <${SettingsCard}
        title="Storage"
        hint="Where captured traffic and webhooks are stored. Changing this takes effect after restarting wiretap."
      >
        <${Field} label="SQLite database path">
          <${Input}
            class="font-mono"
            placeholder=${view.store_default}
            value=${form.store_path}
            onInput=${set("store_path")}
          />
        </>
        <p class="text-xs text-neutral-600">
          Empty uses the default: <span class="font-mono">${view.store_default}</span>
        </p>
      </>

      <${SettingsCard} title="Terminal UI">
        <${Field} label="Theme">
          <${Select}
            value=${form.tui_theme}
            onChange=${set("tui_theme")}
            options=${THEME_OPTIONS}
          />
        </>
      </>

      <div class="flex items-center gap-3">
        <${Button} variant="primary" disabled=${saving} onClick=${save}>
          ${saving ? "Saving…" : "Save settings"}
        </>
        <span class="text-xs text-neutral-600 font-mono">${view.config_path}</span>
      </div>
    </div>
  </div>`;
}
