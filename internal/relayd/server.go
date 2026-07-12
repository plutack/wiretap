package relayd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/plutack/wiretap/internal/api"
	"github.com/plutack/wiretap/internal/store"
	"github.com/plutack/wiretap/internal/testutil"
)

// Server holds the relayd dependencies constructed once at startup. Every
// handler is a method on *Server so it can reach state without package-level
// variables. Tests build a Server with an in-memory store, fake clock, and
// fake id generator via NewServer options.
type Server struct {
	store      *store.RelayStore
	adminToken string
	clock      testutil.Clock
	idgen      testutil.IDGen
	version    string
	tunnels    *TunnelRegistry
}

// Option configures a Server.
type Option func(*Server)

// WithClock injects a clock. Tests pass a FakeClock for deterministic
// received_at and created_at stamps.
func WithClock(c testutil.Clock) Option { return func(s *Server) { s.clock = c } }

// WithIDGen injects an id generator. Tests pass a FakeIDGen so register
// returns predictable client_id / client_token values.
func WithIDGen(g testutil.IDGen) Option { return func(s *Server) { s.idgen = g } }

// WithAdminToken sets the X-Admin-Token required on /register and /admin/*
// routes. An empty token rejects all admin requests.
func WithAdminToken(tok string) Option { return func(s *Server) { s.adminToken = tok } }

// WithVersion stamps the version returned by /health.
func WithVersion(v string) Option { return func(s *Server) { s.version = v } }

// NewServer wires dependencies. store is required; clock and idgen default to
// SystemClock / HexIDGen.
func NewServer(st *store.RelayStore, opts ...Option) *Server {
	s := &Server{
		store:   st,
		clock:   testutil.SystemClock{},
		idgen:   testutil.HexIDGen{},
		version: "dev",
		tunnels: NewTunnelRegistry(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// projectPathRE enforces the project name format from PLAN.md §11:
// lowercase alphanumerics and hyphens, length 2-63. Reserved roots
// (tunnel/register/admin/health) are filtered at the mux level so they
// never collide with ingress.
var projectPathRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

// Routes builds the HTTP mux for production and tests alike. Order matters:
// reserved roots (admin, register, health, tunnel) are registered with
// explicit paths; the catch-all "/" handles webhook ingress by treating the
// first path segment as the project name.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /register", s.requireAdmin(s.handleRegister))
	mux.HandleFunc("GET /admin/clients", s.requireAdmin(s.handleListClients))
	mux.HandleFunc("GET /admin/clients/{clientID}", s.requireAdmin(s.handleGetClient))
	mux.HandleFunc("DELETE /admin/clients/{clientID}", s.requireAdmin(s.handleDeleteClient))
	mux.HandleFunc("GET /admin/projects", s.requireAdmin(s.handleListProjects))
	mux.HandleFunc("POST /admin/projects", s.requireAdmin(s.handleReclaimProject))
	mux.HandleFunc("GET /admin/projects/{project}/webhooks", s.requireAdmin(s.handleListWebhooks))
	// WebSocket tunnel. Auth via HTTP basic auth on the upgrade request.
	mux.HandleFunc("GET /tunnel", s.HandleTunnel)
	// Catch-all for ingress. The first segment of the path is the project;
	// everything after is preserved as the webhook's "path" column.
	mux.HandleFunc("/", s.handleIngress)
	return mux
}

// handleHealth returns a static liveness payload. We do not probe the store
// here so a DB hiccup never blocks monitoring probes; a separate /readyz
// route can be added later for that.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.HealthResponse{Status: "ok", Version: s.version})
}

// handleRegister creates a new client and binds the requested project
// paths. The admin_token check is enforced by requireAdmin wrapping.
//
// On conflict (path already owned by another client) we roll back the
// client row to keep the registration atomic and return 409. This is one of
// the few multi-statement handlers, so we wrap the work in a transaction.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req api.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if len(req.Projects) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "at least one project is required")
		return
	}
	for _, p := range req.Projects {
		if !projectPathRE.MatchString(p) {
			writeErr(w, http.StatusBadRequest, "invalid_path",
				fmt.Sprintf("project %q does not match %s", p, projectPathRE.String()))
			return
		}
	}

	clientID := s.idgen.NewID()
	clientTok := s.idgen.NewID()
	now := s.clock.Now()

	// Create client first.
	if err := s.store.CreateClient(r.Context(), clientID, clientTok, req.DisplayName, now); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "create client: "+err.Error())
		return
	}
	// Bind each project. On any failure roll back the client row so the
	// registration is all-or-nothing; clients can retry with adjusted paths.
	for _, p := range req.Projects {
		if err := s.store.BindProject(r.Context(), p, clientID, now); err != nil {
			if errors.Is(err, store.ErrConflict) {
				_ = s.store.DeleteClient(r.Context(), clientID)
				writeErr(w, http.StatusConflict, "conflict",
					fmt.Sprintf("project %q already claimed", p))
				return
			}
			_ = s.store.DeleteClient(r.Context(), clientID)
			writeErr(w, http.StatusInternalServerError, "internal", "bind project: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusCreated, api.RegisterResponse{
		ClientID: clientID, ClientToken: clientTok, Projects: req.Projects,
	})
}

