package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// searchBodyPrefix bounds how much of a body is matched when body search is
// enabled. Without it a single 10 MiB payload would dominate every scan.
const searchBodyPrefix = 64 * 1024

// defaultSearchLimit is the page size used when a filter leaves Limit unset.
// It matches the GUI's list cap.
const defaultSearchLimit = 100

// CaptureFilter narrows a traffic-capture query. The zero value reproduces the
// plain "newest first" listing: no predicate, capped at defaultSearchLimit rows.
//
// Filtering happens in SQLite rather than in the caller because a caller only
// ever holds the newest page of rows. Matching in Go would silently restrict
// search to whatever had already been fetched, so a capture older than that page
// could never be found no matter how exactly it matched.
type CaptureFilter struct {
	// SessionID restricts the query to one interception session; 0 means all.
	SessionID int64
	// Query is matched as a case-insensitive substring against method, URL and
	// status. With Body set it also matches request and response body content.
	Query string
	// Method is an exact HTTP method, or "" for any.
	Method string
	// Status is "" for any, a family such as "4xx", or an exact code such as "404".
	Status string
	// Body includes request and response bodies in Query matching. Slower by
	// nature: no index can serve a substring match, so every row's body has to
	// be read.
	Body bool
	// BeforeID pages backwards: only rows with id < BeforeID are returned.
	BeforeID int64
	// Limit caps the page; 0 means defaultSearchLimit.
	Limit int
}

// CapturePage is a filtered page of full capture rows, body payloads included.
type CapturePage struct {
	Rows    []TrafficCaptureRow
	Total   int64
	HasMore bool
}

// CaptureSummaryPage is a filtered page of capture metadata. It never selects
// body columns, so scanning stays off the payloads.
type CaptureSummaryPage struct {
	Rows    []TrafficCaptureSummaryRow
	Total   int64
	HasMore bool
}

// WebhookFilter narrows a webhook query; the counterpart of CaptureFilter.
type WebhookFilter struct {
	// Project restricts the query to one project; "" means all projects.
	Project string
	// Query is matched as a case-insensitive substring against project, method,
	// path and source IP.
	Query string
	// Method is an exact HTTP method, or "" for any.
	Method string
	// BeforeSeq pages backwards: only rows with seq < BeforeSeq are returned.
	BeforeSeq int64
	// Limit caps the page; 0 means defaultSearchLimit.
	Limit int
}

// WebhookPage is a filtered page of webhooks.
type WebhookPage struct {
	Rows    []WebhookRow
	Total   int64
	HasMore bool
}

// whereClause accumulates " AND "-joined conditions alongside their arguments.
// Both slices are appended in lockstep by add and containsAny so positional
// parameters always line up with the placeholders.
type whereClause struct {
	conds []string
	args  []any
}

func (w *whereClause) add(cond string, args ...any) {
	w.conds = append(w.conds, cond)
	w.args = append(w.args, args...)
}

// sql renders the clause, or "" when no condition was added.
func (w *whereClause) sql() string {
	if len(w.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.conds, " AND ")
}

// containsAny matches needle against every expression as a case-insensitive
// substring, OR-ing the results. Each expression must already be lowered.
//
// instr() is used rather than LIKE so a query containing "%" or "_" is matched
// literally instead of being read as a wildcard pattern.
func (w *whereClause) containsAny(needle string, exprs ...string) {
	terms := make([]string, 0, len(exprs))
	args := make([]any, 0, len(exprs))
	for _, e := range exprs {
		terms = append(terms, "instr("+e+", ?) > 0")
		args = append(args, needle)
	}
	w.add("("+strings.Join(terms, " OR ")+")", args...)
}

// bodyExpr is the lowered, prefix-bounded text of a body column. The bound keeps
// one oversized payload from dominating a scan; bodies are capped at 10 MiB on
// ingest, which is far more than a substring search needs to read.
func bodyExpr(column string) string {
	return "lower(substr(CAST(COALESCE(" + column + ", '') AS TEXT), 1, " +
		strconv.Itoa(searchBodyPrefix) + "))"
}

// statusRange turns a status lens into a half-open range: "4xx" is 400..500 and
// "404" is 404..405. It reports false when the value is neither, letting the
// caller reject a mistyped filter rather than silently ignoring it.
func statusRange(status string) (lo, hi int, ok bool) {
	s := strings.ToLower(strings.TrimSpace(status))
	if len(s) == 3 && s[1] == 'x' && s[2] == 'x' {
		if fam := int(s[0] - '0'); fam >= 1 && fam <= 5 {
			return fam * 100, fam*100 + 100, true
		}
		return 0, 0, false
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 100 && n <= 599 {
		return n, n + 1, true
	}
	return 0, 0, false
}

// validateStatus rejects a status lens the SQL cannot express, so a typo
// surfaces as an error instead of an unfiltered or empty result.
func validateStatus(status string) error {
	if strings.TrimSpace(status) == "" {
		return nil
	}
	if _, _, ok := statusRange(status); !ok {
		return fmt.Errorf("invalid status filter %q: want a family like \"4xx\" or a code like \"404\"", status)
	}
	return nil
}

// pageLimit resolves the requested page size.
func pageLimit(n int) int {
	if n <= 0 {
		return defaultSearchLimit
	}
	return n
}

// countRows counts a filtered set. The predicate is built by the same helpers as
// the row queries, so the count and the rows can never disagree.
func (s *PCStore) countRows(ctx context.Context, table string, w whereClause, op string) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+w.sql(), w.args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("PCStore.%s count: %w", op, err)
	}
	return n, nil
}

