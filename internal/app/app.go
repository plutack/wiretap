// Package app is the composition root for the local wiretap app: it owns the
// long-lived process state (config, credentials, the local SQLite store, and
// the background relay tunnel) and exposes the read + replay surface the TUI,
// the Wails GUI, and the 127.0.0.1 control API all share.
//
// Design notes:
//   - App holds a *config.Manager so it can resolve the store path default
//     (<configDir>/wiretap.db) and load credentials lazily, the same way the
//     CLI does.
//   - The relay tunnel is behind a TunnelRunner seam so tests run without a
//     real relay. Production wires relayclient.Client; tests inject a noop.
//   - The Querier surface (Webhooks / Captures / WebhookBySeq) lets consumers
//     read the local store without importing store or sql directly.
//   - ReplayWebhook re-POSTs a locally stored webhook to a target URL the user
//     picks (the GUI "replay" button). This is a local replay, distinct from
//     the relay's tunnel re-push.
package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/relayclient"
	"github.com/plutack/wiretap/internal/scripting"
	"github.com/plutack/wiretap/internal/store"
	"github.com/plutack/wiretap/internal/testutil"
)

// TunnelRunner is the seam for the background relay tunnel. relayclient.Client
// satisfies it; tests substitute a noop so App tests never touch the network.
type TunnelRunner interface {
	Run(ctx context.Context) error
}

// App owns the local process state. Construct with New, then Open to ready
// the store, StartTunnel to connect the relay, and Close to release everything.
// Methods are safe for concurrent use; the tunnel lifecycle is mutex-guarded.
type App struct {
	mgr   *config.Manager
	cfg   *config.Config
	creds *config.Credentials
	clock testutil.Clock

	// tunnelFactory builds a runner from the resolved config + store.
	// Default wires relayclient.Client; tests inject a noop.
	tunnelFactory func(cfg TunnelConfig, st *store.PCStore) TunnelRunner

	// replayTransport is the HTTP transport used by ReplayWebhook. Default is
	// a short-timeout transport that skips the interception proxy (replays go
	// direct, not through the MITM). Tests inject a stub.
	replayTransport http.RoundTripper

	// scriptEngine, when set, runs on_replay scripts against a webhook before it
	// is re-POSTed by ReplayWebhook (e.g. regenerate a signature, bump a
	// timestamp). Nil disables scripting. onScriptError, when set, receives any
	// script run error.
	scriptEngine  *scripting.Engine
	onScriptError func(trigger scripting.Trigger, name string, err error)

	db    *sql.DB
	store *store.PCStore

	mu                sync.Mutex
	tunnelCtx         context.Context
	tunnelCancel      context.CancelFunc
	tunnelDone        chan struct{}
	connectedProjects []string // projects the relay says this client owns (set via OnConnect); nil when no tunnel attached
}

// TunnelConfig is the resolved relay connection parameters passed to the
// tunnel factory. Built from config.Relay + credentials.
type TunnelConfig struct {
	URL         string
	ClientID    string
	ClientToken string
	Projects    []string
}

// Option configures an App.
type Option func(*App)

// WithConfig overrides the loaded config (useful when a caller already has it).
func WithConfig(cfg *config.Config) Option {
	return func(a *App) { a.cfg = cfg }
}

// WithCredentials overrides the loaded credentials.
func WithCredentials(creds *config.Credentials) Option {
	return func(a *App) { a.creds = creds }
}

// WithClock injects a clock. Defaults to SystemClock.
func WithClock(c testutil.Clock) Option { return func(a *App) { a.clock = c } }

// WithTunnelFactory overrides the tunnel runner factory (test seam).
func WithTunnelFactory(f func(cfg TunnelConfig, st *store.PCStore) TunnelRunner) Option {
	return func(a *App) { a.tunnelFactory = f }
}

// WithReplayTransport overrides the HTTP transport used by ReplayWebhook
// (test seam). Production uses a direct, short-timeout transport.
func WithReplayTransport(rt http.RoundTripper) Option {
	return func(a *App) { a.replayTransport = rt }
}

