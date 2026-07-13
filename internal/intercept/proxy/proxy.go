// Package proxy is the interception core that intercepts outbound HTTP/HTTPS
// traffic from a spawned shell. It speaks the standard forward-proxy protocol:
// clients configured with HTTP(S)_PROXY point here, and the proxy either
// forwards plain HTTP or — for HTTPS — upgrades a CONNECT to a TLS session it
// terminates locally (presenting a per-host leaf signed by the wiretap CA),
// then re-issues the request upstream over a separate TLS dial. Each
// request/response pair is recorded via an injected Recorder seam.
//
// The design keeps the few hard-to-test OS/time failures behind interfaces so
// the core flow can be exercised against an in-process httptest TLS upstream
// with fakes for the cert signer, the capture sink, and the upstream dial.
//
// Out of scope for the MVP (see PLAN.md §1 non-goals): non-HTTP/1.1 tunnels,
// plain-HTTP-on-port-80 capture beyond basic forward proxying, and
// hop-by-hop/chunked edge cases (deferred to Phase 6 hardening).
package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/plutack/wiretap/internal/intercept/castore"
	"github.com/plutack/wiretap/internal/testutil"
)

// Capture is one observed request/response pair. The proxy constructs it from
// the fully-decoded HTTP exchange and hands it to the Recorder; the recorder
// (production: a thin adapter over PCStore) is what persists it.
type Capture struct {
	At          time.Time
	Method      string
	URL         string
	ReqHeaders  http.Header
	ReqBody     []byte
	Status      int
	RespHeaders http.Header
	RespBody    []byte
}

// Recorder sinks captures. Production wraps PCStore; tests collect them
// in-process and assert on the recorded values.
type Recorder interface {
	Record(ctx context.Context, c Capture) error
}

// CertSigner mints a TLS leaf certificate for a host on demand during the TLS
// handshake that follows a CONNECT. Production uses CastoreSigner wrapping a
// *castore.CA; tests can inject a fake that returns canned certs.
type CertSigner interface {
	LeafCert(host string) (tls.Certificate, error)
}

// CastoreSigner adapts a *castore.CA to the CertSigner seam, filling in the
// current time and crypto/rand.Reader so callers stay free of crypto plumbery.
type CastoreSigner struct {
	CA *castore.CA
}

// NewCastoreSigner wraps a CA behind the CertSigner interface.
func NewCastoreSigner(ca *castore.CA) CertSigner { return &CastoreSigner{CA: ca} }

// LeafCert implements CertSigner.
func (s *CastoreSigner) LeafCert(host string) (tls.Certificate, error) {
	return s.CA.LeafCert(host, time.Now(), rand.Reader)
}

// UpstreamDialer returns a connected stream to addr (host:port as the client
// requested it). For HTTPS targets the default implementation returns a
// TLS-handshaked *tls.Conn trusted against the system roots (or an injected
// pool via WithUpstreamRoots). Putting upstream TLS behind a seam lets tests
// point the proxy at an in-process tls-terminated httptest server without
// touching the real trust store.
type UpstreamDialer interface {
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
}

// tlsDialer dials raw TCP then upgrades to TLS using the standard verifier,
// optionally trusting an extra RootCAs pool (used by tests).
type tlsDialer struct {
	roots   *x509.CertPool // nil → use the system roots
	timeout time.Duration
}

func defaultUpstreamDialer() UpstreamDialer {
	return &tlsDialer{timeout: 30 * time.Second}
}

// Dial implements UpstreamDialer.
func (d *tlsDialer) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	raw, err := (&net.Dialer{Timeout: d.timeout}).DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("proxy: dial upstream %s: %w", addr, err)
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("proxy: split host:port %s: %w", addr, err)
	}
	cfg := &tls.Config{ServerName: host, RootCAs: d.roots}
	tlsConn := tls.Client(raw, cfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("proxy: upstream TLS handshake %s: %w", addr, err)
	}
	return tlsConn, nil
}

// Proxy is the interception proxy server. Construct with New, then run it with Start
// (blocking) or StartAsync (test-friendly: binds synchronously, returns the
// resolved address, serves in a goroutine). Stop cancels serving and closes
// in-flight client connections.
type Proxy struct {
	addr      string
	signer    CertSigner
	recorder  Recorder
	dialer    UpstreamDialer
	clock     testutil.Clock
	roundTrip http.RoundTripper // for plain-HTTP forward proxying

	mu      sync.Mutex
	ln      net.Listener
	server  *http.Server
	conns   map[net.Conn]struct{}
	running bool
}

// Option configures a Proxy.
type Option func(*Proxy)

