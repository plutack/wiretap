# Changelog

## v0.2.12 — 2026-09-11

### Keyring reliability hotfix

- Use Zalando's native keyring implementation exclusively under one `wiretap`
  service, with unique account names for relay-admin and client tokens.
- Fix repeated Secret Service collection creation on Linux and the resulting
  `secret not found` failure when reconnecting a saved relay.
- Verify every admin-token write can be read back before saving profile
  metadata, preventing unusable saved-relay entries.
- Clear the relay connection busy state on disconnect so Connect cannot remain
  stuck on **Connecting...** after an unnamed profile is saved.

## v0.2.11 — 2026-09-11

### Saved relay administration

- Added named relay-admin profiles for one-click reconnection from GUI Settings.
- Store admin tokens in the operating system credential store through secure
  native backends, including macOS Keychain, Windows Credential Manager,
  Secret Service, KWallet, and `pass`.
- Keep only the profile label, canonical relay URL, and last-used timestamp in
  Wiretap's local metadata. No plaintext or encrypted-file fallback is used.
- Preserve ephemeral relay administration when a keyring is unavailable or
  the user disables **Remember this relay**.
- Prefer a separate system-keyring entry for the desktop's long-lived relay
  client token, automatically migrate existing credentials, and fall back to
  the protected mode-`0600` file on headless systems without a usable keyring.
- Show the active client-token storage location as status information in Relay
  connection; there is no storage mode for users to configure.

## v0.2.10 — 2026-09-11

### Settings workflow

- Reorganized desktop settings into Relay connection, Capture and delivery,
  Interface, and Relay server workspaces with a persistent navigation rail.
- Added visible connection state, focused save actions, and unsaved-change
  feedback instead of one long configuration form.

### Relay project administration

- Added admin controls and API routes to assign a new project to any existing
  client and to delete a project independently of its client.
- Relay admin mutations refresh the server snapshot automatically. Changes
  affecting this desktop also synchronize its saved projects and reconnect its
  tunnel without rotating its identity.
- Project deletion explicitly warns that queued relay history is removed.

## v0.2.9 — 2026-09-11

### Relay server management

- Added a privileged **Relay server** workspace under GUI Settings with
  ephemeral admin-token authentication and live relay health.
- Added client inventory, one-time credential creation, client revocation, and
  project ownership reassignment without changing the current desktop identity.
- Added explicit warnings for client deletion and local-identity revocation.
  Project moves preserve queued webhook history.

### Portable transforms

- Added versioned JSON import and export for every transform trigger.
- Imported transforms open as unsaved drafts for review and test runs before
  they can be persisted.
- Added strict format, version, trigger, field, and 1 MiB size validation.
- Added **Duplicate** to create a disabled, unsaved copy while preserving the
  current program and sample request.

## v0.2.8 — 2026-09-10

### Composer layout

- Made the request and response panels stretch to the same height.
- Centered the empty response state across the full response panel instead of
  leaving an exposed block beneath it.

## v0.2.7 — 2026-09-10

### Compose recipes

- Added user-authored `on_compose` transforms that convert arbitrary source
  text, provider payloads, or log envelopes into complete request drafts.
- Added source file import and clipboard paste, selectable enabled recipes, and
  a configurable target base URL.
- Recipe application is side-effect free: generated method, URL, headers, and
  body return to the manual composer for review before delivery.
- Provider recipes remain local and user-managed. No provider-specific logic is
  hard-coded into Wiretap.

### Composer layout

- Separated manual request authoring from source-recipe preparation with a
  clear mode switch.
- Reworked request and response editing into focused Body and Headers tabs.
- Kept Compose visible while editing a transform from the sidebar.
- Added editable method, URL, headers, body, and response status inputs to the
  transform test bench for every trigger.
- Added explicit empty, validation, preparation, and error states while
  preserving the existing JSON import, captured-request handoff, replay
  transforms, and response inspection workflow.

## v0.2.6 — 2026-09-10

### Request composer

- Added a GUI Compose workspace for arbitrary HTTP requests with editable
  method, URL, multi-value headers, and text or JSON bodies.
- Added plain JSON and request-envelope file import.
- Existing webhooks and captured requests can now be opened as editable
  composer drafts.
- Composed requests can run enabled `on_replay` transforms and display response
  status, headers, content, size, duration, and truncation state.
- Restricted destinations to absolute HTTP(S) URLs, retained the existing
  30-second replay timeout, and bounded response previews to 2 MiB.

## v0.2.5 — 2026-09-10

### Project management

- Separated desktop registration from day-to-day project management.
- Added `wiretap relay projects add <path>` to claim another project using
  saved client credentials without creating a new client ID or token.
- Added `wiretap relay projects remove <path> --force` to release an owned
  project and update the local credentials file.
- Made `--projects` optional during `wiretap relay register`. Initial projects
  remain supported for backward compatibility and convenient first-time setup.
- Made repeated Add requests by the same owner idempotent, so retrying after a
  lost response does not fail or rotate credentials.

### Desktop GUI

- Added a dedicated Projects settings card with individual project rows and an
  Add project action.
- Added project removal with an explicit warning about relay-side history.
- Reframed registration as a one-time desktop identity operation and removed
  the misleading Re-register action from the normal project workflow.
- Restart the relay tunnel automatically after GUI project changes so the new
  subscription set takes effect immediately.

### Relay API and security

- Added `POST /client/projects` and `DELETE /client/projects/{project}`.
- Project mutations use the existing client ID/token over HTTP Basic auth;
  the relay admin token is not stored or required for ordinary project changes.
- A client can remove only a project it owns. Attempts by another client return
  not found without exposing ownership information.
- Removing a project deletes that project's queued relay-side webhook history
  through the existing SQLite foreign-key cascade. Already-downloaded desktop
  history is unaffected.

### Compatibility and limitations

- Existing credentials, relay databases, registrations, and projects continue
  to work without migration or re-registration.
- Upgrade the relay before using the new Add/Remove clients; older relays do
  not provide the new `/client/projects` endpoints.
- A project still has exactly one owning desktop client. Multiple independent
  subscribers and per-subscriber delivery cursors are not included.

### Verification

- Added API client, relay handler, storage, CLI, and GUI integration coverage
  for credential preservation, authorization, optional initial projects,
  project mutation, relay-history cleanup, and tunnel restart behavior.