// captureWhere builds the predicate for a capture query. withCursor includes the
// paging cursor; it is excluded when counting, because Total describes the whole
// filtered set rather than the remainder after the cursor.
func captureWhere(f CaptureFilter, withCursor bool) whereClause {
	var w whereClause
	if f.SessionID != 0 {
		w.add("session_id = ?", f.SessionID)
	}
	if m := strings.ToUpper(strings.TrimSpace(f.Method)); m != "" {
		w.add("upper(COALESCE(method, '')) = ?", m)
	}
	if lo, hi, ok := statusRange(f.Status); ok {
		w.add("status >= ? AND status < ?", lo, hi)
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		exprs := []string{
			"lower(COALESCE(method, ''))",
			"lower(COALESCE(url, ''))",
			"lower(COALESCE(CAST(status AS TEXT), ''))",
		}
		if f.Body {
			exprs = append(exprs, bodyExpr("req_body"), bodyExpr("resp_body"))
		}
		w.containsAny(q, exprs...)
	}
	if withCursor && f.BeforeID > 0 {
		w.add("id < ?", f.BeforeID)
	}
	return w
}

// webhookWhere is captureWhere's counterpart.
func webhookWhere(f WebhookFilter, withCursor bool) whereClause {
	var w whereClause
	if f.Project != "" {
		w.add("project = ?", f.Project)
	}
	if m := strings.ToUpper(strings.TrimSpace(f.Method)); m != "" {
		w.add("upper(COALESCE(method, '')) = ?", m)
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		w.containsAny(q,
			"lower(project)",
			"lower(COALESCE(method, ''))",
			"lower(COALESCE(path, ''))",
			"lower(COALESCE(source_ip, ''))",
		)
	}
	if withCursor && f.BeforeSeq > 0 {
		w.add("seq < ?", f.BeforeSeq)
	}
	return w
}

// applyPage trims an over-fetched result to the page size and reports whether
// more rows remain. Every search fetches limit+1 rows for exactly this reason.
func applyPage(rows, limit int) (kept int, hasMore bool) {
	if rows > limit {
		return limit, true
	}
	return rows, false
}

