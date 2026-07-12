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

// ListClients returns every registered client, ordered by creation.
// Used by GET /admin/clients. last_seen_at is 0 (zero time) when NULL.
func (s *RelayStore) ListClients(ctx context.Context, includeProjects bool) ([]ClientRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT client_id, client_token, COALESCE(display_name, ''), created_at, COALESCE(last_seen_at, 0) FROM clients ORDER BY created_at",
	)
	if err != nil {
		return nil, fmt.Errorf("ListClients: %w", err)
	}
	defer rows.Close()
	var out []ClientRow
	for rows.Next() {
		var c ClientRow
		var created, lastSeen int64
		if err := rows.Scan(&c.ClientID, &c.ClientToken, &c.DisplayName, &created, &lastSeen); err != nil {
			return nil, fmt.Errorf("ListClients scan: %w", err)
		}
		c.CreatedAt = time.Unix(created, 0).UTC()
		if lastSeen > 0 {
			c.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListClients rows: %w", err)
	}
	return out, nil
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

// ListProjects returns every claimed project path with its owning client and
// acked seq, ordered by path. Used by GET /admin/projects.
func (s *RelayStore) ListProjects(ctx context.Context) ([]ProjectRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT path, client_id, created_at, acked_seq FROM projects ORDER BY path",
	)
	if err != nil {
		return nil, fmt.Errorf("ListProjects: %w", err)
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		var p ProjectRow
		var created int64
		if err := rows.Scan(&p.Path, &p.ClientID, &created, &p.AckedSeq); err != nil {
			return nil, fmt.Errorf("ListProjects scan: %w", err)
		}
		p.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListProjects rows: %w", err)
	}
	return out, nil
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
//
// Allocation reads projects.next_seq and bumps it under a transaction so
// concurrent ingress cannot share a seq. This is deliberately decoupled from
// projects.acked_seq — that column tracks the PC's delivery cursor (see
// MarkDelivered). Conflating them would make the relay think every
// freshly-ingressed webhook was already acked.
//
// SQLite's single-writer model serialises transactions, so we do not need
// an explicit advisory lock; BEGIN IMMEDIATE could be added later if a
// highly contended relay ever shows SQLITE_BUSY under load.
func (s *RelayStore) NextWebhookSeq(ctx context.Context, project string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("NextWebhookSeq %q begin: %w", project, err)
	}
	defer func() { _ = tx.Rollback() }()

	var nextSeq int64
	if err := tx.QueryRowContext(ctx,
		"SELECT next_seq FROM projects WHERE path = ?", project,
	).Scan(&nextSeq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("NextWebhookSeq %q: %w", project, ErrNotFound)
		}
		return 0, fmt.Errorf("NextWebhookSeq %q select: %w", project, err)
	}
	allocated := nextSeq
	if _, err := tx.ExecContext(ctx,
		"UPDATE projects SET next_seq = ? WHERE path = ?", nextSeq+1, project,
	); err != nil {
		return 0, fmt.Errorf("NextWebhookSeq %q update: %w", project, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("NextWebhookSeq %q commit: %w", project, err)
	}
	return allocated, nil
}

// InsertWebhook stores an inbound webhook. Caller must have allocated seq
// via NextWebhookSeq (or otherwise guaranteed uniqueness); this method
// does not allocate. receivedAt stamps the relay's receipt time.
//
// Both Body and RawHeaders are stored as BLOBs, byte-exact, so replay and
// debug display show exactly what the relay received.
func (s *RelayStore) InsertWebhook(ctx context.Context, w WebhookRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhooks (project, seq, received_at, source_ip, method, path, headers, raw_headers, body, delivered)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		w.Project, w.Seq, w.ReceivedAt.Unix(), w.SourceIP, w.Method, w.Path, w.HeadersJSON, w.RawHeaders, w.Body,
	)
	return wrapExec(err, "InsertWebhook", fmt.Sprintf("%s/%d", w.Project, w.Seq))
}

// WebhooksAfter returns all undelivered webhooks for project with seq >
// afterSeq, in ascending seq order. This is the tunnel's sweep query.
func (s *RelayStore) WebhooksAfter(ctx context.Context, project string, afterSeq int64) ([]WebhookRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT project, seq, received_at, COALESCE(source_ip, ''), method, COALESCE(path, ''), headers, COALESCE(raw_headers, ''), body
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
		var rawHeaders []byte
		if err := rows.Scan(&w.Project, &w.Seq, &received, &w.SourceIP, &w.Method, &w.Path, &w.HeadersJSON, &rawHeaders, &w.Body); err != nil {
			return nil, fmt.Errorf("WebhooksAfter %q scan: %w", project, err)
		}
		w.RawHeaders = rawHeaders
		w.ReceivedAt = time.Unix(received, 0).UTC()
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("WebhooksAfter %q rows: %w", project, err)
	}
	return out, nil
}