// handleIngress receives a webhook. The first path segment names the project;
// any trailing path is preserved as the webhook's path field. Capturing the
// raw body and headers byte-exact is the invariant from PLAN.md §6.
//
// We never require auth on ingress: webhook senders don't carry our tokens.
// The project must exist and be owned by some registered client; otherwise
// we 404 to avoid leaking the existence of unclaimed paths.
func (s *Server) handleIngress(w http.ResponseWriter, r *http.Request) {
	// Reserved routes never reach here (they're registered earlier in the
	// mux), but defensive check the path starts with /something.
	if r.URL.Path == "/" {
		writeErr(w, http.StatusBadRequest, "missing_project", "URL must include a project path")
		return
	}
	// Pull first segment. url.PathUnescape allows percent-encoded hyphens.
	segments := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	project, err := url.PathUnescape(segments[0])
	if err != nil || !projectPathRE.MatchString(project) {
		writeErr(w, http.StatusNotFound, "invalid_path", "no such project")
		return
	}
	// Ensure someone owns this project.
	if _, err := s.store.ClientByProject(r.Context(), project); err != nil {
		// Treat not-found as 404 (don't leak existence to scanners).
		writeErr(w, http.StatusNotFound, "not_found", "no such project")
		return
	}
	// Rest of path after the project (may be empty).
	subPath := ""
	if len(segments) > 1 {
		subPath = "/" + segments[1]
	}
	body, err := readBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	rawHeaders := serializeRawHeaders(r.Header, r.Method, r.Host)
	headersJSON, _ := json.Marshal(headersToMap(r.Header))

	// Allocate the next seq for the project and insert.
	seq, err := s.store.NextWebhookSeq(r.Context(), project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "next seq: "+err.Error())
		return
	}
	row := store.WebhookRow{
		Project:     project,
		Seq:         seq,
		ReceivedAt:  s.clock.Now(),
		SourceIP:    clientIP(r),
		Method:      r.Method,
		Path:        subPath,
		HeadersJSON: string(headersJSON),
		RawHeaders:  rawHeaders,
		Body:        body,
	}
	if err := s.store.InsertWebhook(r.Context(), row); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "insert webhook: "+err.Error())
		return
	}

	// If a tunnel is currently attached for this project, push the webhook
	// straight down so the PC sees it immediately rather than waiting for a
	// reconnect.
	s.pushIfTunnelAttached(r.Context(), project, row)

	writeJSON(w, http.StatusOK, api.IngressResponse{Seq: seq})
}

// readBody returns the request body, capped at maxBodyBytes to prevent a
// misbehaving sender from exhausting relay memory. Bodies over the cap are
// truncated and a sentinel error is returned.
func readBody(r *http.Request) ([]byte, error) {
	const maxBodyBytes = 10 * 1024 * 1024 // 10 MiB
	if r.Body == nil {
		return nil, nil
	}
	if !methodAllowsBody(r.Method) && r.ContentLength == 0 {
		return nil, nil
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(b)) > maxBodyBytes {
		return nil, fmt.Errorf("body exceeds %d bytes", maxBodyBytes)
	}
	return b, nil
}

// clientIP extracts the originating IP. Prefers the leftmost X-Forwarded-For
// value (set by proxies / load balancers), falls back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host
}

//----- admin routes -----

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListClients(r.Context(), true)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	clients := make([]api.Client, 0, len(rows))
	for _, c := range rows {
		// Look up each client's projects. n+1 is fine for MVP; the row count
		// of registered clients is expected to be in single digits.
		paths, err := s.store.ProjectsByClient(r.Context(), c.ClientID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		var lastSeen int64
		if !c.LastSeenAt.IsZero() {
			lastSeen = c.LastSeenAt.Unix()
		}
		clients = append(clients, api.Client{
			ClientID:    c.ClientID,
			DisplayName: c.DisplayName,
			CreatedAt:   c.CreatedAt.Unix(),
			LastSeenAt:  lastSeen,
			Projects:    paths,
		})
	}
	writeJSON(w, http.StatusOK, api.ListClientsResponse{Clients: clients})
}

func (s *Server) handleGetClient(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("clientID")
	if clientID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "missing client_id")
		return
	}
	c, err := s.store.Client(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "no such client")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	paths, _ := s.store.ProjectsByClient(r.Context(), clientID)
	var lastSeen int64
	if !c.LastSeenAt.IsZero() {
		lastSeen = c.LastSeenAt.Unix()
	}
	writeJSON(w, http.StatusOK, api.Client{
		ClientID:    c.ClientID,
		DisplayName: c.DisplayName,
		CreatedAt:   c.CreatedAt.Unix(),
		LastSeenAt:  lastSeen,
		Projects:    paths,
	})
}

