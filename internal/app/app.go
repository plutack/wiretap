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

	db    *sql.DB
	store *store.PCStore

	mu           sync.Mutex
	tunnelCtx    context.Context
	tunnelCancel context.CancelFunc
	tunnelDone   chan struct{}
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

// New builds an App wired to the given config manager. The manager is the path
// resolver for the store default and the credentials file; Open and
// StartTunnel load from it lazily unless overridden via options.
func New(mgr *config.Manager, opts ...Option) *App {
	a := &App{
		mgr:             mgr,
		clock:           testutil.SystemClock{},
		tunnelFactory:   defaultTunnelFactory,
		replayTransport: http.DefaultTransport.(*http.Transport).Clone(),
	}
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
	req, err := http.NewRequestWithContext(ctx, wh.Method, targetURL, bytes.NewReader(wh.Body))
	if err != nil {
		return 0, fmt.Errorf("app: build replay request: %w", err)
	}
	// Re-apply the stored headers (Content-Length is set by NewReaderBody).
	// Skip Hop-by-hop headers that don't belong on a fresh outbound request.
	for k, vs := range parseHeaders(wh.HeadersJSON) {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	client := &http.Client{Transport: a.replayTransport, Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("app: replay to %s: %w", targetURL, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

// --- helpers -----------------------------------------------------------

// defaultTunnelFactory wires relayclient.Client. It is the production default
// for the tunnelFactory seam.
func defaultTunnelFactory(cfg TunnelConfig, st *store.PCStore) TunnelRunner {
	return relayclient.New(
		relayclient.Config{
			URL:         cfg.URL,
			ClientID:    cfg.ClientID,
			ClientToken: cfg.ClientToken,
			Projects:    cfg.Projects,
		},
		st,
		relayclient.WithClock(testutil.SystemClock{}),
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
