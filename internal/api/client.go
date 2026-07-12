package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// HTTPClient is the typed client for the wiretap-relay admin HTTP API. It
// mirrors the routes in PLAN.md §8 and is the only piece of HTTP machinery
// the CLI subcommands touch; tests point it at httptest.Server.
//
// Ingress (POST /:project) is intentionally NOT a client method: real
// webhook senders do plain HTTP POST with arbitrary bodies and content
// types, and scripts can use curl. Encoding their payloads through a
// JSON-typed wrapper would corrupt content. The typed surface here only
// covers admin/v1 reads and writes.
//
// The client never prints; methods return typed responses or wrapped errors.
// Auth (admin_token or client_token) is set on the client once and applied
// to every request via applyAuth.
type HTTPClient struct {
	baseURL    *url.URL
	httpClient *http.Client
	adminToken string // applied as X-Admin-Token
	clientID   string // client side of HTTP basic auth
	clientTok  string // client side of HTTP basic auth
	userAgent  string
}

// ClientOption configures an HTTPClient.
type ClientOption func(*HTTPClient)

// WithHTTPClient injects a *http.Client (e.g. with timeouts). Defaults to
// http.DefaultClient.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(h *HTTPClient) { h.httpClient = c }
}

// WithAdminToken sets the X-Admin-Token used on admin and register calls.
func WithAdminToken(tok string) ClientOption {
	return func(h *HTTPClient) { h.adminToken = tok }
}

// WithClientAuth sets client_id / client_token for routes that require the
// owner. Stored as HTTP basic auth on outbound calls.
func WithClientAuth(clientID, token string) ClientOption {
	return func(h *HTTPClient) { h.clientID, h.clientTok = clientID, token }
}

// WithUserAgent sets an outbound User-Agent header.
func WithUserAgent(ua string) ClientOption {
	return func(h *HTTPClient) { h.userAgent = ua }
}

