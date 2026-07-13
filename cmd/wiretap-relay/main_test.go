package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/plutack/wiretap/internal/relayd"
	"github.com/plutack/wiretap/internal/store"
)

// freshRelayServer builds a real relayd server backed by an in-memory SQLite
// (migrated), wrapped in an httptest.Server. Returns the URL for clients.
func freshRelayServer(t *testing.T) *httptest.Server {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenInMemory("relayd-main-test")
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.MigrateRelay(ctx, db); err != nil {
		t.Fatalf("MigrateRelay: %v", err)
	}
	st := store.NewRelayStore(db)
	srv := relayd.NewServer(st, relayd.WithAdminToken("test-admin"), relayd.WithVersion("test"))
	hs := httptest.NewServer(srv.Routes())
	t.Cleanup(hs.Close)
	return hs
}

func TestRelay_Health(t *testing.T) {
	t.Parallel()
	hs := freshRelayServer(t)

	resp, err := http.Get(hs.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want %q", body["status"], "ok")
	}
	if body["version"] != "test" {
		t.Errorf("version field = %q, want %q", body["version"], "test")
	}
}

// TestRelay_UnknownPathIs404 guards against accidentally making every path 200.
// The real relayd ingress handler treats the first path segment as a project
// name and returns 404 for unclaimed projects (see internal/relayd/server.go).
func TestRelay_UnknownPathIs404(t *testing.T) {
	t.Parallel()
	hs := freshRelayServer(t)

	resp, err := http.Get(hs.URL + "/does-not-exist")
	if err != nil {
		t.Fatalf("GET /does-not-exist: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// TestRelay_AdminRouteRequiresToken ensures the admin token gate works end-to-end.
func TestRelay_AdminRouteRequiresToken(t *testing.T) {
	t.Parallel()
	hs := freshRelayServer(t)

	// Without X-Admin-Token → 401.
	resp, err := http.Get(hs.URL + "/admin/clients")
	if err != nil {
		t.Fatalf("GET /admin/clients (no token): %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	resp.Body.Close()

	// With X-Admin-Token → 200.
	req, _ := http.NewRequest("GET", hs.URL+"/admin/clients", nil)
	req.Header.Set("X-Admin-Token", "test-admin")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /admin/clients (with token): %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("with token: status = %d, want %d", resp2.StatusCode, http.StatusOK)
	}
}
