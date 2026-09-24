package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
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

// TestProxy_RefusesToProxyToItself pins the loop guard. NO_PROXY normally keeps
// clients from aiming at wiretap's own listeners, but not every HTTP client
// honours host:port entries in it, and proxying to ourselves would recurse:
// dial our own listener, re-issue the request, dial again.
//
// Requests are written raw because Go's own client refuses to send loopback
// destinations through a proxy, so it cannot express this case.
func TestProxy_RefusesToProxyToItself(t *testing.T) {
	t.Parallel()
	p, _ := startProxyInTest(t, &captureCollector{})
	self := p.Addr()

	// A loopback port that is not ours must still be forwarded (and simply fail
	// to connect), proving the guard keys on our own port rather than on
	// "loopback" as a category.
	if _, port, err := net.SplitHostPort(self); err == nil && port == "1" {
		t.Skip("proxy happened to bind port 1")
	}

	cases := []struct {
		name       string
		request    string
		wantStatus int
	}{
		{
			name:       "plain http aimed at the proxy",
			request:    "GET http://" + self + "/ HTTP/1.1\r\nHost: " + self + "\r\n\r\n",
			wantStatus: http.StatusLoopDetected,
		},
		{
			name:       "connect aimed at the proxy",
			request:    "CONNECT " + self + " HTTP/1.1\r\nHost: " + self + "\r\n\r\n",
			wantStatus: http.StatusLoopDetected,
		},
		{
			name:       "another loopback port is forwarded, not refused",
			request:    "GET http://127.0.0.1:1/ HTTP/1.1\r\nHost: 127.0.0.1:1\r\n\r\n",
			wantStatus: http.StatusBadGateway,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn, err := net.Dial("tcp", self)
			if err != nil {
				t.Fatalf("dial proxy: %v", err)
			}
			defer conn.Close()
			if _, err := conn.Write([]byte(tc.request)); err != nil {
				t.Fatalf("write request: %v", err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}
