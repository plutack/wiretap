package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/logging"
)

func TestAppLoggingLifecycleAndRequestPrivacy(t *testing.T) {
	var out bytes.Buffer
	logger, _ := logging.New(&out, "json", "debug")
	a := New(config.NewManager(config.WithBaseDir(t.TempDir())), WithLogger(logger))
	if err := a.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); w.WriteHeader(201) }))
	defer server.Close()
	if _, err := a.SendRequest(context.Background(), "POST", server.URL+"/private-path?token=query-secret", http.Header{"Authorization": []string{"auth-secret"}}, []byte("payload-secret"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendRequest(context.Background(), "POST", "not-valid-secret", nil, nil, false); err == nil {
		t.Fatal("expected error")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, event := range []string{"client_store_opened", "composed_request_completed", "composed_request_failed", "client_store_closed"} {
		if !strings.Contains(text, event) {
			t.Fatal("missing", event, text)
		}
	}
	for _, secret := range []string{"private-path", "query-secret", "auth-secret", "payload-secret", "not-valid-secret"} {
		if strings.Contains(text, secret) {
			t.Fatal("leaked", secret)
		}
	}
}
