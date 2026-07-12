package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PCStore is the local storage layer used by the wiretap app on a user's
// machine. It caches webhooks pushed by the relay, holds traffic captures
// from the MITM proxy, and keeps the authoritative per-project cursor sent
// in HELLO on tunnel reconnect. Like RelayStore, it owns only a *sql.DB
// handle and stays free of wire-protocol imports.
type PCStore struct {
	db *sql.DB
}

// NewPCStore wraps an existing *sql.DB. The caller is expected to have run
// MigratePC first.
func NewPCStore(db *sql.DB) *PCStore {
	return &PCStore{db: db}
}

// DB exposes the underlying handle for callers that need ad-hoc
// transactions. Prefer adding a method to PCStore.
func (s *PCStore) DB() *sql.DB { return s.db }

// StoreWebhook inserts a webhook received over the tunnel. It is idempotent
// on (project, seq): re-pushes after a reconnect are ignored, which is the
// whole point of the ack cursor pattern. Returns true if a new row was
// actually inserted; false on ignored duplicates.
func (s *PCStore) StoreWebhook(ctx context.Context, w WebhookRow, storedAt time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO webhooks
			 (project, seq, received_at, stored_at, source_ip, method, path, headers, raw_headers, body)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.Project, w.Seq, w.ReceivedAt.Unix(), storedAt.Unix(), w.SourceIP, w.Method, w.Path, w.HeadersJSON, w.RawHeaders, w.Body,
	)
	if err != nil {
		return false, fmt.Errorf("PCStore.StoreWebhook %s/%d: %w", w.Project, w.Seq, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// LastSeq returns the highest persisted seq for a project, or 0 if none.
// This value is sent in HELLO on tunnel connect and is the authoritative
// cursor; the relay resumes pushing from this point.
func (s *PCStore) LastSeq(ctx context.Context, project string) (int64, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(seq), 0) FROM webhooks WHERE project = ?", project,
	)
	var seq int64
	if err := row.Scan(&seq); err != nil {
		return 0, fmt.Errorf("PCStore.LastSeq %q: %w", project, err)
	}
	return seq, nil
}

// Webhooks lists the most recent `limit` webhooks for `project` (or all
// projects when project is empty), newest-first. Used by the TUI/GUI.
func (s *PCStore) Webhooks(ctx context.Context, project string, limit int) ([]WebhookRow, error) {
	q := `SELECT project, seq, received_at, COALESCE(source_ip, ''), method, COALESCE(path, ''), headers, COALESCE(raw_headers, ''), body
		      FROM webhooks`
	args := []any{}
	if project != "" {
		q += " WHERE project = ?"
		args = append(args, project)
	}
	if limit > 0 {
		q += " ORDER BY seq DESC LIMIT ?"
		args = append(args, limit)
	} else {
		q += " ORDER BY seq DESC"
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("PCStore.Webhooks %q: %w", project, err)
	}
	defer rows.Close()
	var out []WebhookRow
	for rows.Next() {
		var w WebhookRow
		var received int64
		var rawHeaders []byte
		if err := rows.Scan(&w.Project, &w.Seq, &received, &w.SourceIP, &w.Method, &w.Path, &w.HeadersJSON, &rawHeaders, &w.Body); err != nil {
			return nil, fmt.Errorf("PCStore.Webhooks scan: %w", err)
		}
		w.RawHeaders = rawHeaders
		w.ReceivedAt = time.Unix(received, 0).UTC()
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("PCStore.Webhooks rows: %w", err)
	}
	return out, nil
}

// WebhookBySeq returns a specific webhook by (project, seq). Useful for the
// replay feature: load one row and re-POST it to a target URL.
func (s *PCStore) WebhookBySeq(ctx context.Context, project string, seq int64) (*WebhookRow, error) {
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
			return nil, fmt.Errorf("PCStore.WebhookBySeq %s/%d: %w", project, seq, ErrNotFound)
		}
		return nil, fmt.Errorf("PCStore.WebhookBySeq %s/%d: %w", project, seq, err)
	}
	w.RawHeaders = rawHeaders
	w.ReceivedAt = time.Unix(received, 0).UTC()
	return &w, nil
}

// InsertTrafficCapture appends a request/response pair. Returns the row id.
func (s *PCStore) InsertTrafficCapture(ctx context.Context, c TrafficCaptureRow) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO traffic_captures (at, method, url, req_headers, req_body, status, resp_headers, resp_body)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.At.Unix(), c.Method, c.URL, c.ReqHeadersJSON, c.ReqBody, c.Status, c.RespHeadersJSON, c.RespBody,
	)
	if err != nil {
		return 0, fmt.Errorf("PCStore.InsertTrafficCapture: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("PCStore.InsertTrafficCapture last id: %w", err)
	}
	return id, nil
}

// TrafficCaptures lists the most recent traffic captures, newest-first.
func (s *PCStore) TrafficCaptures(ctx context.Context, limit int) ([]TrafficCaptureRow, error) {
	q := `SELECT id, at, COALESCE(method, ''), COALESCE(url, ''), COALESCE(req_headers, ''), COALESCE(req_body, ''), status, COALESCE(resp_headers, ''), COALESCE(resp_body, '') FROM traffic_captures`
	args := []any{}
	if limit > 0 {
		q += " ORDER BY id DESC LIMIT ?"
		args = append(args, limit)
	} else {
		q += " ORDER BY id DESC"
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("PCStore.TrafficCaptures: %w", err)
	}
	defer rows.Close()
	var out []TrafficCaptureRow
	for rows.Next() {
		var c TrafficCaptureRow
		var at int64
		var status sql.NullInt64
		if err := rows.Scan(&c.ID, &at, &c.Method, &c.URL, &c.ReqHeadersJSON, &c.ReqBody, &status, &c.RespHeadersJSON, &c.RespBody); err != nil {
			return nil, fmt.Errorf("PCStore.TrafficCaptures scan: %w", err)
		}
		c.At = time.Unix(at, 0).UTC()
		if status.Valid {
			c.Status = int(status.Int64)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("PCStore.TrafficCaptures rows: %w", err)
	}
	return out, nil
}
