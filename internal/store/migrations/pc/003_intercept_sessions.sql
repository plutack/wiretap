-- Interception sessions: one row per `wiretap intercept start` run so
-- captures can be grouped, referenced, and compared across sessions without
-- exporting anything (the reason wiretap replaced mitmproxy workflows).
--
-- ended_at stays NULL while the session is running — and after a crash, which
-- doubles as a useful "did not shut down cleanly" marker.
CREATE TABLE IF NOT EXISTS intercept_sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at INTEGER NOT NULL,
    ended_at   INTEGER,
    shell      TEXT,
    proxy_addr TEXT
);

-- Tag each capture with its session. NULL for rows captured before this
-- migration and for inserts outside a session (e.g. tests, direct API use).
-- The migration runner tolerates the duplicate-column error on re-runs
-- (migrations are replayed at every startup; see migrate.go).
ALTER TABLE traffic_captures ADD COLUMN session_id INTEGER;

CREATE INDEX IF NOT EXISTS idx_traffic_captures_session
    ON traffic_captures(session_id);