// WithClock injects a clock used to timestamp captures. Defaults to
// testutil.SystemClock.
func WithClock(c testutil.Clock) Option { return func(p *Proxy) { p.clock = c } }

// WithUpstreamDialer overrides the default TLS dialer (tests inject a fake
// that doesn't touch the network or that trusts a test CA pool).
func WithUpstreamDialer(d UpstreamDialer) Option {
	return func(p *Proxy) { p.dialer = d }
}

// WithUpstreamRoots makes the default TLS dialer trust the extra RootCAs pool
// in addition to (or instead of) the system roots. If a custom dialer is
// already set, this replaces it with a TLS dialer honouring the pool so the
// option always takes effect.
func WithUpstreamRoots(pool *x509.CertPool) Option {
	return func(p *Proxy) {
		if td, ok := p.dialer.(*tlsDialer); ok {
			td.roots = pool
			return
		}
		p.dialer = &tlsDialer{roots: pool, timeout: 30 * time.Second}
	}
}

// WithRoundTripper overrides the transport used for plain-HTTP forward proxying
// (the non-CONNECT path). Tests inject a transport pointed at an httptest
// server.
func WithRoundTripper(rt http.RoundTripper) Option {
	return func(p *Proxy) { p.roundTrip = rt }
}

// New wires the proxy. addr is the local listen address (use "127.0.0.1:0" in
// tests for a random free port). signer is required; recorder may be nil to
// drop captures (handy for smoke tests).
func New(addr string, signer CertSigner, recorder Recorder, opts ...Option) *Proxy {
	p := &Proxy{
		addr:      addr,
		signer:    signer,
		recorder:  recorder,
		dialer:    defaultUpstreamDialer(),
		clock:     testutil.SystemClock{},
		roundTrip: http.DefaultTransport.(*http.Transport).Clone(),
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Listen binds the port and seeds internal state but does not serve. Start
// calls it implicitly; exposing it lets tests reach Addr() synchronously.
func (p *Proxy) Listen() error {
	ln, err := net.Listen("tcp", p.addr)
	if err != nil {
		return fmt.Errorf("proxy: listen %s: %w", p.addr, err)
	}
	p.ln = ln
	p.conns = make(map[net.Conn]struct{})
	p.server = &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Track in-flight conns so Stop can abort hijacked tunnels abruptly.
	p.server.ConnState = func(c net.Conn, st http.ConnState) {
		switch st {
		case http.StateNew:
			p.trackConn(c, true)
		case http.StateClosed:
			p.trackConn(c, false)
			// StateHijacked is a no-op: we keep the conn tracked so Stop can kill
			// it, and remove it ourselves from the bridge's defer when it closes.
		}
	}
	return nil
}

// Start binds and serves until Stop or the listener is closed. Blocks.
func (p *Proxy) Start() error {
	if p.server == nil {
		if err := p.Listen(); err != nil {
			return err
		}
	}
	return p.server.Serve(p.ln)
}

// StartAsync binds synchronously, then serves in a goroutine, returning the
// resolved listen address. The address is stable by the time StartAsync
// returns, which is exactly what tests need to dial the proxy without polling.
func (p *Proxy) StartAsync() (string, error) {
	if err := p.Listen(); err != nil {
		return "", err
	}
	go func() { _ = p.server.Serve(p.ln) }()
	return p.ln.Addr().String(), nil
}

// Stop cancels serving and closes outstanding client connections (including
// hijacked tunnels). Calling Stop more than once, or without having started,
// is safe.
func (p *Proxy) Stop(ctx context.Context) error {
	p.mu.Lock()
	for c := range p.conns {
		_ = c.Close()
	}
	p.conns = make(map[net.Conn]struct{})
	srv := p.server
	p.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// Addr returns the resolved listen address, or the configured addr before
// Listen. Tests call it after StartAsync to dial the proxy.
func (p *Proxy) Addr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln != nil {
		return p.ln.Addr().String()
	}
	return p.addr
}

func (p *Proxy) trackConn(c net.Conn, add bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conns == nil {
		return
	}
	if add {
		p.conns[c] = struct{}{}
	} else {
		delete(p.conns, c)
	}
}

// ServeHTTP dispatches plain-HTTP forward proxying vs HTTPS CONNECT tunneling.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

// handleConnect terminates a CONNECT tunnel and intercepts the TLS inside it.
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	addr := r.URL.Host
	if addr == "" {
		addr = r.Host
	}
	host, _, _ := net.SplitHostPort(addr)

	upstream, err := p.dialer.Dial(ctx, "tcp", addr)
	if err != nil {
		http.Error(w, "wiretap: upstream unreachable: "+err.Error(), http.StatusBadGateway)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(w, "wiretap: hijacking unsupported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	defer func() {
		_ = clientConn.Close()
		_ = upstream.Close()
		p.trackConn(clientConn, false)
	}()

	// Tell the client the tunnel is up; we take over the bytes from here.
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}

	leaf, err := p.signer.LeafCert(host)
	if err != nil {
		return
	}
	clientTLS := tls.Server(clientConn, &tls.Config{Certificates: []tls.Certificate{leaf}})
	// After Hijack the server cancels r.Context(), so drive the handshake with a
	// fresh background context rather than a soon-dead one.
	if err := clientTLS.HandshakeContext(context.Background()); err != nil {
		return
	}
	defer func() { _ = clientTLS.Close() }()

	upstreamTLS, ok := upstream.(*tls.Conn)
	if !ok {
		// The default dialer always returns a *tls.Conn; a custom dialer that
		// hands back a plain conn for a CONNECT target is a misconfiguration.
		return
	}

	p.bridge(clientTLS, upstreamTLS, host)
}

// bridge shuttles HTTP/1.1 requests between the client (whose TLS we own) and
// the upstream (whose TLS the dialer owns), recording each exchange. The loop
// ends on EOF, a half-close, or a close/keep-alive-exhausted decision.
func (p *Proxy) bridge(clientTLS, upstreamTLS *tls.Conn, host string) {
	cr := bufio.NewReader(clientTLS)
	ur := bufio.NewReader(upstreamTLS)
	uw := bufio.NewWriter(upstreamTLS)
	cw := bufio.NewWriter(clientTLS)

	for {
		req, err := http.ReadRequest(cr)
		if err != nil {
			return
		}

		// Compute the absolute URL for capture (server-side reads have a
		// relative URL; the Host header carries the authority).
		u := *req.URL
		if u.Host == "" {
			u.Host = req.Host
		}
		if u.Host == "" {
			u.Host = host
		}
		if u.Scheme == "" {
			u.Scheme = "https"
		}

		reqBody, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return
		}

		// Re-issue the request upstream in origin form.
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
		req.ContentLength = int64(len(reqBody))
		req.TransferEncoding = nil
		if err := req.Write(uw); err != nil {
			return
		}
		if err := uw.Flush(); err != nil {
			return
		}

		resp, err := http.ReadResponse(ur, req)
		if err != nil {
			return
		}
		respBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return
		}

		// Record before writing back to the client so the capture is durable
		// by the time the caller observes the response.
		p.record(p.ctx(), Capture{
			At:          p.clock.Now(),
			Method:      req.Method,
			URL:         u.String(),
			ReqHeaders:  req.Header.Clone(),
			ReqBody:     append([]byte(nil), reqBody...),
			Status:      resp.StatusCode,
			RespHeaders: resp.Header.Clone(),
			RespBody:    append([]byte(nil), respBody...),
		})

		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		resp.ContentLength = int64(len(respBody))
		resp.TransferEncoding = nil
		if err := resp.Write(cw); err != nil {
			return
		}
		if err := cw.Flush(); err != nil {
			return
		}

		if resp.Close || req.Close {
			return
		}
	}
}

