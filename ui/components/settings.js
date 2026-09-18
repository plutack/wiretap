import { html } from "../vendor/preact/index.js";
import { useEffect, useRef, useState } from "../vendor/preact/index.js";
import { api } from "../lib/api.js";
import { Button, Input, Select, Field } from "./ui.js";
import { Dropdown } from "./dropdown.js";
import { RelayAdmin } from "./relay-admin.js";
import {
  DENSITIES,
  FONT_SCALES,
  THEMES,
  loadDisplayPrefs,
  saveDisplayPrefs,
} from "../lib/prefs.js";

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
const NAV_ITEMS = [
  {
    id: "connection",
    label: "Relay connection",
    hint: "Endpoint, identity, projects",
  },
  {
    id: "capture",
    label: "Capture and delivery",
    hint: "Forwarding, proxy, storage",
  },
  { id: "interface", label: "Interface", hint: "Readability and window" },
  {
    id: "relay",
    label: "Relay server",
    hint: "Clients, subscriptions, and history",
    server: true,
  },
];

function SettingsNav({ active, onChange }) {
  return html`<nav class="settings-rail" aria-label="Settings sections">
    <div class="settings-rail-label">This desktop</div>
    ${NAV_ITEMS.filter((item) => !item.server).map(
      (item) =>
        html`<button
          class=${active === item.id ? "active" : ""}
          onClick=${() => onChange(item.id)}
        >
          <strong>${item.label}</strong><span>${item.hint}</span>
        </button>`,
    )}
    <div class="settings-rail-label settings-rail-server">
      Server administration
    </div>
    ${NAV_ITEMS.filter((item) => item.server).map(
      (item) =>
        html`<button
          class=${active === item.id ? "active" : ""}
          onClick=${() => onChange(item.id)}
        >
          <strong>${item.label}</strong><span>${item.hint}</span>
        </button>`,
    )}
  </nav>`;
}
function SettingsCard({ title, hint, children }) {
  return html`<section class="settings-card">
    <header>
      <h3>${title}</h3>
      ${hint ? html`<p>${hint}</p>` : null}
    </header>
    <div class="settings-card-body">${children}</div>
  </section>`;
}
function PageHeader({ title, hint }) {
  return html`<header class="settings-page-head">
    <h2>${title}</h2>
    <p>${hint}</p>
  </header>`;
}

function ThemePicker({ value, onChange }) {
  return html`<div
    class="theme-picker"
    role="radiogroup"
    aria-label="Application colour theme"
  >
    ${THEMES.map(
      (theme) =>
        html`<button
          type="button"
          role="radio"
          aria-checked=${value === theme.value}
          class="theme-choice ${value === theme.value ? "selected" : ""}"
          onClick=${() => onChange(theme.value)}
        >
          <span class="theme-swatch" aria-hidden="true">
            ${theme.colors.map((color) => html`<i style=${{ background: color }}></i>`)}
          </span>
          <span class="theme-choice-copy"
            ><strong>${theme.label}</strong
            ><small>${theme.description}</small></span
          >
          <span class="theme-choice-check" aria-hidden="true"
            >${value === theme.value ? "✓" : ""}</span
          >
        </button>`,
    )}
  </div>`;
}

