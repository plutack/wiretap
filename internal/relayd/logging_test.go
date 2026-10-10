package relayd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/plutack/wiretap/internal/relayproto"

	"github.com/plutack/wiretap/internal/logging"
)

type logBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *logBuffer) text() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }

func TestRequestLoggingPrivacyAndCorrelation(t *testing.T) {
	var out logBuffer
	logger, _ := logging.New(&out, "json", "debug")
	srv, _, _ := freshServer(t, WithLogger(logger))
	makeClientFor(t, srv, "client-safe", "client-secret", "project-safe")
	r := httptest.NewRequest(http.MethodPost, "http://relay/project-safe/private-path?token=query-secret", strings.NewReader("payload-secret"))
	r.Header.Set("Authorization", "Bearer auth-secret")
	r.Header.Set("Cookie", "cookie-secret")
	r.Header.Set("X-Request-ID", "untrusted-secret")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	id := w.Header().Get("X-Request-ID")
	if len(id) != 32 {
		t.Fatal("missing generated correlation id", id)
	}
	text := out.text()
	for _, secret := range []string{"private-path", "query-secret", "payload-secret", "auth-secret", "cookie-secret", "untrusted-secret", "client-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("leaked %s: %s", secret, text)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["request_id"] != id {
			t.Fatalf("uncorrelated record: %v", record)
		}
	}
	if !strings.Contains(text, "ingress_persisted") || !strings.Contains(text, "http_request_completed") {
		t.Fatal(text)
	}
}

func TestRequestLogLevels(t *testing.T) {
	var out logBuffer
	logger, _ := logging.New(&out, "json", "info")
	srv, _, _ := freshServer(t, WithLogger(logger))
	for _, path := range []string{"/health", "/admin/clients", "/missing"} {
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	}
	text := out.text()
	if strings.Contains(text, "GET /health") {
		t.Fatal("health poll logged at info")
	}
	if !strings.Contains(text, "WARN") || !strings.Contains(text, "401") || !strings.Contains(text, "404") {
		t.Fatal(text)
	}
}

func TestTunnelLoggingDoesNotLeakPeerErrors(t *testing.T) {
	var out logBuffer
	logger, _ := logging.New(&out, "json", "debug")
	srv, hs, _ := freshServer(t, WithLogger(logger))
	makeClientFor(t, srv, "client-safe", "auth-secret", "project-safe")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialTunnel(t, hs, "client-safe", "auth-secret")
	writeTunnel(t, ctx, conn, relayproto.Hello{Base: relayproto.Base{Type: relayproto.TypeHello}, ClientID: "client-safe", ClientToken: "auth-secret"})
	if _, ok := readTunnel(t, ctx, conn).(relayproto.OK); !ok {
		t.Fatal("expected greeting")
	}
	_ = conn.Close(websocket.StatusNormalClosure, "peer-close-secret")
	// A bounded condition wait synchronizes with the server handler's deferred
	// disconnect log; the client close handshake alone doesn't join the handler.
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !strings.Contains(out.text(), "tunnel_disconnected") {
		select {
		case <-ctx.Done():
			t.Fatal("missing disconnect event")
		case <-ticker.C:
		}
	}
	text := out.text()
	if !strings.Contains(text, "tunnel_connected") {
		t.Fatal(text)
	}
	for _, secret := range []string{"auth-secret", "peer-close-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("leaked %s: %s", secret, text)
		}
	}
}

func TestResponseWriterPreservesStatusAndCapabilities(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &logResponseWriter{ResponseWriter: w}
	rw.WriteHeader(http.StatusCreated)
	rw.WriteHeader(http.StatusInternalServerError)
	if _, err := rw.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if rw.status != http.StatusCreated || w.Code != http.StatusCreated {
		t.Fatal("status overwritten")
	}
	if rw.Unwrap() != w {
		t.Fatal("underlying writer hidden")
	}
	if _, _, err := rw.Hijack(); err == nil {
		t.Fatal("recorder cannot hijack")
	}
}
