// Privileged relay server management. The admin token lives only in component
// memory and is supplied to each operation; it is never sent to SaveSettings.
import { html } from "../vendor/preact/index.js";
import { useEffect, useState } from "../vendor/preact/index.js";
import { api } from "../lib/api.js";
import { copyText } from "../lib/clipboard.js";
import { Button, Field, Input } from "./ui.js";
import { Dropdown } from "./dropdown.js";

function relativeTime(seconds) {
  if (!seconds) return "never connected";
  const elapsed = Math.max(0, Math.floor(Date.now() / 1000) - Number(seconds));
  if (elapsed < 60) return "just now";
  if (elapsed < 3600) return `${Math.floor(elapsed / 60)}m ago`;
  if (elapsed < 86400) return `${Math.floor(elapsed / 3600)}h ago`;
  return new Date(Number(seconds) * 1000).toLocaleDateString();
}

export function RelayAdmin({ defaultURL, localClientID, onToast, onChanged }) {
  const [connection, setConnection] = useState({ url: defaultURL || "", token: "" });
  const [overview, setOverview] = useState(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [createForm, setCreateForm] = useState({ name: "", projects: "" });
  const [credentials, setCredentials] = useState(null);
  const [projectTargets, setProjectTargets] = useState({});
  const [newProject, setNewProject] = useState({ path: "", clientID: "" });
  const [profiles, setProfiles] = useState([]);
  const [remember, setRemember] = useState(true);
  const [profileName, setProfileName] = useState("");

  useEffect(() => {
    if (!connection.url && defaultURL) setConnection((current) => ({ ...current, url: defaultURL }));
  }, [defaultURL]);

  const loadProfiles = async () => {
    try { setProfiles(await api.relayAdminProfiles()); }
    catch (e) { setError(`Load saved relays: ${String(e)}`); }
  };
  useEffect(() => { loadProfiles(); }, []);

  const session = () => ({ relay_url: connection.url, admin_token: connection.token });

  const inspect = async ({ quiet = false, credentials: candidate = connection, persist = true } = {}) => {
    setBusy("inspect");
    setError("");
    try {
      const result = await api.relayAdminOverview({ relay_url: candidate.url, admin_token: candidate.token });
      setConnection(candidate);
      setOverview(result);
      setProjectTargets(Object.fromEntries((result.projects || []).map((project) => [project.path, project.client_id])));
      if (!quiet) onToast(`Connected to relay ${result.version || "server"}`);
      if (persist && remember) {
        try {
          await api.relayAdminSaveProfile({ relay_url: candidate.url, admin_token: candidate.token, name: profileName });
          await loadProfiles();
        } catch (saveError) {
          setError(`Connected, but the relay could not be saved: ${String(saveError)}`);
        }
      }
      setNewProject((current) => ({ ...current, clientID: current.clientID || result.clients?.[0]?.client_id || "" }));
      return true;
    } catch (e) {
      setOverview(null);
      setError(String(e));
      return false;
    } finally {
      setBusy("");
    }
  };

  const connectProfile = async (profile) => {
    setBusy(`profile:${profile.id}`); setError("");
    try {
      const saved = await api.relayAdminLoadProfile(profile.id);
      await inspect({ credentials: { url: saved.relay_url, token: saved.admin_token }, persist: false });
      await loadProfiles();
    } catch (e) { setError(String(e)); setBusy(""); }
  };

  const forgetProfile = async (profile) => {
    if (!window.confirm(`Forget ${profile.name}? Its admin token will be deleted from the system keyring.`)) return;
    setBusy(`forget:${profile.id}`); setError("");
    try { await api.relayAdminDeleteProfile(profile.id); await loadProfiles(); onToast(`Forgot relay ${profile.name}`); }
    catch (e) { setError(String(e)); }
    finally { setBusy(""); }
  };

  const disconnect = () => {
	setBusy("");
    setConnection((current) => ({ ...current, token: "" }));
    setOverview(null);
    setCredentials(null);
    setError("");
  };

  const createClient = async () => {
    setBusy("create");
    setError("");
    try {
      const result = await api.relayAdminCreateClient({
        ...session(),
        display_name: createForm.name,
        projects: createForm.projects.split(",").map((item) => item.trim()).filter(Boolean),
      });
      setCredentials(result);
      setCreateForm({ name: "", projects: "" });
      onToast(`Created client ${result.client_id}`);
      await inspect({ quiet: true });
      onChanged && onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  };

  const copyCredentials = async () => {
    try {
      await copyText(JSON.stringify(credentials, null, 2));
      onToast("Client credentials copied");
    } catch (e) {
      setError(`Copy credentials: ${String(e)}`);
    }
  };

  const revokeClient = async (client) => {
    const localWarning = client.client_id === localClientID
      ? " This is the identity used by this desktop, so its tunnel will stop authenticating."
      : "";
    const projectCount = (client.projects || []).length;
    if (!window.confirm(`Revoke ${client.display_name || client.client_id}? This deletes ${projectCount} project binding${projectCount === 1 ? "" : "s"} and their queued relay history.${localWarning}`)) return;
    setBusy(`delete:${client.client_id}`);
    setError("");
    try {
      await api.relayAdminDeleteClient({ ...session(), client_id: client.client_id });
      onToast(`Revoked client ${client.client_id}`);
      await inspect({ quiet: true });
      onChanged && onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  };

  const moveProject = async (project) => {
    const target = projectTargets[project.path];
    if (!target || target === project.client_id) return;
    const targetClient = (overview.clients || []).find((client) => client.client_id === target);
    if (!window.confirm(`Move ${project.path} to ${targetClient?.display_name || target}? Queued webhook history stays with the project.`)) return;
    setBusy(`move:${project.path}`);
    setError("");
    try {
      await api.relayAdminReassignProject({ ...session(), path: project.path, new_client_id: target, force: true });
      onToast(`Moved project ${project.path}`);
      await inspect({ quiet: true });
      onChanged && onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  };

  const addProject = async () => {
    const path = newProject.path.trim().replace(/^\/+|\/+$/g, "");
    if (!path || !newProject.clientID) return;
    setBusy("add-project");
    setError("");
    try {
      await api.relayAdminAddProject({ ...session(), path, client_id: newProject.clientID });
      setNewProject((current) => ({ ...current, path: "" }));
      onToast(`Added project ${path}`);
      await inspect({ quiet: true });
      onChanged && onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  };

  const deleteProject = async (project) => {
    if (!window.confirm(`Delete ${project.path}? This permanently deletes its queued relay history. Local deliveries remain available.`)) return;
    setBusy(`delete-project:${project.path}`);
    setError("");
    try {
      await api.relayAdminDeleteProject({ ...session(), path: project.path });
      onToast(`Deleted project ${project.path}`);
      await inspect({ quiet: true });
      onChanged && onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  };

  const clientOptions = (overview?.clients || []).map((client) => ({
    value: client.client_id,
    label: client.display_name ? `${client.display_name} (${client.client_id})` : client.client_id,
  }));

  return html`<div class="relay-admin-console">
    <header class="relay-admin-heading">
      <div>
        <h2>Relay server</h2>
        <p>Manage identities and project ownership on a relay you operate.</p>
      </div>
      ${overview ? html`<${Button} onClick=${disconnect}>Disconnect</>` : null}
    </header>

    ${!overview && profiles.length ? html`<section class="relay-profile-panel" aria-label="Saved relays">
      <div class="relay-admin-panel-head"><div><h3>Saved relays <span>${profiles.length}</span></h3><p>Admin tokens are retrieved from your system keyring only when you connect.</p></div></div>
      <div class="relay-profile-list">${profiles.map((profile) => html`<article class="relay-profile-row" key=${profile.id}>
        <div><strong>${profile.name}</strong><code>${profile.relay_url}</code></div>
        <span>${relativeTime(profile.last_used)}</span>
        <div><${Button} class="btn-xs" variant="primary" disabled=${Boolean(busy)} onClick=${() => connectProfile(profile)}>${busy === `profile:${profile.id}` ? "Connecting..." : "Connect"}</>
        <${Button} class="btn-xs" variant="danger" disabled=${Boolean(busy)} onClick=${() => forgetProfile(profile)}>Forget</></div>
      </article>`)}</div>
    </section>` : null}

    <section class="relay-admin-connect" aria-label="Relay admin connection">
      <${Field} label="Relay URL">
        <${Input} class="font-mono" placeholder="https://relay.example.com" value=${connection.url}
          disabled=${Boolean(overview)} onInput=${(event) => setConnection({ ...connection, url: event.target.value })} />
      </>
      <${Field} label="Admin token">
        <${Input} type="password" class="font-mono" placeholder="Enter the server admin token" value=${connection.token}
          disabled=${Boolean(overview)} onInput=${(event) => setConnection({ ...connection, token: event.target.value })}
          onKeyDown=${(event) => event.key === "Enter" && inspect()} />
      </>
      ${!overview ? html`<${Button} variant="primary" disabled=${busy === "inspect" || !connection.url.trim() || !connection.token.trim()} onClick=${() => inspect()}>
        ${busy === "inspect" ? "Connecting..." : "Connect"}
      </>` : null}
    </section>
    ${!overview ? html`<div class="relay-admin-save-options">
      <label><input type="checkbox" checked=${remember} onChange=${(event) => setRemember(event.target.checked)} /><span>Remember this relay</span></label>
      ${remember ? html`<${Input} placeholder="Relay name (optional)" value=${profileName} onInput=${(event) => setProfileName(event.target.value)} />` : null}
      <p>${remember ? "The admin token is stored in your system keyring. Only the relay name and URL are written to Wiretap settings." : "This connection is temporary and will be cleared when you disconnect."}</p>
    </div>` : null}

    ${error ? html`<div class="relay-admin-error" role="alert"><strong>Request failed</strong><span>${error}</span></div>` : null}

    ${!overview ? html`<div class="relay-admin-empty">
      <strong>No server session</strong>
      <p>Enter a relay URL and token, or reconnect to a relay saved in your system keyring.</p>
    </div>` : html`
      <section class="relay-admin-summary" aria-label="Relay health">
        <div class="relay-health"><span class="live-dot online"></span><strong>${overview.status || "online"}</strong><small>${overview.base_url}</small></div>
        <dl>
          <div><dt>Version</dt><dd>${overview.version || "unknown"}</dd></div>
          <div><dt>Live tunnels</dt><dd>${overview.tunnel_count || 0}</dd></div>
          <div><dt>Clients</dt><dd>${overview.clients?.length || 0}</dd></div>
          <div><dt>Projects</dt><dd>${overview.projects?.length || 0}</dd></div>
        </dl>
        <${Button} class="btn-xs" disabled=${busy === "inspect"} onClick=${() => inspect()}>Refresh</>
      </section>

      ${credentials ? html`<section class="relay-credentials" aria-live="polite">
        <div><strong>Save these credentials now</strong><p>The client token cannot be retrieved again from the relay.</p></div>
        <pre>${JSON.stringify(credentials, null, 2)}</pre>
        <div class="relay-credential-actions">
          <${Button} variant="primary" onClick=${copyCredentials}>Copy JSON</>
          <${Button} onClick=${() => setCredentials(null)}>Dismiss</>
        </div>
      </section>` : null}

      <div class="relay-admin-grid">
        <section class="relay-admin-panel">
          <div class="relay-admin-panel-head"><div><h3>Clients <span>${overview.clients?.length || 0}</span></h3><p>Registered identities and their last relay contact.</p></div></div>
          <div class="relay-client-list">
            ${(overview.clients || []).map((client) => html`<article class="relay-client-row" key=${client.client_id}>
              <div class="relay-client-main">
                <strong>${client.display_name || "Unnamed client"}</strong>
                <code>${client.client_id}</code>
                <span>${relativeTime(client.last_seen_at)}</span>
              </div>
              <div class="relay-client-projects">${(client.projects || []).length ? client.projects.join(", ") : "No projects"}</div>
              <${Button} class="btn-xs" variant="danger" disabled=${Boolean(busy)} onClick=${() => revokeClient(client)}>
                ${busy === `delete:${client.client_id}` ? "Revoking..." : "Revoke"}
              </>
            </article>`)}
            ${(overview.clients || []).length === 0 ? html`<div class="relay-list-empty">No clients registered.</div>` : null}
          </div>
        </section>

        <section class="relay-admin-panel relay-create-client">
          <div class="relay-admin-panel-head"><div><h3>Create client</h3><p>Generate credentials without changing this desktop.</p></div></div>
          <${Field} label="Display name">
            <${Input} placeholder="CI runner" value=${createForm.name} onInput=${(event) => setCreateForm({ ...createForm, name: event.target.value })} />
          </>
          <${Field} label="Initial projects (optional)">
            <${Input} class="font-mono" placeholder="orders, billing" value=${createForm.projects} onInput=${(event) => setCreateForm({ ...createForm, projects: event.target.value })} />
          </>
          <p class="relay-admin-note">Projects can also be assigned after the client is created.</p>
          <${Button} variant="primary" disabled=${Boolean(busy)} onClick=${createClient}>
            ${busy === "create" ? "Creating..." : "Create client"}
          </>
        </section>
      </div>

      <section class="relay-admin-panel relay-project-panel">
        <div class="relay-admin-panel-head"><div><h3>Project ownership <span>${overview.projects?.length || 0}</span></h3><p>Add paths, change owners, or remove paths. This view refreshes after every change.</p></div></div>
        <div class="relay-project-create">
          <${Field} label="Project path">
            <${Input} class="font-mono" placeholder="orders" value=${newProject.path}
              disabled=${Boolean(busy)} onInput=${(event) => setNewProject({ ...newProject, path: event.target.value })}
              onKeyDown=${(event) => event.key === "Enter" && addProject()} />
          </>
          <${Field} label="Owner">
            <${Dropdown} aria-label="New project owner" value=${newProject.clientID} options=${clientOptions}
              disabled=${Boolean(busy)} onChange=${(event) => setNewProject({ ...newProject, clientID: event.target.value })} />
          </>
          <${Button} variant="primary" disabled=${Boolean(busy) || !newProject.path.trim() || !newProject.clientID} onClick=${addProject}>
            ${busy === "add-project" ? "Adding..." : "Add project"}
          </>
        </div>
        <p class="relay-project-sync-note">If this desktop gains or loses a project, its saved project list and tunnel update automatically.</p>
        <div class="relay-project-list">
          ${(overview.projects || []).map((project) => html`<article class="relay-project-row" key=${project.path}>
            <div class="relay-project-name"><strong>${project.path}</strong><span>acknowledged through #${project.acked_seq || 0}</span></div>
            <${Dropdown} aria-label=${`Owner for ${project.path}`} value=${projectTargets[project.path] || project.client_id}
              options=${clientOptions} onChange=${(event) => setProjectTargets({ ...projectTargets, [project.path]: event.target.value })} />
            <div class="relay-project-actions">
              <${Button} class="btn-xs" disabled=${Boolean(busy) || !projectTargets[project.path] || projectTargets[project.path] === project.client_id}
                onClick=${() => moveProject(project)}>${busy === `move:${project.path}` ? "Moving..." : "Move"}</>
              <${Button} class="btn-xs" variant="danger" disabled=${Boolean(busy)} onClick=${() => deleteProject(project)}>
                ${busy === `delete-project:${project.path}` ? "Deleting..." : "Delete"}
              </>
            </div>
          </article>`)}
          ${(overview.projects || []).length === 0 ? html`<div class="relay-list-empty">No project paths are claimed.</div>` : null}
        </div>
      </section>
    `}
  </div>`;
}
