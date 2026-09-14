-- Replace the one-owner project model with independent client subscriptions.

CREATE TABLE projects_v2 (
    path       TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    next_seq   INTEGER NOT NULL DEFAULT 1
);

INSERT INTO projects_v2 (path, created_at, next_seq)
SELECT path, created_at, next_seq FROM projects;

CREATE TABLE project_subscriptions_v2 (
    project    TEXT NOT NULL REFERENCES projects_v2(path) ON DELETE CASCADE,
    client_id  TEXT NOT NULL REFERENCES clients(client_id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    start_seq  INTEGER NOT NULL DEFAULT 0,
    acked_seq  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (project, client_id)
);

INSERT INTO project_subscriptions_v2
    (project, client_id, created_at, start_seq, acked_seq)
SELECT path, client_id, created_at, 0, acked_seq FROM projects;

CREATE TABLE webhooks_v2 (
    project     TEXT NOT NULL REFERENCES projects_v2(path) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    received_at INTEGER NOT NULL,
    source_ip   TEXT,
    method      TEXT NOT NULL,
    path        TEXT,
    headers     TEXT NOT NULL,
    raw_headers BLOB,
    body        BLOB,
    PRIMARY KEY (project, seq)
);

INSERT INTO webhooks_v2
    (project, seq, received_at, source_ip, method, path, headers, raw_headers, body)
SELECT project, seq, received_at, source_ip, method, path, headers, raw_headers, body
FROM webhooks;

DROP TABLE webhooks;
DROP TABLE projects;

ALTER TABLE projects_v2 RENAME TO projects;
ALTER TABLE project_subscriptions_v2 RENAME TO project_subscriptions;
ALTER TABLE webhooks_v2 RENAME TO webhooks;

CREATE INDEX idx_project_subscriptions_client
    ON project_subscriptions(client_id, project);

CREATE INDEX idx_webhooks_project_seq
    ON webhooks(project, seq);