// ctx returns a background context for side-effecting calls; the request's own
// context is cancelled the instant we hijack, so we must not reuse it for the
// recorder (which may outlive the request slightly).
func (p *Proxy) ctx() context.Context { return context.Background() }

// handleHTTP forwards a plain-HTTP proxy request (one whose URL is already
// absolute: http://host/path). Plain-HTTP interception is limited in the MVP
// (HTTPS via CONNECT is the primary path); this still records the exchange.
func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	reqBody, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()

	outReq := r.Clone(ctx)
	outReq.RequestURI = "" // round-trippers reject a set RequestURI
	outReq.Body = io.NopCloser(bytes.NewReader(reqBody))
	outReq.ContentLength = int64(len(reqBody))
	outReq.TransferEncoding = nil

	resp, err := p.roundTrip.RoundTrip(outReq)
	if err != nil {
		http.Error(w, "wiretap: upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)

	p.record(ctx, Capture{
		At:          p.clock.Now(),
		Method:      r.Method,
		URL:         r.URL.String(),
		ReqHeaders:  r.Header.Clone(),
		ReqBody:     append([]byte(nil), reqBody...),
		Status:      resp.StatusCode,
		RespHeaders: resp.Header.Clone(),
		RespBody:    append([]byte(nil), respBody...),
	})

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func (p *Proxy) record(ctx context.Context, c Capture) {
	if p.recorder == nil {
		return
	}
	_ = p.recorder.Record(ctx, c)
}
