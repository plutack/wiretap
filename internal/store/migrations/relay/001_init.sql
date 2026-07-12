-- relay schema (wiretap-relay.db) — authoritative registry + webhook buffer.
-- See docs/PLAN.md §6 for the rationale.

-- Registered wiretap clients (one per machine).
CREATE TABLE IF NOT EXISTS clients (
    client_id     TEXT PRIMARY KEY,
    client_token  TEXT NOT NULL,
    display_name  TEXT,
    created_at    INTEGER NOT NULL,
    last_seen_at  INTEGER
);

-- Project paths claimed by clients; e.g. "project-a".
CREATE TABLE IF NOT EXISTS projects (
    path         TEXT PRIMARY KEY,
    client_id    TEXT NOT NULL REFERENCES clients(client_id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    acked_seq    INTEGER NOT NULL DEFAULT 0
);

-- Webhooks received at the relay, awaiting or already delivered over the
-- tunnel. (project, seq) is the natural key for dedup and cursor resume.
-- Webhooks received at the relay, awaiting or already delivered over the
-- tunnel. (project, seq) is the natural key for dedup and cursor resume.
--
-- headers      -- parsed http.Header as JSON (queryable, lossy on order)
-- raw_headers  -- the raw header block exactly as received (BLOB). Preserves
--                 ordering and duplicate headers (X-Forwarded-For: a\r\n...
--                 X-Forwarded-For: b). Used for faithful replay and display.
-- body         -- raw request body, byte-exact (BLOB).
CREATE TABLE IF NOT EXISTS webhooks (
    project      TEXT NOT NULL REFERENCES projects(path) ON DELETE CASCADE,
    seq          INTEGER NOT NULL,
    received_at  INTEGER NOT NULL,
    source_ip    TEXT,
    method       TEXT NOT NULL,
    path         TEXT,
    headers      TEXT NOT NULL,
    raw_headers  BLOB,
    body         BLOB,
    delivered    INTEGER NOT NULL DEFAULT 0,
    delivered_at INTEGER,
    PRIMARY KEY (project, seq)
);

-- Partial index over undelivered rows: the tunnel's sweep query.
CREATE INDEX IF NOT EXISTS idx_undelivered
    ON webhooks(project, seq) WHERE delivered = 0;