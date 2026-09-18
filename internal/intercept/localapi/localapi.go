// Package localapi is the 127.0.0.1 control HTTP API that wiretap exposes so
// external scripts and the GUI can query captured webhooks and intercepted
// traffic without reading SQLite directly.
//
// The handlers are thin and read-only: they translate a Querier (the local
// PCStore) into JSON. Mutations happen through the proxy (captures) and the
// relay tunnel (webhooks); this API only reports them.
package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/plutack/wiretap/internal/store"
)

// Querier is the read-side of the local store. PCStore satisfies it directly.
// Declared here (consumer-side) so the API stays decoupled from the store
// package internals and tests can substitute an in-memory fake.
type Querier interface {
	SearchWebhooks(ctx context.Context, f store.WebhookFilter) (store.WebhookPage, error)
	SearchCaptureSummaries(ctx context.Context, f store.CaptureFilter) (store.CaptureSummaryPage, error)
}

// Server answers the control API routes. Construct with New, serve Routes() in
// a goroutine.
type Server struct {
	q       Querier
	version string
}

// Option configures a Server.
type Option func(*Server)

// WithVersion stamps the version reported by /local/health.
func WithVersion(v string) Option { return func(s *Server) { s.version = v } }

// New builds a server. q is required.
func New(q Querier, opts ...Option) *Server {
	s := &Server{q: q, version: "dev"}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Routes builds the HTTP mux. All routes live under /local/ so they never
// collide with the relayd admin API surface, and so an operator can scope a
// reverse proxy or firewall to that prefix if they ever expose it.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /local/health", s.handleHealth)
	mux.HandleFunc("GET /local/webhooks", s.handleWebhooks)
	mux.HandleFunc("GET /local/captures", s.handleCaptures)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": s.version,
	})
}

// handleWebhooks lists webhooks the relay tunnel pushed to this PC, newest
// first. Optional ?project=p, ?q= (substring over project, method, path and
// source IP), ?method=M, and ?limit=N (default 100, capped 1000).
func (s *Server) handleWebhooks(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, err := s.q.SearchWebhooks(r.Context(), store.WebhookFilter{
		Project: query.Get("project"),
		Query:   query.Get("q"),
		Method:  query.Get("method"),
		Limit:   parseLimit(r, 100, 1000),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody("query webhooks: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, webhookListResponse{
		Webhooks: toWebhooks(page.Rows),
		Total:    page.Total,
		HasMore:  page.HasMore,
	})
}

// handleCaptures lists intercepted traffic captures, newest first. Optional
// ?q= (substring over method, URL and status), ?method=M, ?status=404|4xx, and
// ?limit=N (default 100, capped 1000).
func (s *Server) handleCaptures(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, err := s.q.SearchCaptureSummaries(r.Context(), store.CaptureFilter{
		Query:  query.Get("q"),
		Method: query.Get("method"),
		Status: query.Get("status"),
		Limit:  parseLimit(r, 100, 1000),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody("query captures: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, captureListResponse{
		Captures: toCaptures(page.Rows),
		Total:    page.Total,
		HasMore:  page.HasMore,
	})
}

// --- DTOs --------------------------------------------------------------

type webhookListResponse struct {
	Webhooks []webhookDTO `json:"webhooks"`
	// Total is the size of the whole filtered set, so a caller can tell a full
	// page apart from the end of the results.
	Total   int64 `json:"total"`
	HasMore bool  `json:"has_more"`
}

type webhookDTO struct {
	Project    string    `json:"project"`
	Seq        int64     `json:"seq"`
	ReceivedAt time.Time `json:"received_at"`
	SourceIP   string    `json:"source_ip,omitempty"`
	Method     string    `json:"method,omitempty"`
	Path       string    `json:"path,omitempty"`
	BodyLen    int       `json:"body_len"`
}

func toWebhooks(rows []store.WebhookRow) []webhookDTO {
	out := make([]webhookDTO, 0, len(rows))
	for _, w := range rows {
		out = append(out, webhookDTO{
			Project:    w.Project,
			Seq:        w.Seq,
			ReceivedAt: w.ReceivedAt,
			SourceIP:   w.SourceIP,
			Method:     w.Method,
			Path:       w.Path,
			BodyLen:    len(w.Body),
		})
	}
	return out
}

type captureListResponse struct {
	Captures []captureDTO `json:"captures"`
	// Total is the size of the whole filtered set, so a caller can tell a full
	// page apart from the end of the results.
	Total   int64 `json:"total"`
	HasMore bool  `json:"has_more"`
}

type captureDTO struct {
	ID          int64     `json:"id"`
	At          time.Time `json:"at"`
	Method      string    `json:"method,omitempty"`
	URL         string    `json:"url,omitempty"`
	ReqBodyLen  int       `json:"req_body_len"`
	Status      int       `json:"status,omitempty"`
	RespBodyLen int       `json:"resp_body_len"`
}

func toCaptures(rows []store.TrafficCaptureSummaryRow) []captureDTO {
	out := make([]captureDTO, 0, len(rows))
	for _, c := range rows {
		out = append(out, captureDTO{
			ID:          c.ID,
			At:          c.At,
			Method:      c.Method,
			URL:         c.URL,
			ReqBodyLen:  c.ReqBodyLen,
			Status:      c.Status,
			RespBodyLen: c.RespBodyLen,
		})
	}
	return out
}

// --- helpers -----------------------------------------------------------

// parseLimit reads ?limit=clamped to [1, max]. Empty or invalid → def.
func parseLimit(r *http.Request, def, max int) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errBody(format string, a ...any) map[string]string {
	return map[string]string{"error": fmt.Sprintf(format, a...)}
}
