package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/store"
)

// newTestApp builds an App backed by a temp-dir config manager. Callers get
// the base dir back so they can assert on file paths.
func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	base := t.TempDir()
	mgr := config.NewManager(config.WithBaseDir(base))
	a := New(mgr)
	return a, base
}

// openTestApp is newTestApp + Open, returning the resolved store path.
func openTestApp(t *testing.T) (*App, string) {
	t.Helper()
	a, base := newTestApp(t)
	if err := a.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, base
}

func TestApp_OpenCreatesStore(t *testing.T) {
	t.Parallel()
	a, base := openTestApp(t)
	if a.Store() == nil {
		t.Fatal("Store() nil after Open")
	}
	// The store path defaulted to <configDir>/wiretap.db.
	dir, _ := a.mgr.Dir()
	wantPath := filepath.Join(dir, "wiretap.db")
	_ = base
	if _, err := store.Open(wantPath); err != nil {
		t.Errorf("default store path %s not openable: %v", wantPath, err)
	}
}

func TestApp_OpenIsIdempotent(t *testing.T) {
	t.Parallel()
	a, _ := newTestApp(t)
	if err := a.Open(context.Background()); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	st1 := a.Store()
	if err := a.Open(context.Background()); err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if st2 := a.Store(); st2 != st1 {
		t.Error("second Open returned a different store")
	}
	t.Cleanup(func() { _ = a.Close() })
}

func TestApp_Config_FallsBackToDefault(t *testing.T) {
	t.Parallel()
	a, _ := newTestApp(t) // no config.yaml written
	cfg, err := a.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:8888" {
		t.Errorf("ListenAddr = %q, want default 127.0.0.1:8888", cfg.ListenAddr)
	}
	if cfg.Intercept.ProxyAddr != "127.0.0.1:8888" {
		t.Errorf("Intercept.ProxyAddr = %q", cfg.Intercept.ProxyAddr)
	}
}

// noopTunnelRunner is the test double for the tunnel seam: it records that Run
// was called and blocks until ctx is cancelled.
type noopTunnelRunner struct {
	started atomic.Int32
}

