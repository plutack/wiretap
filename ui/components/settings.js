// Settings renders the full-workspace configuration screen: everything that
// previously required editing config.yaml or running CLI commands (relay
// endpoint, relay registration, interception addresses, storage path, TUI
// theme) is editable here. GetSettings re-reads config.yaml from disk on
// every mount so the form always reflects what is actually configured —
// including values set via `wiretap config init` or a text editor.
//
// The relay admin token is deliberately ephemeral: it is sent once with the
// register call and never persisted (OS-keychain storage is a planned
// follow-up), which mirrors the CLI's flag-only handling.
import { html } from "../vendor/preact/index.js";
import { useEffect, useState } from "../vendor/preact/index.js";
import { api } from "../lib/api.js";
import { Button, Input, Select, Field, Section } from "./ui.js";
import { Dropdown } from "./dropdown.js";
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

function SettingsCard({ title, hint, children }) {
  return html`<section class="card p-4">
    <h3 class="section-label">${title}</h3>
    ${hint ? html`<p class="mt-1 text-xs text-neutral-500">${hint}</p>` : null}
    <div class="mt-3 space-y-3">${children}</div>
  </section>`;
}

export function Settings({ onToast, onSaved }) {
  const [view, setView] = useState(null); // last GetSettings payload
  const [form, setForm] = useState(null); // editable SettingsInput
  const [saving, setSaving] = useState(false);

  // Registration form (separate from the config form; nothing here persists
  // except through RegisterRelay's own save path).
  const [reg, setReg] = useState({ url: "", token: "", projects: "", name: "" });
  const [registering, setRegistering] = useState(false);
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

  return html`<div class="h-full overflow-y-auto">
    <div class="mx-auto flex max-w-2xl flex-col gap-4 p-4 pb-16">
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
          right after it is stored — transforms with the on_replay trigger run
          first, same as a manual replay.
        </p>
        <div class="settings-connection-status">
          <div class="settings-status-line">
            <span class="live-dot ${view.tunnel_running ? "online" : ""} status-line-dot"></span>
            <span>tunnel ${view.tunnel_running ? "running" : "stopped"}</span>
          </div>
          ${view.registered
            ? html`<div class="settings-status-line settings-status-detail">
                <span>registered as</span>
                <span class="font-mono text-neutral-300">${view.client_id}</span>
              </div>
              ${view.projects && view.projects.length
                ? html`<div class="settings-status-line settings-status-detail">
                    <span>projects</span>
                    <span class="font-mono text-neutral-300">${view.projects.join(", ")}</span>
                  </div>`
                : null}`
            : html`<div class="settings-status-line settings-status-detail">not registered</div>`}
        </div>
      </>

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
        title="Relay registration"
        hint="Registers this PC with your relay and claims project paths (same as 'wiretap relay register --save'). The admin token is used once and never stored."
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
          <${Field} label="Project paths (comma-separated)">
            <${Input}
              class="font-mono"
              placeholder="project-a, project-b"
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
            ${registering ? "Registering…" : view.registered ? "Re-register" : "Register"}
          </>
        </div>
        ${view.registered
          ? html`<p class="text-xs text-neutral-600">
              Credentials live in <span class="font-mono">${view.creds_path}</span>.
              Re-registering issues a fresh client id + token.
            </p>`
          : null}
      </>

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
