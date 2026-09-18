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

// BindProject creates a project and its first subscription atomically. It is
// used when a client creates a new path; an existing project always returns
// ErrConflict so clients cannot join guessable paths without admin approval.
func (s *RelayStore) BindProject(ctx context.Context, path, clientID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("BindProject %q begin: %w", path, err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO projects (path, created_at, next_seq) VALUES (?, ?, 1)", path, now.Unix(),
	); err != nil {
		return wrapExec(err, "BindProject", path)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO project_subscriptions (project, client_id, created_at, start_seq, acked_seq)
		 VALUES (?, ?, ?, 0, 0)`, path, clientID, now.Unix(),
	); err != nil {
		return wrapExec(err, "BindProject subscription", path+"/"+clientID)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("BindProject %q commit: %w", path, err)
	}
	return nil
}

// SubscribeProject adds a client to an existing project. By default the
// subscription starts after the newest allocated sequence so historical
// traffic is not disclosed unexpectedly. includeHistory starts at zero.
func (s *RelayStore) SubscribeProject(ctx context.Context, path, clientID string, includeHistory bool, now time.Time) error {
	startExpr := "next_seq - 1"
	if includeHistory {
		startExpr = "0"
	}
	query := `INSERT INTO project_subscriptions (project, client_id, created_at, start_seq, acked_seq)
		SELECT path, ?, ?, ` + startExpr + `, ` + startExpr + ` FROM projects WHERE path = ?`
	res, err := s.db.ExecContext(ctx, query, clientID, now.Unix(), path)
	if err != nil {
		return wrapExec(err, "SubscribeProject", path+"/"+clientID)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("SubscribeProject %q: %w", path, ErrNotFound)
	}
	return nil
}

// UnbindProject removes only this client's subscription. The project and its
// relay-side webhook history remain until an administrator deletes them.
func (s *RelayStore) UnbindProject(ctx context.Context, path, clientID string) error {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM project_subscriptions WHERE project = ? AND client_id = ?", path, clientID,
	)
	if err != nil {
		return fmt.Errorf("UnbindProject %q: %w", path, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("UnbindProject %q: %w", path, ErrNotFound)
	}
	return nil
}

// DeleteProject is an administrative removal. The project foreign key
// cascades to subscriptions and retained webhooks.
func (s *RelayStore) DeleteProject(ctx context.Context, path string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM projects WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("DeleteProject %q: %w", path, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("DeleteProject %q: %w", path, ErrNotFound)
	}
	return nil
}

// Project looks up a project and all subscriber delivery cursors.
func (s *RelayStore) Project(ctx context.Context, path string) (*ProjectRow, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT path, created_at, next_seq,
		 (SELECT COUNT(*) FROM webhooks WHERE project = projects.path)
		 FROM projects WHERE path = ?`,
		path,
	)
	var p ProjectRow
	var created int64
	if err := row.Scan(&p.Path, &created, &p.NextSeq, &p.WebhookCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("Project %q: %w", path, ErrNotFound)
		}
		return nil, fmt.Errorf("Project %q: %w", path, err)
	}
	p.CreatedAt = time.Unix(created, 0).UTC()
	subs, err := s.SubscriptionsByProject(ctx, path)
	if err != nil {
		return nil, err
	}
	p.Subscriptions = subs
	setLegacyProjectProjection(&p)
	return &p, nil
}

// ProjectsByClient lists projects subscribed to by clientID.
func (s *RelayStore) ProjectsByClient(ctx context.Context, clientID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT project FROM project_subscriptions WHERE client_id = ? ORDER BY project",
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

// ProjectSubscription returns one client's membership and delivery cursor.
func (s *RelayStore) ProjectSubscription(ctx context.Context, path, clientID string) (*ProjectSubscriptionRow, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT project, client_id, created_at, start_seq, acked_seq
		 FROM project_subscriptions WHERE project = ? AND client_id = ?`, path, clientID,
	)
	var sub ProjectSubscriptionRow
	var created int64
	if err := row.Scan(&sub.Project, &sub.ClientID, &created, &sub.StartSeq, &sub.AckedSeq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("ProjectSubscription %q/%q: %w", path, clientID, ErrNotFound)
		}
		return nil, fmt.Errorf("ProjectSubscription %q/%q: %w", path, clientID, err)
	}
	sub.CreatedAt = time.Unix(created, 0).UTC()
	return &sub, nil
}

// SubscriptionsByProject lists every subscriber in stable client-id order.
func (s *RelayStore) SubscriptionsByProject(ctx context.Context, path string) ([]ProjectSubscriptionRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT project, client_id, created_at, start_seq, acked_seq
		 FROM project_subscriptions WHERE project = ? ORDER BY client_id`, path,
	)
	if err != nil {
		return nil, fmt.Errorf("SubscriptionsByProject %q: %w", path, err)
	}
	defer rows.Close()
	var out []ProjectSubscriptionRow
	for rows.Next() {
		var sub ProjectSubscriptionRow
		var created int64
		if err := rows.Scan(&sub.Project, &sub.ClientID, &created, &sub.StartSeq, &sub.AckedSeq); err != nil {
			return nil, fmt.Errorf("SubscriptionsByProject %q scan: %w", path, err)
		}
		sub.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SubscriptionsByProject %q rows: %w", path, err)
	}
	return out, nil
}