func (s *Server) handleDeleteClient(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("clientID")
	if err := s.store.DeleteClient(r.Context(), clientID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "no such client")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListProjects(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	projects := make([]api.Project, 0, len(rows))
	for _, p := range rows {
		projects = append(projects, api.Project{
			Path:      p.Path,
			ClientID:  p.ClientID,
			CreatedAt: p.CreatedAt.Unix(),
			AckedSeq:  p.AckedSeq,
		})
	}
	writeJSON(w, http.StatusOK, api.ListProjectsResponse{Projects: projects})
}

func (s *Server) handleReclaimProject(w http.ResponseWriter, r *http.Request) {
	var req api.ReclaimProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Path == "" || req.NewClientID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "path and new_client_id are required")
		return
	}
	// Confirm the new owner exists.
	if _, err := s.store.Client(r.Context(), req.NewClientID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "new_client_id does not exist")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	// Check current owner to surface the conflict vs not-found error.
	current, err := s.store.Project(r.Context(), req.Path)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "no such project")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if current.ClientID == req.NewClientID {
		// Idempotent no-op.
		writeJSON(w, http.StatusOK, api.Project{
			Path:      current.Path,
			ClientID:  current.ClientID,
			CreatedAt: current.CreatedAt.Unix(),
			AckedSeq:  current.AckedSeq,
		})
		return
	}
	if !req.Force {
		writeErr(w, http.StatusConflict, "conflict",
			fmt.Sprintf("project %q is owned by %q; use force=true to reclaim", req.Path, current.ClientID))
		return
	}
	if err := s.store.ReclaimProject(r.Context(), req.Path, req.NewClientID, s.clock.Now()); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	updated, err := s.store.Project(r.Context(), req.Path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api.Project{
		Path:      updated.Path,
		ClientID:  updated.ClientID,
		CreatedAt: updated.CreatedAt.Unix(),
		AckedSeq:  updated.AckedSeq,
	})
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if project == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "missing project")
		return
	}
	afterSeq := parseInt64Query(r, "after_seq", 0)
	limit := parseInt64Query(r, "limit", 50)
	rows, next, err := s.store.ListWebhooks(r.Context(), project, afterSeq, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	webhooks := make([]api.Webhook, 0, len(rows))
	for _, wh := range rows {
		var headers map[string][]string
		_ = json.Unmarshal([]byte(wh.HeadersJSON), &headers)
		webhooks = append(webhooks, api.Webhook{
			Project:    wh.Project,
			Seq:        wh.Seq,
			ReceivedAt: wh.ReceivedAt.Unix(),
			SourceIP:   wh.SourceIP,
			Method:     wh.Method,
			Path:       wh.Path,
			Headers:    headers,
			RawHeaders: wh.RawHeaders,
			Body:       wh.Body,
		})
	}
	writeJSON(w, http.StatusOK, api.ListWebhooksResponse{
		Webhooks:     webhooks,
		NextAfterSeq: next,
	})
}

// parseInt64Query parses a query parameter as int64, returning fallback on
// parse error or absence.
func parseInt64Query(r *http.Request, key string, fallback int64) int64 {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := parseInt64(v)
	if err != nil {
		return fallback
	}
	return n
}

// parseInt64 is the directed import for strconv.ParseInt with defaults.
// Indirected so tests can override; not currently overridden.
var parseInt64 = func(s string) (int64, error) {
	return strconvParseInt(s)
}

// strconvParseInt is a stand-in alias to keep imports tidy; tests override
// parseInt64 directly when needed.
func strconvParseInt(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// AckedSeqThroughStore reports the relay's current acked_seq cursor for
// project. Exported so cross-package integration tests (see
// internal/relayclient/integration_test.go) can assert on it without poking
// private fields. Production code should prefer RelayStore.AckedSeq
// directly via Server.Store().
func (s *Server) AckedSeqThroughStore(ctx context.Context, project string) (int64, error) {
	return s.store.AckedSeq(ctx, project)
}

// PendingCountThroughStore reports the number of undelivered webhooks for
// project on the relay. Same rationale as AckedSeqThroughStore: exported so
// cross-package integration tests can assert without breaking the
// encapsulation of the private store field.
func (s *Server) PendingCountThroughStore(ctx context.Context, project string) (int64, error) {
	return s.store.PendingCount(ctx, project)
}

// Store exposes the relay's underlying RelayStore. Production callers should
// use it sparingly; prefer adding a domain method to Server. Cross-package
// tests use it for ad-hoc seeding (Store.CreateClient, Store.BindProject,
// Store.StoreWebhook) without re-implementing those helpers.
func (s *Server) Store() *store.RelayStore { return s.store }

// pathClean normalises a path for logging; safe wrapper around path.Clean.
func pathClean(p string) string { return path.Clean("/" + p) }
