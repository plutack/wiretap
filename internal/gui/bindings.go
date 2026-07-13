// Package gui exposes the Wails binding surface for the wiretap GUI. It is a
// thin, marshaling-only adapter over the already-tested composition root
// (internal/app): every method delegates straight to *app.App and reshapes
// the store row types into JSON-friendly views for the frontend.
//
// Design notes:
//   - This package does NOT import Wails. Keeping the Wails runtime out of the
//     binding layer means it compiles and tests with the default build (no
//     `gui` build tag, no webkit2gtk/CGO), so `go test -race ./...` stays green
//     on machines that lack the GUI toolchain. The only file that imports Wails
//     is internal/cli/gui.go, build-tagged `gui`.
//   - Views convert raw []byte bodies to string for display. Byte-exact replay
//     still uses the original bytes via app.App.ReplayWebhook; the GUI only
//     shows text, so a UTF-8 best-effort string is the right shape here.
//   - DTOs flatten store.WebhookRow / store.TrafficCaptureRow so the frontend
//     never sees SQL/relayproto types (same isolation localapi uses).
package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/plutack/wiretap/internal/app"
	"github.com/plutack/wiretap/internal/store"
)

// listLimit is the row cap for list calls. Matches the localapi default; big
// enough for an MVP dashboard, small enough to keep payload sane.
const listLimit = 100

// Bindings is the struct registered with wails.Run as the bound App. Every
// exported method becomes a callable in the frontend's wailsjs bindings.
type Bindings struct {
	app     *app.App
	version string
}

// Option configures a Bindings.
type Option func(*Bindings)

// WithVersion sets the version string reported by Status (from the build).
func WithVersion(v string) Option { return func(b *Bindings) { b.version = v } }

// New builds a binding layer over a (already-Open) *app.App. The caller owns the
// App lifecycle (Open/StartTunnel/Close); the bindings are read-only w.r.t. it
// except for ReplayWebhook, which mutates an external target URL, not the store.
func New(a *app.App, opts ...Option) *Bindings {
	b := &Bindings{app: a, version: "dev"}
	for _, o := range opts {
		o(b)
	}
	return b
}

// --- Views ---------------------------------------------------------------

