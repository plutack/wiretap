-- Scripts: user-authored JavaScript payload transformations executed by
-- internal/scripting. One row per script.
--
-- "trigger" is quoted throughout because it is a reserved SQLite keyword; the
-- value is one of on_request / on_response / on_replay / on_webhook.
-- priority orders chained scripts sharing a trigger (lower runs first).
-- enabled is 0/1; timestamps are unix seconds.
CREATE TABLE IF NOT EXISTS scripts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    "trigger"   TEXT NOT NULL,
    body        TEXT NOT NULL,
    priority    INTEGER NOT NULL DEFAULT 0,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

-- Covers the hot path: fetch enabled scripts for a trigger in priority order.
CREATE INDEX IF NOT EXISTS idx_scripts_trigger
    ON scripts ("trigger", enabled, priority);
