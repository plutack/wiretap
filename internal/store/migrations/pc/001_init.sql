-- PC schema (wiretap.db) — local cache of received webhooks + traffic
-- captures + the authoritative per-project cursor sent in HELLO.
-- See docs/PLAN.md §6 for the rationale.

-- Webhooks pushed to this PC by the relay. (project, seq) is the natural
-- dedup key: a reconnect re-pushes rows the PC already has, so inserts are
-- INSERT OR IGNORE.
CREATE TABLE IF NOT EXISTS webhooks (
    project      TEXT NOT NULL,
    seq          INTEGER NOT NULL,
    received_at  INTEGER NOT NULL,
    stored_at    INTEGER NOT NULL,
    source_ip    TEXT,
    method       TEXT,
    path         TEXT,
    headers     TEXT,
    body         BLOB,
    PRIMARY KEY (project, seq)
);

-- Outbound HTTP captures from the MITM proxy. One row per request/response
-- pair. id is autoincrement so the UI can show a stable monotonic order.
CREATE TABLE IF NOT EXISTS traffic_captures (
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

-- Authoritative cursor: the highest seq persisted locally per project.
-- Read on startup and sent in HELLO; updated whenever a Push is stored.
CREATE TABLE IF NOT EXISTS relay_cursor (
    project  TEXT PRIMARY KEY,
    last_seq INTEGER NOT NULL
);