// WebhookView is the GUI + wailsjs DTO for a webhook row. Body is omitted in
// list responses (BodyLen set); GetWebhook fills Body + Headers for the detail
// view and the replay form.
type WebhookView struct {
	Project    string              `json:"project"`
	Seq        int64               `json:"seq"`
	ReceivedAt string              `json:"received_at"` // RFC3339 UTC
	SourceIP   string              `json:"source_ip,omitempty"`
	Method     string              `json:"method,omitempty"`
	Path       string              `json:"path,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       string              `json:"body,omitempty"`
	BodyLen    int                 `json:"body_len"`
}

// CaptureView is the GUI DTO for a traffic capture. Bodies are omitted in list
// responses; GetCapture fills them for the detail pane.
type CaptureView struct {
	ID          int64               `json:"id"`
	At          string              `json:"at"` // RFC3339 UTC
	Method      string              `json:"method,omitempty"`
	URL         string              `json:"url,omitempty"`
	Status      int                 `json:"status,omitempty"`
	ReqHeaders  map[string][]string `json:"req_headers,omitempty"`
	ReqBody     string              `json:"req_body,omitempty"`
	ReqBodyLen  int                 `json:"req_body_len"`
	RespHeaders map[string][]string `json:"resp_headers,omitempty"`
	RespBody    string              `json:"resp_body,omitempty"`
	RespBodyLen int                 `json:"resp_body_len"`
}

// StatusView is the status-bar payload: build version plus the resolved app
// state the GUI shows in its header.
type StatusView struct {
	Version           string   `json:"version"`
	StoreOpen         bool     `json:"store_open"`
	RelayURL          string   `json:"relay_url,omitempty"`
	TunnelRunning     bool     `json:"tunnel_running"`
	ConnectedProjects []string `json:"connected_projects,omitempty"`
}

// ReplayResult is the return value of ReplayWebhook: the upstream HTTP status.
type ReplayResult struct {
	Status int `json:"status"`
}

// --- Bound methods -------------------------------------------------------

// ListWebhooks returns the most recent webhooks, newest-first, optionally
// filtered by project (empty string = all projects). Body/Headers are omitted
// (use GetWebhook for the full payload).
func (b *Bindings) ListWebhooks(project string) ([]WebhookView, error) {
	rows, err := b.app.Webhooks(context.Background(), project, listLimit)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	return webhookSummary(rows), nil
}

// ListCaptures returns the most recent traffic captures, newest-first. Bodies
// and full header maps are omitted (use GetCapture for the detail payload).
func (b *Bindings) ListCaptures() ([]CaptureView, error) {
	rows, err := b.app.Captures(context.Background(), listLimit)
	if err != nil {
		return nil, fmt.Errorf("list captures: %w", err)
	}
	return captureSummary(rows), nil
}

// GetWebhook returns one webhook with body + headers populated for the detail
// view and the replay form. Returns an error suitable for the frontend when the
// row is absent (errors.Is, store.ErrNotFound).
func (b *Bindings) GetWebhook(project string, seq int64) (WebhookView, error) {
	wh, err := b.app.WebhookBySeq(context.Background(), project, seq)
	if err != nil {
		return WebhookView{}, fmt.Errorf("get webhook %s/%d: %w", project, seq, err)
	}
	return webhookDetail(wh), nil
}

// ReplayWebhook re-POSTs a stored webhook to targetURL and returns the upstream
// HTTP status. Delegates to app.App.ReplayWebhook (byte-exact body + hop-by-hop
// stripping already handled there).
func (b *Bindings) ReplayWebhook(project string, seq int64, targetURL string) (ReplayResult, error) {
	if targetURL == "" {
		return ReplayResult{}, errors.New("replay: target URL is required")
	}
	status, err := b.app.ReplayWebhook(context.Background(), project, seq, targetURL)
	if err != nil {
		return ReplayResult{}, err
	}
	return ReplayResult{Status: status}, nil
}

// Status reports build version + app state for the GUI header. It is best-effort
// about config (a missing config file is not an error here — falls back to
// defaults, same as the TUI).
//
// ConnectedProjects mirrors app.App.ConnectedProjects — the list the relay
// says this client owns, set by the tunnel's OnConnect callback. It is nil when
// no tunnel is attached, so the GUI can render "watching: tunnel down".
func (b *Bindings) Status() StatusView {
	v := StatusView{Version: b.version, StoreOpen: b.app.Store() != nil}
	if cfg, err := b.app.Config(); err == nil {
		v.RelayURL = cfg.Relay.URL
	}
	v.TunnelRunning = b.app.TunnelRunning()
	v.ConnectedProjects = b.app.ConnectedProjects()
	return v
}

// --- converters ---------------------------------------------------------

func webhookSummary(rows []store.WebhookRow) []WebhookView {
	out := make([]WebhookView, 0, len(rows))
	for _, r := range rows {
		out = append(out, WebhookView{
			Project:    r.Project,
			Seq:        r.Seq,
			ReceivedAt: r.ReceivedAt.Format(time.RFC3339),
			SourceIP:   r.SourceIP,
			Method:     r.Method,
			Path:       r.Path,
			BodyLen:    len(r.Body),
		})
	}
	return out
}

func webhookDetail(w *store.WebhookRow) WebhookView {
	return WebhookView{
		Project:    w.Project,
		Seq:        w.Seq,
		ReceivedAt: w.ReceivedAt.Format(time.RFC3339),
		SourceIP:   w.SourceIP,
		Method:     w.Method,
		Path:       w.Path,
		Headers:    parseHeaders(w.HeadersJSON),
		Body:       string(w.Body),
		BodyLen:    len(w.Body),
	}
}

func captureSummary(rows []store.TrafficCaptureRow) []CaptureView {
	out := make([]CaptureView, 0, len(rows))
	for _, r := range rows {
		out = append(out, CaptureView{
			ID:          r.ID,
			At:          r.At.Format(time.RFC3339),
			Method:      r.Method,
			URL:         r.URL,
			Status:      r.Status,
			ReqBodyLen:  len(r.ReqBody),
			RespBodyLen: len(r.RespBody),
		})
	}
	return out
}

// parseHeaders decodes the JSON http.Header stored in the row. Returns an empty
// map on any parse failure (best-effort display).
func parseHeaders(jsonStr string) map[string][]string {
	h := map[string][]string{}
	if jsonStr == "" {
		return h
	}
	if err := json.Unmarshal([]byte(jsonStr), &h); err != nil {
		return map[string][]string{}
	}
	return h
}
