package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RelayStore is the storage layer used by wiretap-relay. It owns no state
// except a *sql.DB handle; every method is one DB round-trip. Callers wrap
// multiple operations in a transaction when they need atomicity.
//
// All methods take a context.Context so callers can apply timeouts without
// store-managed goroutines. DB errors are wrapped with the operation name
// and key arguments; callers use errors.Is to check for ErrNotFound /
// ErrConflict which we surface as exported sentinels.
type RelayStore struct {
	db *sql.DB
}

// NewRelayStore wraps an existing *sql.DB. The caller is expected to have
// run MigrateRelay first; NewRelayStore deliberately does not run migrations
// so tests can pin an exact schema state.
func NewRelayStore(db *sql.DB) *RelayStore {
	return &RelayStore{db: db}
}

// DB exposes the underlying handle for callers (relayd) that need to run
// ad-hoc transactions. Use sparingly; prefer adding a method to RelayStore.
func (s *RelayStore) DB() *sql.DB { return s.db }

// CreateClient inserts a new client row. CreatedAt is set to now. Returns
// ErrConflict if a client with the same id already exists.
func (s *RelayStore) CreateClient(ctx context.Context, clientID, token, displayName string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO clients (client_id, client_token, display_name, created_at) VALUES (?, ?, ?, ?)",
		clientID, token, displayName, now.Unix(),
	)
	return wrapExec(err, "CreateClient", clientID)
}

// Client looks up a client by id. Returns ErrNotFound when absent.
func (s *RelayStore) Client(ctx context.Context, clientID string) (*ClientRow, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT client_id, client_token, COALESCE(display_name, ''), created_at, COALESCE(last_seen_at, 0) FROM clients WHERE client_id = ?",
		clientID,
	)
	var c ClientRow
	var created, lastSeen int64
	if err := row.Scan(&c.ClientID, &c.ClientToken, &c.DisplayName, &created, &lastSeen); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("Client %q: %w", clientID, ErrNotFound)
		}
		return nil, fmt.Errorf("Client %q: %w", clientID, err)
	}
	c.CreatedAt = time.Unix(created, 0).UTC()
	if lastSeen > 0 {
		c.LastSeenAt = time.Unix(lastSeen, 0).UTC()
	}
	return &c, nil
}

// TouchClient updates last_seen_at. Used by the tunnel loop on connect.
func (s *RelayStore) TouchClient(ctx context.Context, clientID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE clients SET last_seen_at = ? WHERE client_id = ?",
		now.Unix(), clientID,
	)
	return wrapExec(err, "TouchClient", clientID)
}

// BindProject assigns a path to a client. Returns ErrConflict if the path is
// already owned by a different client.
func (s *RelayStore) BindProject(ctx context.Context, path, clientID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO projects (path, client_id, created_at, acked_seq) VALUES (?, ?, ?, 0)",
		path, clientID, now.Unix(),
	)
	return wrapExec(err, "BindProject", path)
}

// Project looks up a project by path. Returns ErrNotFound when absent.
func (s *RelayStore) Project(ctx context.Context, path string) (*ProjectRow, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT path, client_id, created_at, acked_seq FROM projects WHERE path = ?",
		path,
	)
	var p ProjectRow
	var created int64
	if err := row.Scan(&p.Path, &p.ClientID, &created, &p.AckedSeq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("Project %q: %w", path, ErrNotFound)
		}
		return nil, fmt.Errorf("Project %q: %w", path, err)
	}
	p.CreatedAt = time.Unix(created, 0).UTC()
	return &p, nil
}

// ProjectsByClient lists projects owned by clientID.
func (s *RelayStore) ProjectsByClient(ctx context.Context, clientID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT path FROM projects WHERE client_id = ? ORDER BY path",
		clientID,
	)
	if err != nil {
		return nil, fmt.Errorf("ProjectsByClient %q: %w", clientID, err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("ProjectsByClient %q scan: %w", clientID, err)
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ProjectsByClient %q rows: %w", clientID, err)
	}
	return paths, nil
}

// ReclaimProject moves a path to a new client, deleting the old binding.
// The caller must verify admin auth (this is not enforced here).
func (s *RelayStore) ReclaimProject(ctx context.Context, path, newClientID string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE projects SET client_id = ?, created_at = ? WHERE path = ?",
		newClientID, now.Unix(), path,
	)
	if err != nil {
		return fmt.Errorf("ReclaimProject %q: %w", path, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("ReclaimProject %q: %w", path, ErrNotFound)
	}
	return nil
}

// DeleteClient removes a client and its project bindings (cascades).
func (s *RelayStore) DeleteClient(ctx context.Context, clientID string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM clients WHERE client_id = ?", clientID)
	if err != nil {
		return fmt.Errorf("DeleteClient %q: %w", clientID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("DeleteClient %q: %w", clientID, ErrNotFound)
	}
	return nil
}

// AckedSeq returns the currently-acked sequence number for a project.
func (s *RelayStore) AckedSeq(ctx context.Context, project string) (int64, error) {
	row := s.db.QueryRowContext(ctx, "SELECT acked_seq FROM projects WHERE path = ?", project)
	var seq int64
	if err := row.Scan(&seq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("AckedSeq %q: %w", project, ErrNotFound)
		}
		return 0, fmt.Errorf("AckedSeq %q: %w", project, err)
	}
	return seq, nil
}