// ReclaimProject replaces every subscription with one client. It remains for
// compatibility with older admin clients; new management surfaces add and
// remove subscribers explicitly.
func (s *RelayStore) ReclaimProject(ctx context.Context, path, newClientID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ReclaimProject %q begin: %w", path, err)
	}
	defer tx.Rollback()
	var start int64
	if err := tx.QueryRowContext(ctx, "SELECT next_seq - 1 FROM projects WHERE path = ?", path).Scan(&start); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("ReclaimProject %q: %w", path, ErrNotFound)
		}
		return fmt.Errorf("ReclaimProject %q: %w", path, err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM project_subscriptions WHERE project = ?", path); err != nil {
		return fmt.Errorf("ReclaimProject %q clear: %w", path, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO project_subscriptions (project, client_id, created_at, start_seq, acked_seq)
		 VALUES (?, ?, ?, ?, ?)`, path, newClientID, now.Unix(), start, start,
	); err != nil {
		return fmt.Errorf("ReclaimProject %q subscribe: %w", path, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ReclaimProject %q commit: %w", path, err)
	}
	return nil
}

// DeleteClient removes a client and its subscriptions. Projects and webhook
// history remain independent resources.
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

// ListProjects returns every project with its subscriptions and webhook count.
func (s *RelayStore) ListProjects(ctx context.Context) ([]ProjectRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT path, created_at, next_seq,
		 (SELECT COUNT(*) FROM webhooks WHERE project = projects.path)
		 FROM projects ORDER BY path`,
	)
	if err != nil {
		return nil, fmt.Errorf("ListProjects: %w", err)
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		var p ProjectRow
		var created int64
		if err := rows.Scan(&p.Path, &created, &p.NextSeq, &p.WebhookCount); err != nil {
			return nil, fmt.Errorf("ListProjects scan: %w", err)
		}
		p.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListProjects rows: %w", err)
	}
	for i := range out {
		subs, err := s.SubscriptionsByProject(ctx, out[i].Path)
		if err != nil {
			return nil, err
		}
		out[i].Subscriptions = subs
		setLegacyProjectProjection(&out[i])
	}
	return out, nil
}

func setLegacyProjectProjection(project *ProjectRow) {
	if len(project.Subscriptions) != 1 {
		return
	}
	project.ClientID = project.Subscriptions[0].ClientID
	project.AckedSeq = project.Subscriptions[0].AckedSeq
}

// AckedSeq returns the first subscriber's cursor for compatibility. New code
// should use AckedSeqForClient.
func (s *RelayStore) AckedSeq(ctx context.Context, project string) (int64, error) {
	subs, err := s.SubscriptionsByProject(ctx, project)
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, fmt.Errorf("AckedSeq %q: %w", project, ErrNotFound)
	}
	return subs[0].AckedSeq, nil
}

// AckedSeqForClient returns one subscriber's acknowledgement cursor.
func (s *RelayStore) AckedSeqForClient(ctx context.Context, project, clientID string) (int64, error) {
	sub, err := s.ProjectSubscription(ctx, project, clientID)
	if err != nil {
		return 0, err
	}
	return sub.AckedSeq, nil
}

