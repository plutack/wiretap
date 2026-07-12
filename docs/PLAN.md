# wiretap — Project Plan

> Capture HTTP traffic and webhooks locally, replay them, and receive inbound
> webhooks from the internet via a self-hosted relay on your VPS — all from one
> CLI/GUI/TUI app. Linux first; Windows + macOS behind build tags later.

---

## 1. Goals & non-goals

### Goals (MVP)

1. Intercept outbound HTTP/HTTPS traffic from a spawned shell via an MITM proxy
   (httptoolkit-style env injection), with `stop_interception` for **every** shell.
2. Capture inbound webhooks over the internet using a self-hosted relay (`relayd`)
   on your VPS, with store-and-forward so the local PC never misses a webhook while
   offline.
3. Persist captures in SQLite on both PC and relay; replay any captured webhook.
4. Clean Wails GUI dashboard with two tab modes: **Traffic** and **Webhooks**.
5. Bubbletea TUI behind `wiretap tui`; one-shot CLI via Cobra.
6. Every relay administration capability exposed as **both HTTP routes and CLI
   subcommands** (one API contract, two frontends).
7. Linux-first, cross-platform-ready via build-tag-split seams.
8. Code written to be **testable by default** — this is a Go-learning project, so
   tests are a first-class deliverable, not an afterthought.

### Non-goals (MVP)

- Multi-tenant relay (single owner, single `admin_token`; the schema can grow later).
- Fuzz-testing, formal verification, performance benchmarking at scale.
- Non-HTTPS traffic interception (plain HTTP on port 80) — covered later.
- Mobile clients.
- Authenticated webhook forwarding to multiple downstreams per project (one tunnel
  per client is enough for MVP).

---

## 2. Architecture overview

```mermaid
flowchart TD
    subgraph PC["wiretap on your PC (behind NAT, dynamic IP)"]
        CLI[Cobra CLI]
        TUI[Bubbletea TUI]
        GUI[Wails GUI]
        CORE[internal core packages]
        DB[(SQLite wiretap.db)]
    end

    subgraph VPS["relayd on your VPS (static IP, your domain)"]
        HTTP[HTTP server: admin API + webhook ingress]
            WS[WebSocket tunnel server]
            RDB[(SQLite wiretap-relay.db)]
    end

    SEND[3rd-party webhook senders] -->|POST https://relay.domain.com/project-a| HTTP
    HTTP --> WS
    WS <-->|wss outbound dial from PC| TUN[relay client]
    TUN --> CORE --> DB
    CLI --> CORE
    TUI --> CORE
    GUI --> CORE
    CLI -->|HTTP to relayd admin API| HTTP
```

**Key invariant:** the PC always dials **out** to the VPS (WebSocket over TLS),
so NAT / CGNAT / dynamic home IP never matter. The relay stores webhooks in its
own SQLite and pushes them down the tunnel; the PC acks per-project cursors.

---

## 3. Repository layout