export function Settings({ onToast, onSaved }) {
  const [section, setSection] = useState("connection");
  const [view, setView] = useState(null);
  const [form, setForm] = useState(null);
  const [savedForm, setSavedForm] = useState(null);
  const [saving, setSaving] = useState(false);
  const [reg, setReg] = useState({
    url: "",
    token: "",
    projects: "",
    name: "",
  });
  const [registering, setRegistering] = useState(false);
  const [projectDraft, setProjectDraft] = useState("");
  const [projectBusy, setProjectBusy] = useState(false);
  const [displayPrefs, setDisplayPrefs] = useState(loadDisplayPrefs());
  const [importing, setImporting] = useState(false);
  const importInput = useRef(null);

  const load = async () => {
    try {
      const s = await api.getSettings();
      const next = {
        relay_url: s.relay_url || "",
        forward_url: s.forward_url || "",
        store_path: s.store_path || "",
        tui_theme: s.tui_theme || "dark",
        native_titlebar: s.native_titlebar || "auto",
        proxy_addr: s.proxy_addr || "",
        local_api_addr: s.local_api_addr || "",
        shell: s.shell || "",
      };
      setView(s);
      setForm(next);
      setSavedForm(next);
      setReg((current) => ({
        ...current,
        url: current.url || s.relay_url || "",
      }));
    } catch (e) {
      onToast("load settings: " + e);
    }
  };
  useEffect(() => {
    load();
  }, []);
  if (!view || !form)
    return html`<div class="p-6 text-sm text-neutral-500">
      Loading settings...
    </div>`;

  const refreshView = async () => {
    try {
      setView(await api.getSettings());
      onSaved && onSaved();
    } catch (e) {
      onToast("refresh settings: " + e);
    }
  };

  const set = (key) => (event) => setForm((current) => ({ ...current, [key]: event.target.value }));
  const dirty = JSON.stringify(form) !== JSON.stringify(savedForm);
  const save = async () => {
    setSaving(true);
    try {
      const s = await api.saveSettings(form);
      setView(s);
      setSavedForm({ ...form });
      onToast("Settings saved");
      onSaved && onSaved();
    } catch (e) {
      onToast("save settings: " + e, 5000);
    } finally {
      setSaving(false);
    }
  };
  const updateDisplay = (key) => (event) => {
    const next = { ...displayPrefs, [key]: event.target.value };
    setDisplayPrefs(next);
    saveDisplayPrefs(next);
  };
  const updateTheme = (theme) => {
    const next = { ...displayPrefs, theme };
    setDisplayPrefs(next);
    saveDisplayPrefs(next);
  };
  const register = async () => {
    setRegistering(true);
    try {
      const result = await api.registerRelay({
        relay_url: reg.url,
        admin_token: reg.token,
        projects: reg.projects
          .split(",")
          .map((item) => item.trim())
          .filter(Boolean),
        display_name: reg.name,
      });
      setReg((current) => ({ ...current, token: "" }));
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
    if (
      !window.confirm(
        `Unsubscribe this desktop from ${project}? The project and relay history remain available to other subscribers.`,
      )
    )
      return;
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
  const importRelayClient = async (event) => {
    const input = event.target;
    const file = input.files?.[0];
    if (!file) return;
    const replacing = Boolean(view.registered);
    if (
      replacing &&
      !window.confirm(
        `Replace relay client ${view.client_id}? Its current credentials will no longer be used by this desktop.`,
      )
    ) {
      input.value = "";
      return;
    }
    setImporting(true);
    try {
      const contents = await file.text();
      const imported = await api.importRelayClientFile(contents, replacing);
      await load();
      onToast(`Imported relay client ${imported.client_id}; tunnel is reconnecting.`);
      onSaved && onSaved();
    } catch (e) {
      onToast("import credentials: " + e, 6000);
    } finally {
      setImporting(false);
      input.value = "";
    }
  };

  return html`<div class="settings-workspace">
    <${SettingsNav} active=${section} onChange=${setSection} />
    <main class="settings-content">
      ${
        section === "relay"
          ? html`<${RelayAdmin}
            defaultURL=${view.relay_url}
            localClientID=${view.client_id}
            onToast=${onToast}
            onChanged=${refreshView}
          />`
          : null
      }
      ${
        section === "connection"
          ? html`
        <${PageHeader} title="Relay connection" hint="Connect this client to manage its webhook paths with credentials." />
        <div class="settings-status-card"><span class="live-dot ${view.tunnel_running ? "online" : ""}"></span><div>
          <strong>${view.tunnel_running ? "Tunnel connected" : "Tunnel stopped"}</strong>
          <p>${view.registered ? html`Registered as <code>${view.client_id}</code>` : "This desktop has not been registered."}</p>
        </div></div>
        <${SettingsCard} title="Import client credentials" hint="Use an exported json file by a relay administrator.">
          <div class="settings-inline-action">
            <div><strong>${view.registered ? "Replace this desktop's relay identity" : "Connect this desktop without an admin token"}</strong><p class="settings-path-note">Client token is saved securely to the system keyring if available.</p></div>
            <${Button} variant="primary" disabled=${importing} onClick=${() => importInput.current?.click()}>${importing ? "Importing..." : "Choose file"}</>
          </div>
          <input ref=${importInput} type="file" accept="application/json,.json" hidden onChange=${importRelayClient} />
        </>
        <${SettingsCard} title="Relay endpoint" hint="Accepts the public HTTPS base URL or its WSS tunnel URL.">
          <${Field} label="Relay tunnel URL"><${Input} class="font-mono" placeholder="wss://relay.example.com/tunnel" value=${form.relay_url} onInput=${set("relay_url")} /></>
        </>
        ${
          !view.registered
            ? html`<${SettingsCard} title="Register this desktop" hint="Creates one identity. Initial projects are optional and can be added later without registering again.">
          <${Field} label="Relay URL"><${Input} class="font-mono" placeholder="https://relay.example.com" value=${reg.url} onInput=${(e) => setReg({ ...reg, url: e.target.value })} /></>
          <${Field} label="Admin token"><${Input} type="password" class="font-mono" placeholder="Not saved" value=${reg.token} onInput=${(e) => setReg({ ...reg, token: e.target.value })} /></>
          <div class="settings-field-grid"><${Field} label="Initial projects (optional)"><${Input} class="font-mono" placeholder="orders, billing" value=${reg.projects} onInput=${(e) => setReg({ ...reg, projects: e.target.value })} /></>
          <${Field} label="Display name (optional)"><${Input} placeholder="Development laptop" value=${reg.name} onInput=${(e) => setReg({ ...reg, name: e.target.value })} /></></div>
          <${Button} variant="primary" disabled=${registering || !reg.url.trim() || !reg.token.trim()} onClick=${register}>${registering ? "Registering..." : "Register desktop"}</>
        </>`
            : html`<${SettingsCard} title="Projects for this desktop" hint="Changes preserve this client ID and automatically reconnect the tunnel.">
          <div class="settings-project-list">${(view.projects || []).map((project) => html`<div><code>${project}</code><${Button} class="btn-xs" variant="danger" disabled=${projectBusy} onClick=${() => removeProject(project)}>Remove</></div>`)}
          ${(view.projects || []).length === 0 ? html`<p>No projects assigned yet.</p>` : null}</div>
          <div class="settings-inline-action"><${Input} class="font-mono" placeholder="new-project" value=${projectDraft} disabled=${projectBusy} onInput=${(e) => setProjectDraft(e.target.value)} onKeyDown=${(e) => e.key === "Enter" && addProject()} />
          <${Button} variant="primary" disabled=${projectBusy || !projectDraft.trim()} onClick=${addProject}>${projectBusy ? "Updating..." : "Add project"}</></div>
          <div class="credential-storage-note ${view.token_storage === "keyring" ? "secure" : "fallback"}">
            <span>${view.token_storage === "keyring" ? "Keyring" : "Protected file"}</span>
            <p>${
              view.token_storage === "keyring"
                ? "The client token is in your system keyring; this file contains only the client ID and project metadata."
                : "No supported keyring was available, so the client token remains in this mode-0600 file."
            }</p>
            <code>${view.creds_path}</code>
          </div>
          ${view.token_warning ? html`<p class="settings-credential-warning">${view.token_warning}</p>` : null}
        </>`
        }
      `
          : null
      }
      ${
        section === "capture"
          ? html`
        <${PageHeader} title="Capture and delivery" hint="Choose where requests go, how local traffic is intercepted, and where history is stored." />
        <${SettingsCard} title="Automatic delivery" hint="Incoming webhooks are stored first, then sent here with enabled on_replay transforms.">
          <${Field} label="Default forward URL"><${Input} class="font-mono" placeholder="http://127.0.0.1:8080/webhooks" value=${form.forward_url} onInput=${set("forward_url")} /></>
        </>
        <${SettingsCard} title="Local interception" hint="Applies the next time an interception session starts.">
          <div class="settings-field-grid"><${Field} label="Proxy address"><${Input} class="font-mono" placeholder="127.0.0.1:8888" value=${form.proxy_addr} onInput=${set("proxy_addr")} /></>
          <${Field} label="Local API address"><${Input} class="font-mono" placeholder="127.0.0.1:9876" value=${form.local_api_addr} onInput=${set("local_api_addr")} /></></div>
          <${Field} label="Shell"><${Select} value=${form.shell} onChange=${set("shell")} options=${SHELL_OPTIONS} /></>
        </>
        <${SettingsCard} title="Storage" hint="A custom database path takes effect after restarting wiretap.">
          <${Field} label="SQLite database path"><${Input} class="font-mono" placeholder=${view.store_default} value=${form.store_path} onInput=${set("store_path")} /></>
          <p class="settings-path-note">Default: <code>${view.store_default}</code></p>
        </>
      `
          : null
      }
      ${
        section === "interface"
          ? html`
        <${PageHeader} title="Interface" hint="Tune the desktop and terminal views without changing capture behavior." />
        <${SettingsCard} title="Application theme" hint="Palette presets recolour the complete workbench, payload syntax, and transform editor.">
          <${ThemePicker} value=${displayPrefs.theme} onChange=${updateTheme} />
        </>
        <${SettingsCard} title="Desktop readability" hint="These preferences apply immediately and are stored on this desktop.">
          <div class="settings-field-grid"><${Field} label="Text size"><${Dropdown} value=${displayPrefs.fontScale} onChange=${updateDisplay("fontScale")} options=${FONT_SCALES} aria-label="Text size" /></>
          <${Field} label="Row density"><${Dropdown} value=${displayPrefs.density} onChange=${updateDisplay("density")} options=${DENSITIES} aria-label="Row density" /></></div>
        </>
        <${SettingsCard} title="Desktop window" hint="Controls the platform title bar and window buttons.">
          <${Field} label="Native title bar"><${Select} value=${form.native_titlebar} onChange=${set("native_titlebar")} options=${TITLEBAR_OPTIONS} /></>
        </>
        <${SettingsCard} title="Terminal UI"><${Field} label="Theme"><${Select} value=${form.tui_theme} onChange=${set("tui_theme")} options=${THEME_OPTIONS} /></></>
      `
          : null
      }
      ${
        section !== "relay"
          ? html`<div class="settings-savebar"><div><strong>${dirty ? "Unsaved changes" : "All changes saved"}</strong><span>${view.config_path}</span></div>
        <${Button} variant="primary" disabled=${saving || !dirty} onClick=${save}>${saving ? "Saving..." : "Save changes"}</></div>`
          : null
      }
    </main>
  </div>`;
}