// NextWebhookSeq reserves the next sequence number for a project atomically.
//
// Allocation reads projects.next_seq and bumps it under a transaction so
// concurrent ingress cannot share a seq. This is deliberately decoupled from
// each subscription's acked_seq delivery cursor. Conflating them would make
// the relay think every freshly-ingressed webhook was already acknowledged.
//
// The read-then-write shape requires BEGIN IMMEDIATE, which Open configures
// through the driver's _txlock parameter. A deferred transaction that reads and
// then upgrades to a write returns SQLITE_BUSY without consulting the busy
// handler once its snapshot goes stale, so busy_timeout alone cannot save it.
func (s *RelayStore) NextWebhookSeq(ctx context.Context, project string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("NextWebhookSeq %q begin: %w", project, err)
	}
	defer tx.Rollback()

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
		`INSERT INTO webhooks (project, seq, received_at, source_ip, method, path, headers, raw_headers, body)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.Project, w.Seq, w.ReceivedAt.Unix(), w.SourceIP, w.Method, w.Path, w.HeadersJSON, w.RawHeaders, w.Body,
	)
	return wrapExec(err, "InsertWebhook", fmt.Sprintf("%s/%d", w.Project, w.Seq))
}

// WebhooksAfter returns all retained webhooks for project with seq > afterSeq,
// in ascending seq order. This is the compatibility, unbounded sweep query.
func (s *RelayStore) WebhooksAfter(ctx context.Context, project string, afterSeq int64) ([]WebhookRow, error) {
	return s.webhooksAfter(ctx, project, afterSeq, -1)
}

// WebhooksAfterLimit is the bounded variant used by delivery pumps so a large
// retained backlog is not materialized in memory in one query.
func (s *RelayStore) WebhooksAfterLimit(ctx context.Context, project string, afterSeq, limit int64) ([]WebhookRow, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("WebhooksAfterLimit %q: limit must be positive", project)
	}
	return s.webhooksAfter(ctx, project, afterSeq, limit)
}

func (s *RelayStore) webhooksAfter(ctx context.Context, project string, afterSeq, limit int64) ([]WebhookRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT project, seq, received_at, COALESCE(source_ip, ''), method, COALESCE(path, ''), headers, COALESCE(raw_headers, ''), body
			 FROM webhooks
			 WHERE project = ? AND seq > ?
			 ORDER BY seq ASC
			 LIMIT ?`,
		project, afterSeq, limit,
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
// is the highest seq returned). When fewer rows than limit are returned,
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
		project, afterSeq, limit+1,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("ListWebhooks %q: %w", project, err)
	}
	defer rows.Close()
	var out []WebhookRow
	for rows.Next() {
		var w WebhookRow
		var received int64
		var rawHeaders []byte
		if err := rows.Scan(&w.Project, &w.Seq, &received, &w.SourceIP, &w.Method, &w.Path, &w.HeadersJSON, &rawHeaders, &w.Body); err != nil {
			return nil, 0, fmt.Errorf("ListWebhooks %q scan: %w", project, err)
		}
		w.RawHeaders = rawHeaders
		w.ReceivedAt = time.Unix(received, 0).UTC()
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("ListWebhooks %q rows: %w", project, err)
	}
	if int64(len(out)) <= limit {
		// Less than a full page means end of data.
		return out, 0, nil
	}
	out = out[:limit]
	return out, out[len(out)-1].Seq, nil
}

// ListWebhookSummaries is the bounded relay-management listing. It reads body
// length in SQLite and never materializes body or raw-header blobs.
func (s *RelayStore) ListWebhookSummaries(ctx context.Context, project string, afterSeq, limit int64) ([]WebhookRow, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT project, seq, received_at, COALESCE(source_ip, ''), method,
		 COALESCE(path, ''), headers, length(COALESCE(body, X''))
		 FROM webhooks WHERE project = ? AND seq > ? ORDER BY seq ASC LIMIT ?`,
		project, afterSeq, limit+1,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("ListWebhookSummaries %q: %w", project, err)
	}
	defer rows.Close()
	var out []WebhookRow
	for rows.Next() {
		var row WebhookRow
		var received int64
		if err := rows.Scan(&row.Project, &row.Seq, &received, &row.SourceIP, &row.Method, &row.Path, &row.HeadersJSON, &row.BodyLength); err != nil {
			return nil, 0, fmt.Errorf("ListWebhookSummaries %q scan: %w", project, err)
		}
		row.ReceivedAt = time.Unix(received, 0).UTC()
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("ListWebhookSummaries %q rows: %w", project, err)
	}
	if int64(len(out)) <= limit {
		return out, 0, nil
	}
	out = out[:limit]
	return out, out[len(out)-1].Seq, nil
}

// WebhookBySeq returns a specific webhook by (project, seq). Used by the
// replay route to re-push a retained webhook down the tunnel.
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

// MarkDelivered advances the first subscriber for compatibility with the
// original one-owner store API.
func (s *RelayStore) MarkDelivered(ctx context.Context, project string, upToSeq int64, now time.Time) error {
	subs, err := s.SubscriptionsByProject(ctx, project)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return fmt.Errorf("MarkDelivered %q: %w", project, ErrNotFound)
	}
	return s.MarkDeliveredForClient(ctx, project, subs[0].ClientID, upToSeq, now)
}

// MarkDeliveredForClient advances one subscriber's cursor monotonically.
// Delivery state is never global: another client may still be waiting for the
// same row.
func (s *RelayStore) MarkDeliveredForClient(ctx context.Context, project, clientID string, upToSeq int64, _ time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE project_subscriptions
		 SET acked_seq = MIN(?, (SELECT next_seq - 1 FROM projects WHERE path = ?))
		 WHERE project = ? AND client_id = ?
		   AND MIN(?, (SELECT next_seq - 1 FROM projects WHERE path = ?)) > acked_seq`,
		upToSeq, project, project, clientID, upToSeq, project,
	)
	if err != nil {
		return fmt.Errorf("MarkDelivered %q/%q: %w", project, clientID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		if _, err := s.ProjectSubscription(ctx, project, clientID); err != nil {
			return err
		}
	}
	return nil
}