```
wiretap/
  .go-version
  go.mod                                  # module github.com/plutack/wiretap
  LICENSE
  README.md
  docs/
    PLAN.md                               # this file
  cmd/
    wiretap/                              # local app (CLI + TUI + GUI host)
      main.go                            # cobra root; dispatches to subcommands
    wiretap-relay/                       # standalone relay server binary for the VPS (package main; binary name `wiretap-relay`)
      main.go
  internal/
    app/                                 # wires deps together for the local app
    config/                              # Viper config loading, paths, defaults
    api/                                 # shared request/response types (HTTP contract)
      client.go                          # typed HTTP client used by CLI
      server.go                          # handler constructors (used by relayd)
      types.go                           # DTOs: Client, Project, Webhook, etc.
    store/                               # SQLite (modernc.org/sqlite, pure Go)
      migrations/                        # *.sql files, applied in order
      pc.go                              # local PC store: webhooks, captures, cursor
      relay.go                           # relay store: clients, projects, webhooks
      pc_test.go
      relay_test.go
      testutil_test.go                   # helpers for opening an isolated SQLite
    intercept/                           # traffic interception
      proxy/                             # MITM proxy core (pluggable transport)
      shellscript/                       # per-shell script generators
        bash.go bash_test.go
        fish.go fish_test.go
        powershell.go powershell_test.go
        gitbash.go gitbash_test.go
        doc.go                           # ShellScript(env) -> string dispatcher
      overridebin/                       # shim scripts for git/curl/node/etc.
      castore/                           # CA install (build-tag split)
        castore.go                        # interface
        castore_linux.go                 # #build linux
        castore_darwin.go                # #build darwin
        castore_windows.go               # #build windows
        castore_fake_test.go              # in-memory impl for tests
      intercept.go                       # Start/Stop orchestration w/ deps injected
    relayproto/                          # tunnel message types + encode/decode
      message.go                         # HELLO/ACK/REPLAY/PUSH/OK/ERROR
      message_test.go                    # round-trip, table-driven
    relayclient/                         # PC-side tunnel client
      client.go                          # dial, reconnect, send/recv loop
      client_test.go                     # against httptest.Server + real ws
    relayd/                              # relay server (importable package; named `relayd` since Go package names can't hyphenate)
      server.go                          # HTTP routes + WebSocket upgrade
      server_test.go                     # httptest + in-memory store
      auth.go                            # admin_token + client_token validation
      auth_test.go
    cli/                                # cobra command tree (root + subcommands incl. relay HTTP API wrappers)
      root.go version.go config.go
      clients.go projects.go webhooks.go
      clients_test.go                    # against httptest.Server
    tui/                                  # bubbletea models
      model.go
      updates_test.go                    # Msg/Model table-driven
    testutil/                            # shared test helpers (clocks, ids, tmp dirs)
      clock.go idgen.go golden.go
```

Build-tag convention: `//go:build linux`, `//go:build darwin`, `//go:build
windows`. Non-Linux files can be stubs returning `ErrUnsupportedOS` initially;
implementations land when tested on those OSes.

---

## 4. Testability principles (this is a learning project)

Codified rules every package obeys:

