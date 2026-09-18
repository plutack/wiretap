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
// from the interception proxy, and keeps the authoritative per-project
// cursor sent in HELLO on tunnel reconnect. Like RelayStore, it owns only a
// *sql.DB handle and stays free of wire-protocol imports.
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
// A zero SessionID is stored as NULL so "no session" rows look the same as
// pre-session history.
func (s *PCStore) InsertTrafficCapture(ctx context.Context, c TrafficCaptureRow) (int64, error) {
	var sessionID any
	if c.SessionID != 0 {
		sessionID = c.SessionID
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO traffic_captures (session_id, at, method, url, req_headers, req_body, status, resp_headers, resp_body)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, c.At.Unix(), c.Method, c.URL, c.ReqHeadersJSON, c.ReqBody, c.Status, c.RespHeadersJSON, c.RespBody,
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

// TrafficCapturePreviewByID returns capture metadata and at most bodyLimit
// bytes from each body. bodyLimit must be positive.
func (s *PCStore) TrafficCapturePreviewByID(ctx context.Context, id int64, bodyLimit int) (*TrafficCapturePreviewRow, error) {
	if bodyLimit <= 0 {
		return nil, fmt.Errorf("PCStore.TrafficCapturePreviewByID: body limit must be positive")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(session_id, 0), at, COALESCE(method, ''), COALESCE(url, ''), COALESCE(req_headers, ''), substr(COALESCE(req_body, ''), 1, ?), COALESCE(length(req_body), 0), status, COALESCE(resp_headers, ''), substr(COALESCE(resp_body, ''), 1, ?), COALESCE(length(resp_body), 0)
		 FROM traffic_captures WHERE id = ?`,
		bodyLimit, bodyLimit, id,
	)
	var c TrafficCapturePreviewRow
	var at int64
	var status sql.NullInt64
	if err := row.Scan(&c.ID, &c.SessionID, &at, &c.Method, &c.URL, &c.ReqHeadersJSON, &c.ReqBody, &c.ReqBodyLen, &status, &c.RespHeadersJSON, &c.RespBody, &c.RespBodyLen); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("PCStore.TrafficCapturePreviewByID %d: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("PCStore.TrafficCapturePreviewByID %d: %w", id, err)
	}
	c.At = time.Unix(at, 0).UTC()
	if status.Valid {
		c.Status = int(status.Int64)
	}
	return &c, nil
}

// TrafficCaptureBody returns a bounded prefix of one body. A non-positive
// limit returns the full body and is reserved for explicit user actions.
func (s *PCStore) TrafficCaptureBody(ctx context.Context, id int64, response bool, limit int) ([]byte, int, error) {
	column := "req_body"
	if response {
		column = "resp_body"
	}
	expr := "COALESCE(" + column + ", '')"
	args := []any{}
	if limit > 0 {
		expr = "substr(" + expr + ", 1, ?)"
		args = append(args, limit)
	}
	args = append(args, id)
	row := s.db.QueryRowContext(ctx,
		"SELECT "+expr+", COALESCE(length("+column+"), 0) FROM traffic_captures WHERE id = ?",
		args...,
	)
	var body []byte
	var bodyLen int
	if err := row.Scan(&body, &bodyLen); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, fmt.Errorf("PCStore.TrafficCaptureBody %d: %w", id, ErrNotFound)
		}
		return nil, 0, fmt.Errorf("PCStore.TrafficCaptureBody %d: %w", id, err)
	}
	return body, bodyLen, nil
}

// TrafficCaptureByID returns a single traffic capture with its request/response
// headers and bodies populated (the detail view). Returns ErrNotFound when no
// row has that id.
func (s *PCStore) TrafficCaptureByID(ctx context.Context, id int64) (*TrafficCaptureRow, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(session_id, 0), at, COALESCE(method, ''), COALESCE(url, ''), COALESCE(req_headers, ''), COALESCE(req_body, ''), status, COALESCE(resp_headers, ''), COALESCE(resp_body, '')
		 FROM traffic_captures WHERE id = ?`,
		id,
	)
	var c TrafficCaptureRow
	var at int64
	var status sql.NullInt64
	if err := row.Scan(&c.ID, &c.SessionID, &at, &c.Method, &c.URL, &c.ReqHeadersJSON, &c.ReqBody, &status, &c.RespHeadersJSON, &c.RespBody); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("PCStore.TrafficCaptureByID %d: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("PCStore.TrafficCaptureByID %d: %w", id, err)
	}
	c.At = time.Unix(at, 0).UTC()
	if status.Valid {
		c.Status = int(status.Int64)
	}
	return &c, nil
}

// --- intercept sessions ---------------------------------------------------

// CreateInterceptSession inserts a new (running) session row and returns its
// id. EndedAt is left NULL until EndInterceptSession.
func (s *PCStore) CreateInterceptSession(ctx context.Context, startedAt time.Time, shell, proxyAddr string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO intercept_sessions (started_at, shell, proxy_addr) VALUES (?, ?, ?)`,
		startedAt.Unix(), shell, proxyAddr,
	)
	if err != nil {
		return 0, fmt.Errorf("PCStore.CreateInterceptSession: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("PCStore.CreateInterceptSession last id: %w", err)
	}
	return id, nil
}

// EndInterceptSession stamps ended_at on a session. Idempotent: re-ending an
// already-closed session simply overwrites the timestamp.
func (s *PCStore) EndInterceptSession(ctx context.Context, id int64, endedAt time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE intercept_sessions SET ended_at = ? WHERE id = ?`, endedAt.Unix(), id,
	); err != nil {
		return fmt.Errorf("PCStore.EndInterceptSession %d: %w", id, err)
	}
	return nil
}

// ReconcileInterceptSessions closes out sessions that never recorded an end
// time, which is what a crashed or killed `wiretap intercept start` leaves
// behind. Without this, such a row stays open forever and never shows a close
// time.
//
// activeSessionID identifies the session owned by a process that is still
// running (0 when none is); it is never touched. Sessions started within
// gracePeriod are skipped too, because `intercept start` inserts its row
// slightly before it publishes the PID file that identifies it as active, so a
// session begun moments ago may not be identifiable yet.
//
// ended_at is backfilled with the session's last captured request, falling back
// to started_at when the session captured nothing, and interrupted is set so
// the run stays distinguishable from a clean shutdown. Returns the number of
// sessions closed.
func (s *PCStore) ReconcileInterceptSessions(ctx context.Context, activeSessionID int64, now time.Time, gracePeriod time.Duration) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE intercept_sessions
		SET ended_at = COALESCE(
				(SELECT MAX(c.at) FROM traffic_captures c WHERE c.session_id = intercept_sessions.id),
				started_at
			),
			interrupted = 1
		WHERE ended_at IS NULL AND id != ? AND started_at <= ?`,
		activeSessionID, now.Add(-gracePeriod).Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("PCStore.ReconcileInterceptSessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// InterceptSessions lists sessions newest-first with their capture counts.
// limit <= 0 returns all.
func (s *PCStore) InterceptSessions(ctx context.Context, limit int) ([]InterceptSessionRow, error) {
	rows, _, err := s.InterceptSessionsPage(ctx, 0, limit)
	return rows, err
}

// InterceptSessionsPage lists sessions newest-first. beforeID == 0 starts at
// the newest row; otherwise only rows older than beforeID are returned. Total
// is the count across all sessions, independent of the page cursor.
func (s *PCStore) InterceptSessionsPage(ctx context.Context, beforeID int64, limit int) ([]InterceptSessionRow, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intercept_sessions`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("PCStore.InterceptSessionsPage count: %w", err)
	}

	q := `SELECT s.id, s.started_at, COALESCE(s.ended_at, 0), COALESCE(s.shell, ''), COALESCE(s.proxy_addr, ''),
		COALESCE(s.interrupted, 0),
		(SELECT COUNT(*) FROM traffic_captures c WHERE c.session_id = s.id)
		FROM intercept_sessions s`
	args := []any{}
	if beforeID > 0 {
		q += " WHERE s.id < ?"
		args = append(args, beforeID)
	}
	if limit > 0 {
		q += " ORDER BY s.id DESC LIMIT ?"
		args = append(args, limit)
	} else {
		q += " ORDER BY s.id DESC"
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("PCStore.InterceptSessionsPage: %w", err)
	}
	defer rows.Close()
	var out []InterceptSessionRow
	for rows.Next() {
		var r InterceptSessionRow
		var started, ended int64
		var interrupted int
		if err := rows.Scan(&r.ID, &started, &ended, &r.Shell, &r.ProxyAddr, &interrupted, &r.Captures); err != nil {
			return nil, 0, fmt.Errorf("PCStore.InterceptSessionsPage scan: %w", err)
		}
		r.StartedAt = time.Unix(started, 0).UTC()
		if ended != 0 {
			r.EndedAt = time.Unix(ended, 0).UTC()
		}
		r.Interrupted = interrupted != 0
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("PCStore.InterceptSessionsPage rows: %w", err)
	}
	return out, total, nil
}
