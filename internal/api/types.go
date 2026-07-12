// Package api defines the contract between the local wiretap app, the
// wiretap-relay server, and anything else (curl, scripts) that talks to
// the relay over HTTP.
//
// The package contains no I/O: only the JSON request/response types used by
// relayd's HTTP handlers and the typed HTTP client in client.go. Keeping
// the contract in one place is the "one API, multiple frontends"
// invariant from docs/PLAN.md §5 — relayd, the CLI, the GUI, and external
// scripts all share these DTOs.
package api

// RegisterRequest is the body of POST /register. The admin_token authenticates
// the call; projects is the set of paths this client wants to claim.
// display_name is an optional human label shown in admin listings.
type RegisterRequest struct {
	AdminToken  string   `json:"admin_token"`
	Projects    []string `json:"projects"`
	DisplayName string   `json:"display_name,omitempty"`
}

// RegisterResponse is returned on a successful register. The client_id and
// client_token are the credentials the PC must store locally and use on
// every tunnel connect. They are returned exactly once.
type RegisterResponse struct {
	ClientID    string   `json:"client_id"`
	ClientToken string   `json:"client_token"`
	Projects    []string `json:"projects"`
}

// HealthResponse is the body of GET /health. tunnel_count is the number of
// currently-connected tunnels; included once we wire that up.
type HealthResponse struct {
	Status      string `json:"status"`
	Version     string `json:"version"`
	TunnelCount int    `json:"tunnel_count,omitempty"`
}

// IngressResponse is returned on a successful POST /:project. seq is the
// sequence number assigned to the webhook by the relay; useful for the
// caller to reference later via /admin/projects/:p/webhooks/:seq/replay.
type IngressResponse struct {
	Seq int64 `json:"seq"`
}

// Client is the public projection of a registered client. Used in
// /admin/clients listings.
type Client struct {
	ClientID    string   `json:"client_id"`
	DisplayName string   `json:"display_name,omitempty"`
	CreatedAt   int64    `json:"created_at"`             // unix seconds
	LastSeenAt  int64    `json:"last_seen_at,omitempty"` // 0 = never
	Projects    []string `json:"projects,omitempty"`
}

// Project is the public projection of a claimed project path.
type Project struct {
	Path      string `json:"path"`
	ClientID  string `json:"client_id"` // owning client_id
	CreatedAt int64  `json:"created_at"`
	AckedSeq  int64  `json:"acked_seq"` // highest acked webhook seq by owner
}

// Webhook is the public projection of a stored webhook. Returned by
// /admin/projects/:p/webhooks and (later) by replay.
//
// Headers are the parsed http.Header as JSON-shaped map. RawHeaders is the
// raw header block (CRLF joined, preserving duplicates) base64-encoded for
// JSON transport; the typed HTTP client decodes it back to []byte.
// Body is base64-encoded for the same reason.
type Webhook struct {
	Project    string              `json:"project"`
	Seq        int64               `json:"seq"`
	ReceivedAt int64               `json:"received_at"` // unix seconds
	SourceIP   string              `json:"source_ip,omitempty"`
	Method     string              `json:"method"`
	Path       string              `json:"path,omitempty"`
	Headers    map[string][]string `json:"headers"`
	RawHeaders []byte              `json:"raw_headers,omitempty"` // base64 in JSON
	Body       []byte              `json:"body"`                  // base64 in JSON
}

// ReclaimProjectRequest is the body of POST /admin/projects (reclaim).
// With force=true the relay moves an already-owned path to new_client_id;
// without force it errors with ErrConflict.
type ReclaimProjectRequest struct {
	Path        string `json:"path"`
	NewClientID string `json:"new_client_id"`
	Force       bool   `json:"force,omitempty"`
}

// ListClientsResponse wraps GET /admin/clients.
type ListClientsResponse struct {
	Clients []Client `json:"clients"`
}

// ListProjectsResponse wraps GET /admin/projects.
type ListProjectsResponse struct {
	Projects []Project `json:"projects"`
}

// ListWebhooksResponse wraps GET /admin/projects/:p/webhooks. Includes a
// cursor for pagination: the next request passes after_seq = next_after_seq
// to continue. Empty cur.ser indicates end of results.
type ListWebhooksResponse struct {
	Webhooks     []Webhook `json:"webhooks"`
	NextAfterSeq int64     `json:"next_after_seq,omitempty"`
}

// ErrorResponse is the body returned on any 4xx/5xx. Code is a stable
// machine-readable string (e.g. "auth_failed", "not_found", "conflict");
// Message is human-readable.
type ErrorResponse struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}