// WithScriptEngine installs the JS scripting engine used to run on_replay
// scripts inside ReplayWebhook. onError (optional) receives per-script run
// errors so the GUI/TUI can surface them.
func WithScriptEngine(e *scripting.Engine, onError func(scripting.Trigger, string, error)) Option {
	return func(a *App) {
		a.scriptEngine = e
		a.onScriptError = onError
	}
}

// New builds an App wired to the given config manager. The manager is the path
// resolver for the store default and the credentials file; Open and
// StartTunnel load from it lazily unless overridden via options.
func New(mgr *config.Manager, opts ...Option) *App {
	a := &App{
		mgr:             mgr,
		clock:           testutil.SystemClock{},
		replayTransport: http.DefaultTransport.(*http.Transport).Clone(),
	}
	a.tunnelFactory = a.defaultTunnelFactory // method value; closes over a so OnConnect can write back
	for _, o := range opts {
		o(a)
	}
	return a
}

// Config returns the loaded config, loading it from the manager on first
// access. Falls back to Default when the file is missing so the app works
// zero-touch before `wiretap config init`.
func (a *App) Config() (*config.Config, error) {
	if a.cfg != nil {
		return a.cfg, nil
	}
	cfg, err := a.mgr.Load()
	if err != nil {
		def := config.Default()
		a.cfg = &def
		return a.cfg, nil
	}
	a.cfg = cfg
	return a.cfg, nil
}

// Store returns the PC store. Returns nil before Open.
func (a *App) Store() *store.PCStore { return a.store }

// Open opens (and migrates) the local SQLite database, resolving the path
// from the config (defaulting to <configDir>/wiretap.db). Idempotent: a
// second call is a no-op if the store is already open.
func (a *App) Open(ctx context.Context) error {
	if a.store != nil {
		return nil
	}
	cfg, err := a.Config()
	if err != nil {
		return err
	}
	path := cfg.Store.Path
	if path == "" {
		dir, err := a.mgr.Dir()
		if err != nil {
			return fmt.Errorf("app: resolve config dir: %w", err)
		}
		// Ensure the dir exists so SQLite can create the file (WAL mode needs a
		// writable directory for its sidecar files).
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("app: create config dir %s: %w", dir, err)
		}
		path = filepath.Join(dir, "wiretap.db")
	}
	db, err := store.Open(path)
	if err != nil {
		return fmt.Errorf("app: open store %s: %w", path, err)
	}
	if err := store.MigratePC(ctx, db); err != nil {
		_ = db.Close()
		return fmt.Errorf("app: migrate store: %w", err)
	}
	a.db = db
	a.store = store.NewPCStore(db)
	return nil
}

// Close stops the tunnel (if running) and closes the store. Safe to call
// multiple times.
func (a *App) Close() error {
	a.StopTunnel()
	var err error
	if a.db != nil {
		err = a.db.Close()
		a.db = nil
		a.store = nil
	}
	return err
}

// StartTunnel resolves the relay config + credentials and runs the tunnel in
// a background goroutine. It is a no-op (not an error) when the relay URL is
// empty or credentials are missing — the app still serves historical data.
// Returns an error only if the store is not open yet.
func (a *App) StartTunnel(ctx context.Context) error {
	if a.store == nil {
		return errors.New("app: store not open; call Open first")
	}
	cfg, err := a.Config()
	if err != nil {
		return err
	}
	if cfg.Relay.URL == "" {
		return nil // tunnel disabled
	}
	creds := a.creds
	if creds == nil {
		loaded, err := a.mgr.LoadCredentials()
		if err != nil {
			return nil // no credentials → tunnel disabled, not fatal
		}
		creds = loaded
	}
	if creds.ClientID == "" || creds.ClientToken == "" {
		return nil
	}

	tcfg := TunnelConfig{
		URL:         cfg.Relay.URL,
		ClientID:    creds.ClientID,
		ClientToken: creds.ClientToken,
		Projects:    creds.Projects,
	}
	runner := a.tunnelFactory(tcfg, a.store)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tunnelCancel != nil {
		return nil // already running
	}
	tctx, cancel := context.WithCancel(ctx)
	a.tunnelCtx = tctx
	a.tunnelCancel = cancel
	a.tunnelDone = make(chan struct{})
	go func() {
		defer close(a.tunnelDone)
		_ = runner.Run(tctx)
	}()
	return nil
}