// SearchCaptureSummaries returns a filtered page of capture metadata without
// selecting body payloads. It backs the GUI list and the local control API;
// because the predicate runs in SQLite, a match older than any page the caller
// has already fetched is still found.
func (s *PCStore) SearchCaptureSummaries(ctx context.Context, f CaptureFilter) (CaptureSummaryPage, error) {
	if err := validateStatus(f.Status); err != nil {
		return CaptureSummaryPage{}, fmt.Errorf("PCStore.SearchCaptureSummaries: %w", err)
	}
	limit := pageLimit(f.Limit)
	w := captureWhere(f, true)
	q := `SELECT id, COALESCE(session_id, 0), at, COALESCE(method, ''), COALESCE(url, ''), status,
		 COALESCE(length(req_body), 0), COALESCE(length(resp_body), 0), COUNT(*) OVER ()
		 FROM traffic_captures` + w.sql() + ` ORDER BY id DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, append(w.args, limit+1)...)
	if err != nil {
		return CaptureSummaryPage{}, fmt.Errorf("PCStore.SearchCaptureSummaries: %w", err)
	}
	defer rows.Close()

	page := CaptureSummaryPage{Rows: make([]TrafficCaptureSummaryRow, 0, limit)}
	for rows.Next() {
		var r TrafficCaptureSummaryRow
		var at, total int64
		var status sql.NullInt64
		if err := rows.Scan(&r.ID, &r.SessionID, &at, &r.Method, &r.URL, &status, &r.ReqBodyLen, &r.RespBodyLen, &total); err != nil {
			return CaptureSummaryPage{}, fmt.Errorf("PCStore.SearchCaptureSummaries scan: %w", err)
		}
		r.At = time.Unix(at, 0).UTC()
		if status.Valid {
			r.Status = int(status.Int64)
		}
		// COUNT(*) OVER () is evaluated before LIMIT, so on an unpaged query every
		// row already carries the size of the whole filtered set.
		page.Total = total
		page.Rows = append(page.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return CaptureSummaryPage{}, fmt.Errorf("PCStore.SearchCaptureSummaries rows: %w", err)
	}
	kept, hasMore := applyPage(len(page.Rows), limit)
	page.Rows = page.Rows[:kept]
	page.HasMore = hasMore
	if f.BeforeID > 0 {
		// The cursor is part of the predicate, so the window value above counts
		// only what is left after it. Recount without the cursor so Total stays
		// the size of the whole filtered set across pages.
		if page.Total, err = s.countRows(ctx, "traffic_captures", captureWhere(f, false), "SearchCaptureSummaries"); err != nil {
			return CaptureSummaryPage{}, err
		}
	}
	return page, nil
}

// SearchCaptures returns a filtered page of full capture rows, bodies included.
// The TUI needs these rather than summaries because its detail pane renders
// straight from the row without a refetch.
func (s *PCStore) SearchCaptures(ctx context.Context, f CaptureFilter) (CapturePage, error) {
	if err := validateStatus(f.Status); err != nil {
		return CapturePage{}, fmt.Errorf("PCStore.SearchCaptures: %w", err)
	}
	limit := pageLimit(f.Limit)
	w := captureWhere(f, true)
	q := `SELECT id, COALESCE(session_id, 0), at, COALESCE(method, ''), COALESCE(url, ''),
		 COALESCE(req_headers, ''), COALESCE(req_body, ''), status, COALESCE(resp_headers, ''), COALESCE(resp_body, ''), COUNT(*) OVER ()
		 FROM traffic_captures` + w.sql() + ` ORDER BY id DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, append(w.args, limit+1)...)
	if err != nil {
		return CapturePage{}, fmt.Errorf("PCStore.SearchCaptures: %w", err)
	}
	defer rows.Close()

	page := CapturePage{Rows: make([]TrafficCaptureRow, 0, limit)}
	for rows.Next() {
		var c TrafficCaptureRow
		var at, total int64
		var status sql.NullInt64
		if err := rows.Scan(&c.ID, &c.SessionID, &at, &c.Method, &c.URL, &c.ReqHeadersJSON, &c.ReqBody, &status, &c.RespHeadersJSON, &c.RespBody, &total); err != nil {
			return CapturePage{}, fmt.Errorf("PCStore.SearchCaptures scan: %w", err)
		}
		c.At = time.Unix(at, 0).UTC()
		if status.Valid {
			c.Status = int(status.Int64)
		}
		page.Total = total
		page.Rows = append(page.Rows, c)
	}
	if err := rows.Err(); err != nil {
		return CapturePage{}, fmt.Errorf("PCStore.SearchCaptures rows: %w", err)
	}
	kept, hasMore := applyPage(len(page.Rows), limit)
	page.Rows = page.Rows[:kept]
	page.HasMore = hasMore
	if f.BeforeID > 0 {
		if page.Total, err = s.countRows(ctx, "traffic_captures", captureWhere(f, false), "SearchCaptures"); err != nil {
			return CapturePage{}, err
		}
	}
	return page, nil
}

// SearchWebhooks returns a filtered page of webhooks, newest first. Like the
// capture search it filters in SQLite, so matches beyond the newest page are
// reachable.
func (s *PCStore) SearchWebhooks(ctx context.Context, f WebhookFilter) (WebhookPage, error) {
	limit := pageLimit(f.Limit)
	w := webhookWhere(f, true)
	q := `SELECT project, seq, received_at, COALESCE(source_ip, ''), method, COALESCE(path, ''),
		 headers, COALESCE(raw_headers, ''), body, COUNT(*) OVER ()
		 FROM webhooks` + w.sql() + ` ORDER BY seq DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, append(w.args, limit+1)...)
	if err != nil {
		return WebhookPage{}, fmt.Errorf("PCStore.SearchWebhooks: %w", err)
	}
	defer rows.Close()

	page := WebhookPage{Rows: make([]WebhookRow, 0, limit)}
	for rows.Next() {
		var (
			r          WebhookRow
			received   int64
			rawHeaders []byte
			total      int64
		)
		if err := rows.Scan(&r.Project, &r.Seq, &received, &r.SourceIP, &r.Method, &r.Path, &r.HeadersJSON, &rawHeaders, &r.Body, &total); err != nil {
			return WebhookPage{}, fmt.Errorf("PCStore.SearchWebhooks scan: %w", err)
		}
		r.RawHeaders = rawHeaders
		r.ReceivedAt = time.Unix(received, 0).UTC()
		page.Total = total
		page.Rows = append(page.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return WebhookPage{}, fmt.Errorf("PCStore.SearchWebhooks rows: %w", err)
	}
	kept, hasMore := applyPage(len(page.Rows), limit)
	page.Rows = page.Rows[:kept]
	page.HasMore = hasMore
	if f.BeforeSeq > 0 {
		if page.Total, err = s.countRows(ctx, "webhooks", webhookWhere(f, false), "SearchWebhooks"); err != nil {
			return WebhookPage{}, err
		}
	}
	return page, nil
}
