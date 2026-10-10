package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/store"
)

type stubbornTunnel struct {
	release <-chan struct{}
	started chan struct{}
}

func (s stubbornTunnel) Run(context.Context) error { close(s.started); <-s.release; return nil }

func TestShutdownTimeoutKeepsStoreUntilTunnelJoined(t *testing.T) {
	release, started := make(chan struct{}), make(chan struct{})
	cfg := config.Default()
	cfg.Relay.URL = "ws://relay.invalid/tunnel"
	a := New(config.NewManager(config.WithBaseDir(t.TempDir())), WithConfig(&cfg), WithCredentials(&config.Credentials{ClientID: "id", ClientToken: "token"}), WithTunnelFactory(func(TunnelConfig, *store.PCStore) TunnelRunner { return stubbornTunnel{release, started} }))
	if err := a.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.StartTunnel(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := a.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if a.Store() == nil {
		t.Fatal("closed store before worker exit")
	}
	close(release)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if a.Store() != nil {
		t.Fatal("store remains open")
	}
	if err := a.StartTunnel(context.Background()); err == nil {
		t.Fatal("started after closing")
	}
}

func TestShutdownForceCancelsRequestAndRetryClosesStore(t *testing.T) {
	a, _ := openTestApp(t)
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer server.Close()
	done := make(chan error, 1)
	go func() { _, err := a.SendRequest(context.Background(), "GET", server.URL, nil, nil, false); done <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := a.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("request not cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("request did not stop")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if a.Store() != nil {
		t.Fatal("store not closed after forced request joined")
	}
}

func TestShutdownDrainsAcceptedRequestAndRejectsNew(t *testing.T) {
	a, _ := openTestApp(t)
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(201) }))
	defer server.Close()
	requestDone := make(chan error, 1)
	go func() {
		_, err := a.SendRequest(context.Background(), "POST", server.URL, nil, nil, false)
		requestDone <- err
	}()
	<-entered
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- a.Close() }()
	// Synchronize against the lifecycle admission lock, not a timing sleep.
	for {
		a.mu.Lock()
		closing := a.closing
		a.mu.Unlock()
		if closing {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := a.SendRequest(context.Background(), "GET", server.URL, nil, nil, false); !errors.Is(err, ErrClosing) {
		t.Fatal(err)
	}
	close(release)
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
}
