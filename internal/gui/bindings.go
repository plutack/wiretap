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
	"net/http"
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

// ScriptView is the GUI + wailsjs DTO for a stored script. It mirrors
// store.ScriptRow with JSON-friendly field names; timestamps are RFC3339 UTC.
type ScriptView struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Trigger   string `json:"trigger"`
	Body      string `json:"body"`
	Priority  int    `json:"priority"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ScriptInput is the payload the editor sends to SaveScript. ID == 0 means
// "create"; a non-zero ID updates that row.
type ScriptInput struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Trigger  string `json:"trigger"`
	Body     string `json:"body"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

// ScriptTestRequest is the test-run payload: the script body plus a sample
// exchange (usually filled from the selected capture/webhook).
type ScriptTestRequest struct {
	Body    string            `json:"body"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	ReqBody string            `json:"req_body"`
	Status  int               `json:"status"`
}

// ScriptTestView is the test-run outcome the editor renders: the mutated
// request/response plus console logs and rejection state. Error, when set, is
// the script exception message (logs are still populated up to the failure).
type ScriptTestView struct {
	Method       string              `json:"method"`
	URL          string              `json:"url"`
	ReqHeaders   map[string][]string `json:"req_headers"`
	ReqBody      string              `json:"req_body"`
	Status       int                 `json:"status"`
	RespHeaders  map[string][]string `json:"resp_headers"`
	RespBody     string              `json:"resp_body"`
	Logs         []string            `json:"logs"`
	Rejected     bool                `json:"rejected"`
	RejectReason string              `json:"reject_reason,omitempty"`
	Error        string              `json:"error,omitempty"`
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

// --- scripts -------------------------------------------------------------

// ListScripts returns every stored script (all triggers) for the editor
// sidebar, ordered by trigger then priority.
func (b *Bindings) ListScripts() ([]ScriptView, error) {
	rows, err := b.app.Scripts(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list scripts: %w", err)
	}
	out := make([]ScriptView, 0, len(rows))
	for _, r := range rows {
		out = append(out, scriptView(r))
	}
	return out, nil
}

// GetScript returns one script by id (the editor loads the full body on
// selection). Wraps store.ErrNotFound when absent.
func (b *Bindings) GetScript(id int64) (ScriptView, error) {
	sc, err := b.app.ScriptByID(context.Background(), id)
	if err != nil {
		return ScriptView{}, fmt.Errorf("get script %d: %w", id, err)
	}
	return scriptView(*sc), nil
}

// SaveScript creates (ID == 0) or updates a script and returns its id. The
// frontend calls this from the editor's Save button.
func (b *Bindings) SaveScript(in ScriptInput) (int64, error) {
	row := store.ScriptRow{
		ID:       in.ID,
		Name:     in.Name,
		Trigger:  in.Trigger,
		Body:     in.Body,
		Priority: in.Priority,
		Enabled:  in.Enabled,
	}
	if in.ID == 0 {
		id, err := b.app.CreateScript(context.Background(), row)
		if err != nil {
			return 0, fmt.Errorf("create script: %w", err)
		}
		return id, nil
	}
	if err := b.app.UpdateScript(context.Background(), row); err != nil {
		return 0, fmt.Errorf("update script %d: %w", in.ID, err)
	}
	return in.ID, nil
}

// SetScriptEnabled toggles the enabled flag on one script (sidebar checkbox).
func (b *Bindings) SetScriptEnabled(id int64, enabled bool) error {
	if err := b.app.SetScriptEnabled(context.Background(), id, enabled); err != nil {
		return fmt.Errorf("set script %d enabled=%v: %w", id, enabled, err)
	}
	return nil
}

// DeleteScript removes a script by id.
func (b *Bindings) DeleteScript(id int64) error {
	if err := b.app.DeleteScript(context.Background(), id); err != nil {
		return fmt.Errorf("delete script %d: %w", id, err)
	}
	return nil
}

// TestScript runs a script body once against a sample exchange and returns the
// mutated result + logs. It never persists or touches live traffic. A script
// exception is returned in the view's Error field (not as a Go error) so the
// editor can render logs alongside the failure.
func (b *Bindings) TestScript(req ScriptTestRequest) (ScriptTestView, error) {
	out, err := b.app.TestScript(context.Background(), req.Body, app.ScriptTestInput{
		Method:  req.Method,
		URL:     req.URL,
		Headers: expandHeaders(req.Headers),
		Body:    req.ReqBody,
		Status:  req.Status,
	})
	if errors.Is(err, app.ErrScriptEngineUnavailable) {
		return ScriptTestView{}, err
	}
	v := ScriptTestView{
		Method:       out.Method,
		URL:          out.URL,
		ReqHeaders:   out.ReqHeaders,
		ReqBody:      out.ReqBody,
		Status:       out.Status,
		RespHeaders:  out.RespHeaders,
		RespBody:     out.RespBody,
		Logs:         out.Logs,
		Rejected:     out.Rejected,
		RejectReason: out.RejectReason,
	}
	if err != nil {
		v.Error = err.Error()
	}
	return v, nil
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

// scriptView converts a stored script row into the JSON-friendly DTO the
// frontend renders. Timestamps use RFC3339 UTC for consistency with the other
// views; the body is passed through verbatim (the editor displays/saves it as
// text, not a structured object).
func scriptView(r store.ScriptRow) ScriptView {
	return ScriptView{
		ID:        r.ID,
		Name:      r.Name,
		Trigger:   r.Trigger,
		Body:      r.Body,
		Priority:  r.Priority,
		Enabled:   r.Enabled,
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
		UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
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

// expandHeaders converts the single-valued map the frontend sends into
// http.Header (canonicalized values). The editor emits at most one value per
// header name, so a missing slice entry is treated as a single-element header.
// Names are canonicalized via http.Header.Set so lookups by the standard form
// (e.g. "Content-Type") work regardless of how the user typed them in the UI.
func expandHeaders(in map[string]string) http.Header {
	h := http.Header{}
	for k, v := range in {
		h.Set(k, v)
	}
	return h
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
