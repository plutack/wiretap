// Package api defines the contract between the local wiretap app, the
// wiretap-relay server, and anything else (curl, scripts) that talks to
// the relay over HTTP.
//
// The package contains no I/O: only the JSON request/response types used by
// relayd's HTTP handlers and the typed HTTP client in client.go. Keeping the
// contract in one place ensures relayd, the CLI, the GUI, and external scripts
// all share these DTOs.
package api

// RegisterRequest is the body of POST /register. The admin_token authenticates
// the call. Projects is an optional set of initial paths kept for compatibility;
// normal project changes use the authenticated /client/projects routes.
type RegisterRequest struct {
	AdminToken  string   `json:"admin_token"`
	Projects    []string `json:"projects,omitempty"`
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

// ProjectRequest is the body of POST /client/projects. The authenticated
// client creates a new project as its first subscriber.
type ProjectRequest struct {
	Path string `json:"path"`
}

// ClientProjectsResponse is returned after a client subscription mutation.
type ClientProjectsResponse struct {
	Projects []string `json:"projects"`
}

// HealthResponse is the body of GET /health. tunnel_count is the number of
// currently connected desktop tunnel sessions.
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

// Project is the public projection of a relay project and its subscribers.
type Project struct {
	Path          string                `json:"path"`
	CreatedAt     int64                 `json:"created_at"`
	NextSeq       int64                 `json:"next_seq"`
	WebhookCount  int64                 `json:"webhook_count"`
	Subscriptions []ProjectSubscription `json:"subscriptions"`
	// Legacy single-owner projection retained for older admin clients.
	ClientID string `json:"client_id,omitempty"`
	AckedSeq int64  `json:"acked_seq,omitempty"`
}

// ProjectSubscription exposes one client's independent delivery state.
type ProjectSubscription struct {
	ClientID  string `json:"client_id"`
	CreatedAt int64  `json:"created_at"`
	StartSeq  int64  `json:"start_seq"`
	AckedSeq  int64  `json:"acked_seq"`
	Pending   int64  `json:"pending"`
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
	BodyBytes  int                 `json:"body_bytes,omitempty"`
}

// ReclaimProjectRequest is the legacy replace-subscriptions operation. With
// force=true every existing subscriber is replaced by new_client_id.
type ReclaimProjectRequest struct {
	Path        string `json:"path"`
	NewClientID string `json:"new_client_id"`
	Force       bool   `json:"force,omitempty"`
}

// AssignProjectRequest is the body of PUT /admin/projects/:project. It
// creates a new project binding without creating or rotating a client.
type AssignProjectRequest struct {
	ClientID       string `json:"client_id"`
	IncludeHistory bool   `json:"include_history,omitempty"`
}

// DeleteWebhooksRequest selects one batch deletion mode. Exactly one of Seqs,
// ThroughSeq, or All must be supplied.
type DeleteWebhooksRequest struct {
	Seqs       []int64 `json:"seqs,omitempty"`
	ThroughSeq int64   `json:"through_seq,omitempty"`
	All        bool    `json:"all,omitempty"`
}

type DeleteWebhooksResponse struct {
	Deleted int64 `json:"deleted"`
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
// to continue. A zero cursor indicates the end of results.
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