func (n *noopTunnelRunner) Run(ctx context.Context) error {
	n.started.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

func TestApp_StartTunnel_NoopWhenURLMissing(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	// Default config has Relay.URL = "".
	var called atomic.Int32
	a.tunnelFactory = func(TunnelConfig, *store.PCStore) TunnelRunner {
		called.Add(1)
		return &noopTunnelRunner{}
	}
	if err := a.StartTunnel(context.Background()); err != nil {
		t.Fatalf("StartTunnel: %v", err)
	}
	if called.Load() != 0 {
		t.Error("tunnel factory called despite empty URL")
	}
}

func TestApp_StartTunnel_NoopWhenCredentialsMissing(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	def := config.Default()
	def.Relay.URL = "wss://relay.example.com/tunnel"
	a.cfg = &def

	var called atomic.Int32
	a.tunnelFactory = func(TunnelConfig, *store.PCStore) TunnelRunner {
		called.Add(1)
		return &noopTunnelRunner{}
	}
	if err := a.StartTunnel(context.Background()); err != nil {
		t.Fatalf("StartTunnel: %v", err)
	}
	if called.Load() != 0 {
		t.Error("tunnel factory called despite missing credentials")
	}
}

func TestApp_StartTunnel_RunsAndStops(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	def := config.Default()
	def.Relay.URL = "wss://relay.example.com/tunnel"
	a.cfg = &def
	a.creds = &config.Credentials{
		ClientID:    "cid",
		ClientToken: "ctok",
		Projects:    []string{"project-a"},
	}

	runner := &noopTunnelRunner{}
	var gotCfg TunnelConfig
	a.tunnelFactory = func(cfg TunnelConfig, st *store.PCStore) TunnelRunner {
		gotCfg = cfg
		return runner
	}

	if err := a.StartTunnel(context.Background()); err != nil {
		t.Fatalf("StartTunnel: %v", err)
	}
	// Give the goroutine a moment to start.
	time.Sleep(50 * time.Millisecond)
	if runner.started.Load() != 1 {
		t.Error("tunnel runner not started")
	}
	if gotCfg.URL != def.Relay.URL || gotCfg.ClientID != "cid" || gotCfg.ClientToken != "ctok" {
		t.Errorf("tunnel config = %+v", gotCfg)
	}
	if len(gotCfg.Projects) != 1 || gotCfg.Projects[0] != "project-a" {
		t.Errorf("projects = %v", gotCfg.Projects)
	}

	// Double Start is a no-op.
	if err := a.StartTunnel(context.Background()); err != nil {
		t.Fatalf("second StartTunnel: %v", err)
	}

	a.StopTunnel()
	// Double Stop is safe.
	a.StopTunnel()
}

func TestApp_QuerierSurface(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	ctx := context.Background()

	// Insert a webhook + a capture, then read them back through the App.
	_, err := a.store.StoreWebhook(ctx, store.WebhookRow{
		Project: "project-a", Seq: 1, Method: "POST", Path: "/hook",
		HeadersJSON: `{"Content-Type":["application/json"]}`,
		Body:        []byte(`{"k":"v"}`),
		ReceivedAt:  time.Unix(100, 0).UTC(),
	}, time.Unix(101, 0).UTC())
	if err != nil {
		t.Fatalf("StoreWebhook: %v", err)
	}
	id, err := a.InsertTrafficCapture(ctx, store.TrafficCaptureRow{
		Method: "GET", URL: "https://x/y", Status: 200,
	})
	if err != nil {
		t.Fatalf("InsertTrafficCapture: %v", err)
	}
	if id == 0 {
		t.Error("capture id = 0")
	}

	whs, err := a.Webhooks(ctx, "", 10)
	if err != nil {
		t.Fatalf("Webhooks: %v", err)
	}
	if len(whs) != 1 || whs[0].Project != "project-a" {
		t.Errorf("webhooks = %+v", whs)
	}
	wh, err := a.WebhookBySeq(ctx, "project-a", 1)
	if err != nil {
		t.Fatalf("WebhookBySeq: %v", err)
	}
	if wh.Method != "POST" {
		t.Errorf("method = %q", wh.Method)
	}
	caps, err := a.Captures(ctx, 10)
	if err != nil {
		t.Fatalf("Captures: %v", err)
	}
	if len(caps) != 1 || caps[0].URL != "https://x/y" {
		t.Errorf("captures = %+v", caps)
	}

	// WebhookBySeq on missing row → ErrNotFound.
	if _, err := a.WebhookBySeq(ctx, "project-a", 999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing webhook: err = %v, want ErrNotFound", err)
	}
}

func TestApp_ReplayWebhook(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	ctx := context.Background()

	// Store a webhook.
	_, err := a.store.StoreWebhook(ctx, store.WebhookRow{
		Project: "project-a", Seq: 1, Method: "POST", Path: "/hook",
		HeadersJSON: `{"X-Custom":["yes"],"Content-Type":["application/json"]}`,
		Body:        []byte(`{"hello":"world"}`),
		ReceivedAt:  time.Unix(100, 0).UTC(),
	}, time.Unix(101, 0).UTC())
	if err != nil {
		t.Fatalf("StoreWebhook: %v", err)
	}

	// Upstream that echoes the received method, headers, body.
	var gotMethod, gotXCustom, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotXCustom = r.Header.Get("X-Custom")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(upstream.Close)

	a.replayTransport = http.DefaultTransport // use real transport for httptest
	status, err := a.ReplayWebhook(ctx, "project-a", 1, upstream.URL+"/echo")
	if err != nil {
		t.Fatalf("ReplayWebhook: %v", err)
	}
	if status != http.StatusAccepted {
		t.Errorf("status = %d, want 202", status)
	}
	if gotMethod != "POST" {
		t.Errorf("upstream method = %q, want POST", gotMethod)
	}
	if gotXCustom != "yes" {
		t.Errorf("upstream X-Custom = %q, want yes", gotXCustom)
	}
	if gotBody != `{"hello":"world"}` {
		t.Errorf("upstream body = %q", gotBody)
	}
}

func TestApp_ReplayWebhook_HopByHopStripped(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	ctx := context.Background()

	// Store a webhook whose headers include hop-by-hop + content-length.
	_, err := a.store.StoreWebhook(ctx, store.WebhookRow{
		Project: "p", Seq: 1, Method: "POST", Path: "/",
		HeadersJSON: `{"Connection":["keep-alive"],"Transfer-Encoding":["chunked"],"X-Keep":["yes"]}`,
		Body:        []byte("body"),
		ReceivedAt:  time.Unix(1, 0).UTC(),
	}, time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatalf("StoreWebhook: %v", err)
	}

	var sawConn, sawTE, sawXKeep string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawConn = r.Header.Get("Connection")
		sawTE = r.Header.Get("Transfer-Encoding")
		sawXKeep = r.Header.Get("X-Keep")
		w.WriteHeader(200)
	}))
	t.Cleanup(upstream.Close)

	a.replayTransport = http.DefaultTransport
	if _, err := a.ReplayWebhook(ctx, "p", 1, upstream.URL+"/"); err != nil {
		t.Fatalf("ReplayWebhook: %v", err)
	}
	if sawConn != "" {
		t.Errorf("Connection header forwarded: %q", sawConn)
	}
	if sawTE != "" {
		t.Errorf("Transfer-Encoding forwarded: %q", sawTE)
	}
	if sawXKeep != "yes" {
		t.Errorf("X-Keep stripped: got %q, want yes", sawXKeep)
	}
}

func TestApp_CloseStopsTunnel(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	def := config.Default()
	def.Relay.URL = "wss://relay.example.com/tunnel"
	a.cfg = &def
	a.creds = &config.Credentials{ClientID: "c", ClientToken: "t", Projects: []string{"p"}}

	runner := &noopTunnelRunner{}
	a.tunnelFactory = func(TunnelConfig, *store.PCStore) TunnelRunner { return runner }

	if err := a.StartTunnel(context.Background()); err != nil {
		t.Fatalf("StartTunnel: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if runner.started.Load() != 1 {
		t.Fatal("runner not started")
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// After Close, the runner's ctx is cancelled (Run returned ctx.Err()).
	// A second Close is safe.
	if err := a.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestApp_ErrorsBeforeOpen(t *testing.T) {
	t.Parallel()
	a, _ := newTestApp(t)
	ctx := context.Background()
	if _, err := a.Webhooks(ctx, "", 10); err == nil {
		t.Error("Webhooks before Open: want error, got nil")
	}
	if _, err := a.Captures(ctx, 10); err == nil {
		t.Error("Captures before Open: want error, got nil")
	}
	if err := a.StartTunnel(ctx); err == nil {
		t.Error("StartTunnel before Open: want error, got nil")
	}
}
