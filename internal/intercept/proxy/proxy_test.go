package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/intercept/castore"
	"github.com/plutack/wiretap/internal/testutil"
)

// captureCollector is the test Recorder: it gathers Captures in-process so
// assertions can inspect exactly what the proxy observed. Mutex-guarded
// because the bridge records from the proxy's serve goroutine while the test
// reads from the test goroutine.
type captureCollector struct {
	mu   sync.Mutex
	caps []Capture
}

func (c *captureCollector) Record(_ context.Context, cap Capture) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.caps = append(c.caps, cap)
	return nil
}

func (c *captureCollector) List() []Capture {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Capture, len(c.caps))
	copy(out, c.caps)
	return out
}

// startProxyInTest builds a proxy backed by a freshly minted interception CA,
// starts it on a random loopback port, and returns the proxy plus the CA's
// root pool (so the test client can trust the leaf certs it presents). Cleanup
// closes the proxy.
func startProxyInTest(t *testing.T, rec Recorder) (*Proxy, *x509.CertPool) {
	t.Helper()
	now := time.Now().UTC()
	interceptCA, err := castore.GenerateCA(now, rand.Reader)
	if err != nil {
		t.Fatalf("GenerateCA interception: %v", err)
	}
	interceptPool := x509.NewCertPool()
	interceptPool.AddCert(interceptCA.Cert)

	p := New("127.0.0.1:0", NewCastoreSigner(interceptCA), rec, WithClock(&testutil.FakeClock{T: now}))
	addr, err := p.StartAsync()
	if err != nil {
		t.Fatalf("StartAsync: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })
	_ = addr
	return p, interceptPool
}

// startTLSUpstream builds an httptest TLS server presenting a leaf signed by a
// CA we mint, and returns the server plus the root pool that trusts it (so the
// proxy's upstream dialer can verify it via WithUpstreamRoots).
func startTLSUpstream(t *testing.T, h http.Handler) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	now := time.Now().UTC()
	upCA, err := castore.GenerateCA(now, rand.Reader)
	if err != nil {
		t.Fatalf("GenerateCA upstream: %v", err)
	}
	leaf, err := upCA.LeafCert("127.0.0.1", now, rand.Reader)
	if err != nil {
		t.Fatalf("upstream LeafCert: %v", err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{leaf}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(upCA.Cert)
	return srv, pool
}

func TestProxy_InterceptsHTTPS(t *testing.T) {
	t.Parallel()

	const echoBody = "upstream-says-hi"
	upstream, upRoots := startTLSUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Echoed", "yes")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(append([]byte("echo:"), b...))
	}))

	rec := &captureCollector{}
	p, interceptPool := startProxyInTestForUpstream(t, rec, upRoots)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) {
				return url.Parse("http://" + p.Addr())
			},
			TLSClientConfig: &tls.Config{RootCAs: interceptPool},
		},
		Timeout: 10 * time.Second,
	}

	resp, err := client.Post(upstream.URL+"/echo", "text/plain", bytes.NewReader([]byte(echoBody)))
	if err != nil {
		t.Fatalf("client.Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Echoed"); got != "yes" {
		t.Errorf("X-Echoed = %q, want yes", got)
	}
	got, _ := io.ReadAll(resp.Body)
	if want := "echo:" + echoBody; string(got) != want {
		t.Errorf("resp body = %q, want %q", got, want)
	}

	caps := rec.List()
	if len(caps) != 1 {
		t.Fatalf("captures = %d, want 1", len(caps))
	}
	c := caps[0]
	if c.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", c.Method)
	}
	if !strings.HasSuffix(c.URL, "/echo") || !strings.HasPrefix(c.URL, "https://") {
		t.Errorf("url = %q", c.URL)
	}
	if string(c.ReqBody) != echoBody {
		t.Errorf("req body = %q, want %q", c.ReqBody, echoBody)
	}
	if c.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", c.Status)
	}
	if string(c.RespBody) != "echo:"+echoBody {
		t.Errorf("resp body = %q", c.RespBody)
	}
	if got := c.RespHeaders.Get("X-Echoed"); got != "yes" {
		t.Errorf("recorded X-Echoed = %q", got)
	}
}

// startProxyInTestForUpstream is startProxyInTest that also points the proxy at
// the upstream-root pool so its dialer trusts the httptest upstream cert.
func startProxyInTestForUpstream(t *testing.T, rec Recorder, upRoots *x509.CertPool) (*Proxy, *x509.CertPool) {
	t.Helper()
	now := time.Now().UTC()
	interceptCA, err := castore.GenerateCA(now, rand.Reader)
	if err != nil {
		t.Fatalf("GenerateCA interception: %v", err)
	}
	interceptPool := x509.NewCertPool()
	interceptPool.AddCert(interceptCA.Cert)

	p := New("127.0.0.1:0",
		NewCastoreSigner(interceptCA), rec,
		WithClock(&testutil.FakeClock{T: now}),
		WithUpstreamRoots(upRoots))
	if _, err := p.StartAsync(); err != nil {
		t.Fatalf("StartAsync: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })
	return p, interceptPool
}

func TestProxy_PlaintextForwarding(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Echoed", "plain")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write(append([]byte("body:"), b...))
	}))
	t.Cleanup(upstream.Close)

	rec := &captureCollector{}
	now := time.Now().UTC()
	interceptCA, err := castore.GenerateCA(now, rand.Reader)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	p := New("127.0.0.1:0",
		NewCastoreSigner(interceptCA), rec,
		WithClock(&testutil.FakeClock{T: now}))
	if _, err := p.StartAsync(); err != nil {
		t.Fatalf("StartAsync: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })

	// For plain-HTTP forwarding the proxy uses its own round-tripper to reach
	// the upstream. The default is a clone of http.DefaultTransport, which is
	// fine for a normal htt plaintext httptest server.
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) {
				return url.Parse("http://" + p.Addr())
			},
		},
		Timeout: 10 * time.Second,
	}

	resp, err := client.Post(upstream.URL+"/ping", "text/plain", bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatalf("client.Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", resp.StatusCode)
	}

	caps := rec.List()
	if len(caps) != 1 {
		t.Fatalf("captures = %d, want 1", len(caps))
	}
	c := caps[0]
	if c.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", c.Method)
	}
	if !strings.HasSuffix(c.URL, "/ping") {
		t.Errorf("url = %q", c.URL)
	}
	if c.Status != http.StatusTeapot {
		t.Errorf("recorded status = %d, want 418", c.Status)
	}
}

func TestProxy_StopWithoutStartIsSafe(t *testing.T) {
	t.Parallel()
	p := New("127.0.0.1:0", NewCastoreSigner(nil), nil)
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop before Start: %v", err)
	}
}