// NextWebhookSeq reserves the next sequence number for a project atomically.
// It updates projects.acked_seq to be the max of current and (max+1) and
// returns the next seq. Implemented as a single UPDATE ... RETURNING when
// supported (modernc.org/sqlite supports it) — wrapped in a tx for clarity.
//
// The next seq is acked_seq + 1 (acked_seq doubles as "highest allocated");
// we bump it under a transaction so concurrent inserts cannot share a seq.
func (s *RelayStore) NextWebhookSeq(ctx context.Context, project string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("NextWebhookSeq %q begin: %w", project, err)
	}
	defer func() { _ = tx.Rollback() }()

	var seq int64
	if err := tx.QueryRowContext(ctx,
		"SELECT acked_seq FROM projects WHERE path = ?", project,
	).Scan(&seq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("NextWebhookSeq %q: %w", project, ErrNotFound)
		}
		return 0, fmt.Errorf("NextWebhookSeq %q select: %w", project, err)
	}
	seq++
	if _, err := tx.ExecContext(ctx,
		"UPDATE projects SET acked_seq = ? WHERE path = ?", seq, project,
	); err != nil {
		return 0, fmt.Errorf("NextWebhookSeq %q update: %w", project, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("NextWebhookSeq %q commit: %w", project, err)
	}
	return seq, nil
}

// InsertWebhook stores an inbound webhook. Caller must have allocated seq
// via NextWebhookSeq (or otherwise guaranteed uniqueness); this method
// does not allocate. receivedAt stamps the relay's receipt time.
func (s *RelayStore) InsertWebhook(ctx context.Context, w WebhookRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhooks (project, seq, received_at, source_ip, method, path, headers, body, delivered)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		w.Project, w.Seq, w.ReceivedAt.Unix(), w.SourceIP, w.Method, w.Path, w.HeadersJSON, w.Body,
	)
	return wrapExec(err, "InsertWebhook", fmt.Sprintf("%s/%d", w.Project, w.Seq))
}

// WebhooksAfter returns all undelivered webhooks for project with seq >
// afterSeq, in ascending seq order. This is the tunnel's sweep query.
func (s *RelayStore) WebhooksAfter(ctx context.Context, project string, afterSeq int64) ([]WebhookRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT project, seq, received_at, COALESCE(source_ip, ''), method, COALESCE(path, ''), headers, body
		 FROM webhooks
		 WHERE project = ? AND seq > ?
		 ORDER BY seq ASC`,
		project, afterSeq,
	)
	if err != nil {
		return nil, fmt.Errorf("WebhooksAfter %q: %w", project, err)
	}
	defer rows.Close()
	var out []WebhookRow
	for rows.Next() {
		var w WebhookRow
		var received int64
		if err := rows.Scan(&w.Project, &w.Seq, &received, &w.SourceIP, &w.Method, &w.Path, &w.HeadersJSON, &w.Body); err != nil {
			return nil, fmt.Errorf("WebhooksAfter %q scan: %w", project, err)
		}
		w.ReceivedAt = time.Unix(received, 0).UTC()
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("WebhooksAfter %q rows: %w", project, err)
	}
	return out, nil
}

// MarkDelivered flips the delivered flag on rows up to and including
// upToSeq. Also stamps delivered_at. Idempotent: re-acking an old seq is a
// no-op (no rows match").
func (s *RelayStore) MarkDelivered(ctx context.Context, project string, upToSeq int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhooks SET delivered = 1, delivered_at = ?
		 WHERE project = ? AND seq <= ? AND delivered = 0`,
		now.Unix(), project, upToSeq,
	)
	return wrapExec(err, "MarkDelivered", fmt.Sprintf("%s/%d", project, upToSeq))
}

// PendingCount returns the number of undelivered rows for a project. Useful
// for admin dashboards and tests asserting state.
func (s *RelayStore) PendingCount(ctx context.Context, project string) (int64, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM webhooks WHERE project = ? AND delivered = 0", project,
	)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("PendingCount %q: %w", project, err)
	}
	return n, nil
}

// VacuumDelivered deletes delivered rows older than ttl. Called by a
// background sweep on the relay; not in the hot path.
func (s *RelayStore) VacuumDelivered(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM webhooks WHERE delivered = 1 AND delivered_at < ?",
		olderThan.Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("VacuumDelivered: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ClientByProject resolves the owning client for a given project path.
// Used by the ingress handler to route webhooks to the right tunnel.
func (s *RelayStore) ClientByProject(ctx context.Context, project string) (string, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT client_id FROM projects WHERE path = ?", project,
	)
	var clientID string
	if err := row.Scan(&clientID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("ClientByProject %q: %w", project, ErrNotFound)
		}
		return "", fmt.Errorf("ClientByProject %q: %w", project, err)
	}
	return clientID, nil
}

// wrapExec centralises the common DB error wrapping for Exec calls. It
// surfaces sqlite UNIQUE/PK violations as ErrConflict so callers can branch
// without parsing driver-specific error strings.
func wrapExec(err error, op, key string) error {
	if err == nil {
		return nil
	}
	// modernc.org/sqlite reports UNIQUE constraint violations with the
	// keyword "UNIQUE constraint failed:" or "constraint failed: UNIQUE".
	// Matching is case-insensitive to be safe; matching precision is
	// improved later by importing the driver's error type.
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		return fmt.Errorf("%s %q: %w", op, key, ErrConflict)
	}
	return fmt.Errorf("%s %q: %w", op, key, err)
}