// ListWebhooks returns up to limit webhooks for project with seq <=
// BeforeSeq (descending), starting from afterSeq (ascending) when set. Used
// by GET /admin/projects/:p/webhooks for paginated history reads.
//
// Parameters:
//   - afterSeq: only return rows with seq > this (0 = from start)
//   - limit:    page size; default 50 when 0
//
// Returns rows in ascending seq order plus the next cursor (next_after_seq
// is the highest seq returned + 1). When fewer rows than limit are returned,
// NextAfterSeq is 0 to signal end-of-data.
func (s *RelayStore) ListWebhooks(ctx context.Context, project string, afterSeq, limit int64) ([]WebhookRow, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT project, seq, received_at, COALESCE(source_ip, ''), method, COALESCE(path, ''), headers, COALESCE(raw_headers, ''), body
		 FROM webhooks
		 WHERE project = ? AND seq > ?
		 ORDER BY seq ASC
		 LIMIT ?`,
		project, afterSeq, limit,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("ListWebhooks %q: %w", project, err)
	}
	defer rows.Close()
	var out []WebhookRow
	var maxSeq int64
	for rows.Next() {
		var w WebhookRow
		var received int64
		var rawHeaders []byte
		if err := rows.Scan(&w.Project, &w.Seq, &received, &w.SourceIP, &w.Method, &w.Path, &w.HeadersJSON, &rawHeaders, &w.Body); err != nil {
			return nil, 0, fmt.Errorf("ListWebhooks %q scan: %w", project, err)
		}
		w.RawHeaders = rawHeaders
		w.ReceivedAt = time.Unix(received, 0).UTC()
		if w.Seq > maxSeq {
			maxSeq = w.Seq
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("ListWebhooks %q rows: %w", project, err)
	}
	if int64(len(out)) < limit {
		// Less than a full page means end of data.
		return out, 0, nil
	}
	return out, maxSeq + 1, nil
}

// WebhookBySeq returns a specific webhook by (project, seq). Used by the
// replay route to re-push an already-delivered webhook down the tunnel.
func (s *RelayStore) WebhookBySeq(ctx context.Context, project string, seq int64) (*WebhookRow, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT project, seq, received_at, COALESCE(source_ip, ''), method, COALESCE(path, ''), headers, COALESCE(raw_headers, ''), body
		 FROM webhooks WHERE project = ? AND seq = ?`,
		project, seq,
	)
	var w WebhookRow
	var received int64
	var rawHeaders []byte
	if err := row.Scan(&w.Project, &w.Seq, &received, &w.SourceIP, &w.Method, &w.Path, &w.HeadersJSON, &rawHeaders, &w.Body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("WebhookBySeq %s/%d: %w", project, seq, ErrNotFound)
		}
		return nil, fmt.Errorf("WebhookBySeq %s/%d: %w", project, seq, err)
	}
	w.RawHeaders = rawHeaders
	w.ReceivedAt = time.Unix(received, 0).UTC()
	return &w, nil
}

// MarkDelivered flips the delivered flag on rows up to and including
// upToSeq and stamps delivered_at. It also advances projects.acked_seq to
// max(acked_seq, upToSeq) — the relay's view of the PC's cursor — and is
// idempotent: re-acking an old seq is a no-op (no rows match the WHERE, and
// the GREATEST clamp keeps acked_seq from going backwards).
//
// The work runs in a single transaction so the delivered flag and the
// cursor move atomically; an acked webhook can never be visibly undelivered
// to a later reader.
func (s *RelayStore) MarkDelivered(ctx context.Context, project string, upToSeq int64, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("MarkDelivered %q begin: %w", project, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`UPDATE webhooks SET delivered = 1, delivered_at = ?
		 WHERE project = ? AND seq <= ? AND delivered = 0`,
		now.Unix(), project, upToSeq,
	); err != nil {
		return fmt.Errorf("MarkDelivered %q update rows: %w", project, err)
	}
	// Advance the cursor monotonically: only move forward when the new seq
	// is greater than the current value. SQLite's MAX() over a constant works
	// for this clamp without a separate SELECT.
	if _, err := tx.ExecContext(ctx,
		`UPDATE projects SET acked_seq = ?
		 WHERE path = ? AND ? > acked_seq`,
		upToSeq, project, upToSeq,
	); err != nil {
		return fmt.Errorf("MarkDelivered %q update cursor: %w", project, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("MarkDelivered %q commit: %w", project, err)
	}
	return nil
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
