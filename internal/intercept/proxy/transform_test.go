package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/intercept/castore"
	"github.com/plutack/wiretap/internal/testutil"
)

// fakeTransformer is a test Transformer driven by injected functions. A nil
// func means "identity" for that half.
type fakeTransformer struct {
	req  func(ReqEdit) (ReqEdit, error)
	resp func(RespEdit) (RespEdit, error)
}

func (f fakeTransformer) TransformRequest(_ context.Context, in ReqEdit) (ReqEdit, error) {
	if f.req == nil {
		return in, nil
	}
	return f.req(in)
}

func (f fakeTransformer) TransformResponse(_ context.Context, in RespEdit) (RespEdit, error) {
	if f.resp == nil {
		return in, nil
	}
	return f.resp(in)
}

// startProxyWithTransformer mirrors startProxyInTestForUpstream but installs a
// Transformer so the request/response rewrite path is exercised end to end.
func startProxyWithTransformer(t *testing.T, rec Recorder, upRoots *x509.CertPool, tr Transformer) (*Proxy, *x509.CertPool) {
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
		WithUpstreamRoots(upRoots),
		WithTransformer(tr))
	if _, err := p.StartAsync(); err != nil {
		t.Fatalf("StartAsync: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })
	return p, interceptPool
}

func proxyClient(t *testing.T, proxyAddr string, roots *x509.CertPool) *http.Client {
	t.Helper()
	return &http.Client{
		Transport: &http.Transport{
			Proxy:           func(*http.Request) (*url.URL, error) { return url.Parse("http://" + proxyAddr) },
			TLSClientConfig: &tls.Config{RootCAs: roots},
		},
		Timeout: 10 * time.Second,
	}
}

func TestProxy_TransformerRewritesRequest(t *testing.T) {
	t.Parallel()

	// The upstream echoes what it received so we can prove the rewrite landed.
	var gotHeader, gotBody string
	upstream, upRoots := startTLSUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Injected")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	tr := fakeTransformer{req: func(in ReqEdit) (ReqEdit, error) {
		in.Headers.Set("X-Injected", "from-script")
		in.Body = []byte("rewritten")
		return in, nil
	}}
	rec := &captureCollector{}
	p, interceptPool := startProxyWithTransformer(t, rec, upRoots, tr)

	resp, err := proxyClient(t, p.Addr(), interceptPool).Post(upstream.URL+"/x", "text/plain", bytes.NewReader([]byte("original")))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()

	if gotHeader != "from-script" {
		t.Errorf("upstream saw X-Injected = %q, want from-script", gotHeader)
	}
	if gotBody != "rewritten" {
		t.Errorf("upstream saw body = %q, want rewritten", gotBody)
	}
	// The capture records the post-transform request.
	caps := rec.List()
	if len(caps) != 1 {
		t.Fatalf("captures = %d, want 1", len(caps))
	}
	if string(caps[0].ReqBody) != "rewritten" {
		t.Errorf("capture ReqBody = %q, want rewritten", caps[0].ReqBody)
	}
	if caps[0].ReqHeaders.Get("X-Injected") != "from-script" {
		t.Errorf("capture missing injected header")
	}
}

func TestProxy_TransformerRewritesResponse(t *testing.T) {
	t.Parallel()

	upstream, upRoots := startTLSUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("upstream-body"))
	}))

	tr := fakeTransformer{resp: func(in RespEdit) (RespEdit, error) {
		in.Status = http.StatusTeapot
		in.Headers.Set("X-Rewritten", "yes")
		in.Body = []byte("client-sees-this")
		return in, nil
	}}
	rec := &captureCollector{}
	p, interceptPool := startProxyWithTransformer(t, rec, upRoots, tr)

	resp, err := proxyClient(t, p.Addr(), interceptPool).Get(upstream.URL + "/y")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want 418", resp.StatusCode)
	}
	if resp.Header.Get("X-Rewritten") != "yes" {
		t.Errorf("missing rewritten header")
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "client-sees-this" {
		t.Errorf("client body = %q, want client-sees-this", body)
	}
	caps := rec.List()
	if len(caps) != 1 || caps[0].Status != http.StatusTeapot {
		t.Fatalf("capture = %+v, want status 418", caps)
	}
}

func TestProxy_TransformerRequestErrorDropsExchange(t *testing.T) {
	t.Parallel()

	upstream, upRoots := startTLSUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tr := fakeTransformer{req: func(in ReqEdit) (ReqEdit, error) {
		return in, errors.New("rejected")
	}}
	rec := &captureCollector{}
	p, interceptPool := startProxyWithTransformer(t, rec, upRoots, tr)

	// A request-transform error aborts the HTTPS bridge, so the client's
	// request fails (connection closed) rather than reaching the upstream.
	_, err := proxyClient(t, p.Addr(), interceptPool).Get(upstream.URL + "/z")
	if err == nil {
		t.Fatal("expected error when request transform aborts the exchange")
	}
	if caps := rec.List(); len(caps) != 0 {
		t.Errorf("captures = %d, want 0 (exchange aborted before record)", len(caps))
	}
}