1. **Interfaces at every external boundary.** Each collaborator a package uses is
   passed in as a small interface, defined at the point of use (consumer-side
   interfaces, Go's implicit satisfaction). Examples:
   - `store.Store` for persistence (both `PCStore` and `RelayStore`).
   - `relayproto.Transport` for the WebSocket conn (so tests use fakes).
   - `clock.Clock` and `idgen.IDGen` so tests are deterministic.
   - `castore.Installer` for CA trust-store mutation.
2. **Constructor injection with functional options.** Public types expose
   `New(opts ...Option)`; `WithStore`, `WithClock`, `WithIDGen`, `WithLogger`
   let tests substitute any dep. Production wiring in `internal/app` passes
   concrete implementations.
3. **No package-level mutable state.** No `var now = time.Now`. No globals
   holding config. Everything flows through a struct.
4. **Pure functions where possible.** `shellscript.Bash(env)` is `func(env Env)
   string` — no I/O, trivially table-tested.
5. **Table-driven tests are the default.** Every test that has ≥2 cases is a
   `tests := []struct{...}{...}` loop with `t.Run(tc.name, ...)`.
6. **Real SQLite in tests.** Open `:memory:` (or a tmp file via `t.TempDir()`)
   for each test; avoid mocking the database. Migrations run in a helper.
7. **`httptest.Server` for HTTP.** Relay admin routes and the tunnel WebSocket
   are tested against an in-process `httptest.NewServer`.
8. **Golden files for generated shell scripts.** `internal/intercept/shellscript`
   uses `testdata/*.golden` snapshots; `go test -update` refreshes them.
9. **`t.Cleanup`** for resources (DBs, temp dirs, servers).
10. **Stdlib first, minimal helpers.** We use `testing` + a tiny `internal/testutil`
    (fake clock, fixed ID generator, golden helpers). No testify unless a clear
    payoff emerges — keeps the learning surface focused.
11. **Tests live next to code**, named `foo_test.go` (white-box, same package) by
    default; use `package foo_test` (black-box) only when testing the public API
    surface specifically.
12. **One behaviour per test** where feasible; composite flows live in `_test.go`
    `TestIntegration*` functions gated behind a build tag if slow.

Learning checklist I will intentionally demonstrate in the first few packages:

- subtests (`t.Run`) and `t.Parallel()` for independent cases
- `t.Helper()` in assertion helpers
- `t.TempDir()` and `t.Setenv()` (Go 1.17+; we're on 1.26)
- `errors.Is` / `errors.As` in error assertions
- `testing.TB` parameters so helpers accept both `*testing.T` and `*testing.B`
- `go test -race` (always on locally); CI runs `-race -shuffle=on`
- coverage gates: aim ≥85% on `internal/relayproto`, `internal/store`,
  `internal/intercept/shellscript` (the pure-logic cores)

---

## 5. The "HTTP + CLI compatible" pattern

There is **one API contract** in `internal/api/types.go` — request/response DTOs
used by three consumers:

```mermaid
graph LR
    T[internal/api/types.go]
    T --> H[relayd HTTP handlers]
    T --> C[wiretap CLI subcommands]
    T --> G[Wails/GUI optional calls]
```

- `relayd` registers HTTP routes: `POST /register`, `GET /clients`,
  `GET /projects`, `POST /projects` (reclaim), `GET /projects/:p/webhooks`,
  `POST /projects/:p/webhooks/:seq/replay`, `GET /health`. All JSON, all using
  `internal/api` types.
- `wiretap relay clients list` (and friends) instantiate `api.Client` (a typed
  HTTP client) pointed at `relay.url` with auth headers, call the same routes,
  and pretty-print the JSON. So `curl` and `wiretap relay ...` hit the **same**
  endpoints with the **same** payloads.
- This means a new admin capability follows a fixed recipe:
  1. define types in `internal/api`
  2. add the HTTP handler in `internal/relayd/server.go` (+ test)
  3. add the CLI subcommand in `internal/clitwo` (+ test using `httptest`)
  4. (optional) wire into TUI/GUI

Open question for you (answer before phase 4): should the **local app** also
expose a 127.0.0.1 HTTP control API so external scripts can query captures
(`GET http://127.0.0.1:PORT/local/webhooks`)? Cheap to add and matches the
"everything is an HTTP API" theme; useful for automation. Default: **yes**, add
it. I'll confirm before building.

---

## 6. Data models

### relayd SQLite (`relay.db`)

```sql
CREATE TABLE clients (
    client_id     TEXT PRIMARY KEY,
    client_token  TEXT NOT NULL,
    display_name  TEXT,
    created_at    INTEGER NOT NULL,
    last_seen_at  INTEGER
);

CREATE TABLE projects (
    path         TEXT PRIMARY KEY,        -- "project-a"
    client_id    TEXT NOT NULL REFERENCES clients(client_id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    acked_seq    INTEGER NOT NULL DEFAULT 0   -- relay's view of PC cursor per project
);

CREATE TABLE webhooks (
    project      TEXT NOT NULL REFERENCES projects(path) ON DELETE CASCADE,
    seq          INTEGER NOT NULL,
    received_at  INTEGER NOT NULL,
    source_ip    TEXT,
    method       TEXT NOT NULL,
    path         TEXT,                    -- full nested path after project segment
    headers      TEXT NOT NULL,           -- JSON
    body         BLOB,
    delivered    INTEGER NOT NULL DEFAULT 0,
    delivered_at INTEGER,
    PRIMARY KEY (project, seq)
);
CREATE INDEX idx_undelivered ON webhooks(project, seq) WHERE delivered = 0;
```

### wiretap PC SQLite (`wiretap.db`)

```sql
CREATE TABLE webhooks (
    project      TEXT NOT NULL,
    seq          INTEGER NOT NULL,
    received_at  INTEGER NOT NULL,        -- from relay
    stored_at    INTEGER NOT NULL,        -- local arrival time
    source_ip    TEXT,
    method       TEXT,
    path         TEXT,
    headers      TEXT,
    body         BLOB,
    PRIMARY KEY (project, seq)            -- dedup by (project, seq) on reconnect
);

CREATE TABLE traffic_captures (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    at           INTEGER NOT NULL,
    method       TEXT,
    url          TEXT,
    req_headers  TEXT,
    req_body     BLOB,
    status       INTEGER,
    resp_headers TEXT,
    resp_body    BLOB
);

CREATE TABLE relay_cursor (
    project      TEXT PRIMARY KEY,
    last_seq     INTEGER NOT NULL
);  -- authoritative cursor; used in HELLO on reconnect
```

---

## 7. Tunnel protocol (WebSocket over TLS)

Message envelope is a tagged JSON union. Defined in `internal/relayproto`.

```
PC → relayd:
  HELLO    { type: "hello", client_id, client_token, last_seqs: { "project-a": 420 } }
  ACK      { type: "ack", project, up_to_seq }
  REPLAY   { type: "replay", project, seqs: [422, 423] }   # re-deliver to local

relayd → PC:
  OK       { type: "ok", projects: ["project-a"], resume_from: { "project-a": 420 } }
  PUSH     { type: "push", project, seq, method, path, headers, body, received_at, source_ip }
  ERROR    { type: "error", code, message }
```

Reliability rules (already discussed, locked here):

- PC declares `last_seqs` on every HELLO; relay treats it as ground truth.
- Idempotent on PC: `INSERT OR IGNORE` keyed by `(project, seq)`.
- Reconnect uses exponential backoff 1s→30s with jitter; ping/pong every 30s.
- Relay retains delivered rows for a TTL (default 7d), then vacuums.
- After a successful `ACK up_to_seq=N`, relay updates `projects.acked_seq`.

---

## 8. HTTP API (relayd)

All routes return JSON. Admin routes require `X-Admin-Token`; client routes
require `Authorization: Basic client_id:client_token` OR a tunnel-attached
session.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/register` | admin | claim `client_id`/`client_token` + bind projects |
| GET | `/health` | none | liveness |
| POST | `/inbox/:project` (alias `/`) | none | ingress for webhooks (path preserved) |
| GET | `/admin/clients` | admin | list clients |
| GET | `/admin/clients/:id` | admin | client detail + bound projects |
| DELETE | `/admin/clients/:id` | admin | revoke client (frees its projects) |
| GET | `/admin/projects` | admin | list projects + acked_seq |
| POST | `/admin/projects` | admin | reclaim a path under a new client (`--force`) |
| GET | `/admin/projects/:p/webhooks` | admin/owner | paginated history |
| POST | `/admin/projects/:p/webhooks/:seq/replay` | admin/owner | re-push to PC over tunnel |
| GET | `/tunnel` | client | WebSocket upgrade (the tunnel itself) |

Path-naming regex for `:project`: `^[a-z0-9][a-z0-9-]{1,62}$`. Reserved roots:
`tunnel`, `register`, `admin`, `health`.

---

## 9. Module-by-module testing map

| Package | Test style | Doubles |
|---|---|---|
| `internal/relayproto` | Table-driven encode/decode round-trips | none (pure) |
| `internal/store` | Real `:memory:` SQLite per test | none |
| `internal/relayd` | `httptest.NewServer` + in-memory store + real WS handshakes | `FakeStore`, `FakeClock`, `FakeIDGen` |
| `internal/relayclient` | `httptest.NewServer` upgraded to WS | `FakeTransport`, `FakeStore` |
| `internal/cli` | `httptest.NewServer` + stdlib assertions | `FakeClock` |
| `internal/intercept/shellscript` | Golden files + table-driven | none (pure) |
| `internal/intercept/proxy` | `httptest.NewTLSServer` as upstream | `FakeCA` |
| `internal/intercept/castore` | interface-only tests using `castore_fake_test.go` | `FakeInstaller` |
| `internal/tui` | `Model`/`Msg` table-driven with tea `TestModel` | `FakeStore` |
| `internal/config` | `t.TempDir()` + `t.Setenv()` | none |
| `internal/app` | Light integration: wire real deps, exercise one end-to-end flow | none (integration) |

---

## 10. Build phases (what lands in what order)

Each phase ends with a green test suite for its packages before moving on.

### Phase 0 — Scaffolding (no behaviour)
- Rename module to `github.com/plutack/wiretap` in `go.mod`.
- Create directory layout above (empty `doc.go`s).
- `cmd/wiretap/main.go`: cobra root with `version`, `config init` only.
- `cmd/relayd/main.go`: serves `/health` and exits cleanly.
- `internal/config`, `internal/testutil` baselines.
- Wire `go test ./...` clean (zero tests pass trivially).

### Phase 1 — Cross-cutting cores (pure logic, easiest to test)
- `internal/relayproto` types + encode/decode + table tests.
- `internal/store` migrations + `RelayStore` + `PCStore` + tests.
- `internal/intercept/shellscript` dispatcher + bash/fish/powershell/gitbash
  generators + golden files. **No stop_interception** omitted; we include
  `wiretap_stop_interception` in every shell per your latest call.

### Phase 2 — relayd MVP (HTTP + tunnel)
- `internal/api/types.go`, `server.go` routes from §8.
- `internal/relayd` registration, auth, `/inbox/:project` ingress,
  `/tunnel` WebSocket with §7 protocol.
- `internal/clitwo` subcommands wrap the same routes.
- Integration test: register → ingress → tunnel PUSH → PC ACK → acked_seq
  advanced.

### Phase 3 — wiretap local (relay client + CLI)
- `internal/relayclient` dial/reconnect/recv loop.
- Cursor loading/saving via `PCStore`.
- `wiretap relay register`, `wiretap relay clients list`, ... cobra commands.
- TUI stub: `wiretap tui` shows live webhook feed from PC store.

### Phase 4 — Traffic interception
- `internal/intercept/castore` Linux impl (+ stubs for darwin/windows).
- `internal/intercept/proxy` MITM core using `net/http` + CONNECT.
- `internal/intercept` orchestration: write startup-file gated blocks,
  generate override-bin shims, spawn shell with `WIRETAP_ACTIVE=1`.
- `wiretap intercept start` / `wiretap intercept stop`.

### Phase 5 — Wails GUI
- `ui/` minimal Tailwind + a component skeleton.
- Two tabs: Traffic (list of `traffic_captures`) and Webhooks (list of
  `webhooks`), replay button.
- Bindings call into `internal/app` (already tested).

### Phase 6 — Hardening
- Playground for cross-platform CA on darwin/windows.
- Relay token rotation, multi-client admin UI (CLI covers it already).
- Docs + README.

---

## 11. Decisions locked

| Area | Decision |
|---|---|
| Name | **wiretap** (module `github.com/plutack/wiretap`) |
| GUI | Wails |
| TUI | Bubbletea, behind `wiretap tui` |
| CLI | Cobra + Viper |
| Storage (both sides) | SQLite via `modernc.org/sqlite` (pure Go, no cgo) |
| Relay protocol | WebSocket over TLS, ack-cursor store-and-forward |
| Relay identification | client_id + client_token; `admin_token` for registration |
| Path routing | `/project-a` — first segment is the project; nested paths preserved |
| Projects per client | multiple (one tunnel, multiplexed) |
| Project reclaim | `--force` + admin token moves a path to a new client_id |
| Path naming regex | `^[a-z0-9][a-z0-9-]{1,62}$` |
| Env markers | `WIRETAP_ACTIVE`, `WIRETAP_OVERRIDE_BIN` |
| Startup-file section | `# --wiretap-intercept--` / `# --wiretap-intercept-end--` |
| Stop interception | injected for every shell; unsets `WIRETAP_ACTIVE` + restores env |
| Cross-platform | Linux first; darwin/windows behind build tags later |
| Go version | `1.26.5` (`.go-version` created) |
| API surface | one contract in `internal/api`; HTTP routes + cobra frontends |
| Testing | stdlib + minimal `internal/testutil`; table-driven; real SQLite; httptest |

## 12. Open questions (resolved)

1. **Local control HTTP API** — **yes**. The local app exposes a 127.0.0.1 HTTP
   control API so external scripts can query captures (`/local/webhooks`, etc.).
   Built in Phase 4 alongside the interception work.
2. **Relay binary name** — **`wiretap-relay`**. The importable server
   package stays `internal/relayd` (Go package names cannot contain
   hyphens); the binary lives in `cmd/wiretap-relay` and is named
   `wiretap-relay` so it is clearly part of the wiretap family.
3. **`wiretap intercept start` behaviour** — **spawn an interactive shell**
   (httptoolkit-style). Implemented in Phase 4. *(Interpreted as "spawn" from the
   reply; revisit before Phase 4 if this is wrong.)*
4. **Wails version** — **v2** (stable). Migrate to v3 later if it matures.