// PendingCount returns rows still outstanding for at least one subscriber.
func (s *RelayStore) PendingCount(ctx context.Context, project string) (int64, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM webhooks w
		 WHERE w.project = ? AND EXISTS (
		   SELECT 1 FROM project_subscriptions s
		   WHERE s.project = w.project AND w.seq > s.start_seq AND w.seq > s.acked_seq
		 )`, project,
	)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("PendingCount %q: %w", project, err)
	}
	return n, nil
}

// PendingCountForClient returns one subscription's outstanding row count.
func (s *RelayStore) PendingCountForClient(ctx context.Context, project, clientID string) (int64, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM webhooks w
		 JOIN project_subscriptions s ON s.project = w.project
		 WHERE w.project = ? AND s.client_id = ?
		   AND w.seq > s.start_seq AND w.seq > s.acked_seq`, project, clientID,
	)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("PendingCountForClient %q/%q: %w", project, clientID, err)
	}
	return n, nil
}

// VacuumDelivered removes old rows only after every active subscription that
// was eligible to receive them has acknowledged them. Projects without
// subscribers do not pin history forever.
func (s *RelayStore) VacuumDelivered(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM webhooks
		 WHERE received_at < ? AND NOT EXISTS (
		   SELECT 1 FROM project_subscriptions s
		   WHERE s.project = webhooks.project
		     AND webhooks.seq > s.start_seq
		     AND s.acked_seq < webhooks.seq
		 )`,
		olderThan.Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("VacuumDelivered: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ClientByProject returns a stable subscribed client for compatibility with
// older callers. New routing code uses SubscriptionsByProject.
func (s *RelayStore) ClientByProject(ctx context.Context, project string) (string, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT client_id FROM project_subscriptions WHERE project = ? ORDER BY client_id LIMIT 1", project,
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

// DeleteWebhook removes one stored relay webhook.
func (s *RelayStore) DeleteWebhook(ctx context.Context, project string, seq int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM webhooks WHERE project = ? AND seq = ?", project, seq)
	if err != nil {
		return fmt.Errorf("DeleteWebhook %s/%d: %w", project, seq, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("DeleteWebhook %s/%d: %w", project, seq, ErrNotFound)
	}
	return nil
}

// DeleteWebhooks deletes selected sequence numbers atomically.
func (s *RelayStore) DeleteWebhooks(ctx context.Context, project string, seqs []int64) (int64, error) {
	if len(seqs) == 0 {
		return 0, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(seqs)), ",")
	args := make([]any, 0, len(seqs)+1)
	args = append(args, project)
	for _, seq := range seqs {
		args = append(args, seq)
	}
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM webhooks WHERE project = ? AND seq IN ("+placeholders+")", args...,
	)
	if err != nil {
		return 0, fmt.Errorf("DeleteWebhooks %q: %w", project, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteWebhooksThrough removes all rows at or below a sequence number.
func (s *RelayStore) DeleteWebhooksThrough(ctx context.Context, project string, seq int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM webhooks WHERE project = ? AND seq <= ?", project, seq)
	if err != nil {
		return 0, fmt.Errorf("DeleteWebhooksThrough %q: %w", project, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteAllWebhooks clears a project's relay-side history without deleting
// the project or any subscriptions.
func (s *RelayStore) DeleteAllWebhooks(ctx context.Context, project string) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM webhooks WHERE project = ?", project)
	if err != nil {
		return 0, fmt.Errorf("DeleteAllWebhooks %q: %w", project, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
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