// NewClient builds a typed client pointed at base. base must include the
// scheme (http or https) and host; no trailing slash.
func NewClient(base string, opts ...ClientOption) (*HTTPClient, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("api: parse base %q: %w", base, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("api: base %q must use http or https", base)
	}
	c := &HTTPClient{
		baseURL:    u,
		httpClient: http.DefaultClient,
		userAgent:  "wiretap/dev",
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// do is the single JSON request path. It serializes in, applies auth, sends,
// and unmarshals the 2xx body into out. Non-2xx responses come back as *Error
// carrying the parsed ErrorResponse body so callers can branch on Code.
func (c *HTTPClient) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("api: marshal request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.absolute(path), body)
	if err != nil {
		return fmt.Errorf("api: build request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	c.applyAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("api: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

// absolute joins baseURL with path, ensuring no double slashes. Query
// strings in path are preserved.
func (c *HTTPClient) absolute(path string) string {
	return strings.TrimRight(c.baseURL.String(), "/") + "/" + strings.TrimLeft(path, "/")
}

// applyAuth stamps whichever credentials the client holds. admin_token goes
// in X-Admin-Token; client creds in HTTP basic auth. Both can be set at
// once; routes pick the relevant one. No-op when neither is set (used by
// Health, which is unauthenticated).
func (c *HTTPClient) applyAuth(req *http.Request) {
	if c.adminToken != "" {
		req.Header.Set("X-Admin-Token", c.adminToken)
	}
	if c.clientID != "" || c.clientTok != "" {
		req.SetBasicAuth(c.clientID, c.clientTok)
	}
}

// decodeResponse reads the response body, decoding into out on 2xx. On 4xx
// or 5xx it parses an ErrorResponse body if present and returns an *Error
// with structured fields so callers can use errors.As / Is* helpers.
func decodeResponse(resp *http.Response, out any) error {
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("api: read response: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || len(b) == 0 {
			return nil
		}
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("api: decode %d response: %w", resp.StatusCode, err)
		}
		return nil
	}
	er := &ErrorResponse{}
	if err := json.Unmarshal(b, er); err != nil || er.Message == "" {
		er.Code = ""
		er.Message = strings.TrimSpace(string(b))
	}
	return &Error{Status: resp.StatusCode, ErrorResponse: ErrorResponse{Code: er.Code, Message: er.Message}}
}

// Error is returned by every non-2xx response. Callers use errors.As to get
// the structured fields; codes are dynamic strings so switch on err.Code
// rather than errors.Is with a sentinel.
type Error struct {
	Status int
	ErrorResponse
}

// Error implements error.
func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("api: HTTP %d [%s]: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("api: HTTP %d: %s", e.Status, e.Message)
}

// IsNotFound reports whether err is a 404 from the relay.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// IsConflict reports whether err is a 409.
func IsConflict(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusConflict
}

// IsUnauthorized reports whether err is a 401 or 403.
func IsUnauthorized(err error) bool {
	var e *Error
	return errors.As(err, &e) && (e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden)
}

// CodeMatches reports whether err is an *Error with the given Code field.
// Convenience for branching on stable, sentinel-like error codes the relay
// emits ("auth_failed", "not_found", "conflict", "invalid_path", ...).
func CodeMatches(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// ----- typed route wrappers (one per admin HTTP route) -----

// Health calls GET /health. No auth required.
func (c *HTTPClient) Health(ctx context.Context) (*HealthResponse, error) {
	var out HealthResponse
	if err := c.do(ctx, http.MethodGet, "/health", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Register calls POST /register with the supplied admin token.
func (c *HTTPClient) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	var out RegisterResponse
	if err := c.do(ctx, http.MethodPost, "/register", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListClients calls GET /admin/clients. Requires admin auth.
func (c *HTTPClient) ListClients(ctx context.Context) (*ListClientsResponse, error) {
	var out ListClientsResponse
	if err := c.do(ctx, http.MethodGet, "/admin/clients", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetClient calls GET /admin/clients/:id. Requires admin auth.
func (c *HTTPClient) GetClient(ctx context.Context, clientID string) (*Client, error) {
	var out Client
	if err := c.do(ctx, http.MethodGet, "/admin/clients/"+url.PathEscape(clientID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteClient calls DELETE /admin/clients/:id. Requires admin auth.
// Cascades to the client's project bindings.
func (c *HTTPClient) DeleteClient(ctx context.Context, clientID string) error {
	return c.do(ctx, http.MethodDelete, "/admin/clients/"+url.PathEscape(clientID), nil, nil)
}

// ListProjects calls GET /admin/projects. Requires admin auth.
func (c *HTTPClient) ListProjects(ctx context.Context) (*ListProjectsResponse, error) {
	var out ListProjectsResponse
	if err := c.do(ctx, http.MethodGet, "/admin/projects", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReclaimProject calls POST /admin/projects to move a path between clients.
// Requires admin auth. With req.Force=true the relay rebinds an existing
// path; without it the call returns 409.
func (c *HTTPClient) ReclaimProject(ctx context.Context, req ReclaimProjectRequest) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodPost, "/admin/projects", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListWebhooks calls GET /admin/projects/:p/webhooks?after_seq=&limit=.
// afterSeq=0 returns from the start; limit=0 means use the server default.
func (c *HTTPClient) ListWebhooks(ctx context.Context, project string, afterSeq, limit int64) (*ListWebhooksResponse, error) {
	path := fmt.Sprintf("/admin/projects/%s/webhooks?after_seq=%d&limit=%d", url.PathEscape(project), afterSeq, limit)
	var out ListWebhooksResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplayWebhook calls POST /admin/projects/:p/webhooks/:seq/replay. Pushes
// the stored webhook down the owner's tunnel again. Requires admin or owner
// auth.
func (c *HTTPClient) ReplayWebhook(ctx context.Context, project string, seq int64) error {
	path := fmt.Sprintf("/admin/projects/%s/webhooks/%d/replay", url.PathEscape(project), seq)
	return c.do(ctx, http.MethodPost, path, nil, nil)
}