// TunnelRunning reports whether the background relay tunnel goroutine is
// active. Used by the GUI status bar (read-only, lock-guarded).
func (a *App) TunnelRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tunnelCancel != nil
}

// ConnectedProjects returns a snapshot of the project paths the relay says this
// client owns, set by the tunnel's OnConnect callback. Returns nil when no
// tunnel is attached (or before the first OK arrives). The GUI/TUI show this in
// their status bars so the user can see what the relay is actually routing to
// them — without having to trust the local credentials file.
func (a *App) ConnectedProjects() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.connectedProjects == nil {
		return nil
	}
	out := make([]string, len(a.connectedProjects))
	copy(out, a.connectedProjects)
	return out
}

// SetConnectedProjects overrides the cached "connected projects" snapshot. It
// is the write path used by the tunnel's OnConnect (production) and by tests
// that inject a noop tunnel which will never fire OnConnect.
func (a *App) SetConnectedProjects(p []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connectedProjects = p
}

// StopTunnel cancels the background tunnel and waits for it to exit. Safe to
// call when not running or after Close.
func (a *App) StopTunnel() {
	a.mu.Lock()
	cancel := a.tunnelCancel
	done := a.tunnelDone
	a.tunnelCancel = nil
	a.tunnelDone = nil
	a.tunnelCtx = nil
	a.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
}

// --- Querier surface (TUI / GUI / local API) ----------------------------

// Webhooks lists the most recent webhooks (newest-first), optionally filtered
// by project. Delegates to PCStore.
func (a *App) Webhooks(ctx context.Context, project string, limit int) ([]store.WebhookRow, error) {
	if a.store == nil {
		return nil, errors.New("app: store not open")
	}
	return a.store.Webhooks(ctx, project, limit)
}

// Captures lists the most recent traffic captures (newest-first).
func (a *App) Captures(ctx context.Context, limit int) ([]store.TrafficCaptureRow, error) {
	if a.store == nil {
		return nil, errors.New("app: store not open")
	}
	return a.store.TrafficCaptures(ctx, limit)
}

// WebhookBySeq loads one webhook by (project, seq). Returns store.ErrNotFound
// (wrapped) when absent.
func (a *App) WebhookBySeq(ctx context.Context, project string, seq int64) (*store.WebhookRow, error) {
	if a.store == nil {
		return nil, errors.New("app: store not open")
	}
	return a.store.WebhookBySeq(ctx, project, seq)
}

// InsertTrafficCapture appends a traffic capture. Returns the row id. Used by
// the interception proxy's recorder adapter.
func (a *App) InsertTrafficCapture(ctx context.Context, c store.TrafficCaptureRow) (int64, error) {
	if a.store == nil {
		return 0, errors.New("app: store not open")
	}
	return a.store.InsertTrafficCapture(ctx, c)
}

// ReplayWebhook re-POSTs a locally stored webhook to targetURL. The stored
// method, headers, and body are sent as-is; the response status is returned.
// This is a local replay (send to a dev server), distinct from the relay's
// tunnel re-push.
func (a *App) ReplayWebhook(ctx context.Context, project string, seq int64, targetURL string) (int, error) {
	wh, err := a.WebhookBySeq(ctx, project, seq)
	if err != nil {
		return 0, err
	}

	// Assemble the replay request from the stored webhook, dropping hop-by-hop
	// headers that don't belong on a fresh outbound request.
	method := wh.Method
	headers := http.Header{}
	for k, vs := range parseHeaders(wh.HeadersJSON) {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			headers.Add(k, v)
		}
	}
	body := wh.Body

	// Run on_replay scripts before re-POSTing so users can regenerate a
	// signature, bump a timestamp, or swap a token. A rejection aborts the
	// replay; a script error is reported but non-fatal.
	method, targetURL, headers, body, err = a.runReplayScripts(ctx, method, targetURL, headers, body)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("app: build replay request: %w", err)
	}
	req.Header = headers
	client := &http.Client{Transport: a.replayTransport, Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("app: replay to %s: %w", targetURL, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

// runReplayScripts runs enabled on_replay scripts against the outbound replay
// request, returning the (possibly rewritten) method/url/headers/body. With no
// engine or no scripts it returns the inputs unchanged. A reject() surfaces as
// a *ReplayRejectedError; per-script errors go to onScriptError and are
// otherwise swallowed so one bad script never blocks a replay.
func (a *App) runReplayScripts(ctx context.Context, method, url string, h http.Header, body []byte) (string, string, http.Header, []byte, error) {
	if a.scriptEngine == nil || a.store == nil {
		return method, url, h, body, nil
	}
	rows, err := a.store.ScriptsByTrigger(ctx, string(scripting.OnReplay), true)
	if err != nil {
		if a.onScriptError != nil {
			a.onScriptError(scripting.OnReplay, "", err)
		}
		return method, url, h, body, nil
	}
	if len(rows) == 0 {
		return method, url, h, body, nil
	}
	scripts := make([]scripting.Script, len(rows))
	for i, r := range rows {
		scripts[i] = scripting.Script{Name: r.Name, Trigger: scripting.OnReplay, Body: r.Body, Priority: r.Priority, Enabled: r.Enabled}
	}

	ex := &scripting.Exchange{}
	ex.SetRequest(method, url, h, body)
	chain := a.scriptEngine.RunChain(ctx, scripting.OnReplay, scripts, ex)
	if a.onScriptError != nil {
		for _, r := range chain.Results {
			if r.Err != nil {
				a.onScriptError(scripting.OnReplay, r.Name, r.Err)
			}
		}
	}
	if chain.Rejected {
		return method, url, h, body, &ReplayRejectedError{Reason: chain.RejectReason}
	}
	outMethod, outURL, outHeaders, outBody := ex.RequestParts()
	return outMethod, outURL, outHeaders, outBody, nil
}

// ReplayRejectedError is returned by ReplayWebhook when an on_replay script
// calls reject(reason), so the caller can distinguish a policy block from a
// transport failure.
type ReplayRejectedError struct{ Reason string }

func (e *ReplayRejectedError) Error() string {
	return fmt.Sprintf("app: on_replay script rejected the webhook: %s", e.Reason)
}

// --- helpers -----------------------------------------------------------

// defaultTunnelFactory wires relayclient.Client. It is the production default
// for the tunnelFactory seam. Because it is a method on *App, the relayclient
// callbacks can write back into App state: OnConnect gives us the list of
// projects the relay says this client owns (so the GUI/TUI can show it without
// re-reading the credentials file), and OnDisconnect clears it.
func (a *App) defaultTunnelFactory(cfg TunnelConfig, st *store.PCStore) TunnelRunner {
	return relayclient.New(
		relayclient.Config{
			URL:         cfg.URL,
			ClientID:    cfg.ClientID,
			ClientToken: cfg.ClientToken,
			Projects:    cfg.Projects,
		},
		st,
		relayclient.WithClock(testutil.SystemClock{}),
		relayclient.WithCallbacks(relayclient.Callbacks{
			OnConnect:    func(projects []string) { a.SetConnectedProjects(projects) },
			OnDisconnect: func(_ error) { a.SetConnectedProjects(nil) },
		}),
	)
}

// parseHeaders decodes the JSON-encoded http.Header stored in the webhooks
// table. Returns an empty header on any parse error (best-effort replay).
func parseHeaders(jsonStr string) http.Header {
	h := http.Header{}
	if jsonStr == "" {
		return h
	}
	// http.Header marshals as map[string][]string; unmarshal into that.
	if err := json.Unmarshal([]byte(jsonStr), &h); err != nil {
		return http.Header{}
	}
	return h
}

// isHopByHop reports whether k is an HTTP/1.1 hop-by-hop header that should
// not be forwarded on a replayed request.
func isHopByHop(k string) bool {
	switch http.CanonicalHeaderKey(k) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailers", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}
