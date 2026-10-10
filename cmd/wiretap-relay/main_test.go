package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/logging"

	"github.com/plutack/wiretap/internal/relayd"
	"github.com/plutack/wiretap/internal/store"
)

func TestResolveLoggingPrecedence(t *testing.T) {
	t.Setenv("WIRETAP_LOG_FORMAT", "json")
	if got := resolve("text", "WIRETAP_LOG_FORMAT", "text"); got != "text" {
		t.Fatal(got)
	}
	if got := resolve("", "WIRETAP_LOG_FORMAT", "text"); got != "json" {
		t.Fatal(got)
	}
	t.Setenv("WIRETAP_LOG_FORMAT", "")
	if got := resolve("", "WIRETAP_LOG_FORMAT", "text"); got != "text" {
		t.Fatal(got)
	}
}

func TestRelayLifecycleLogging(t *testing.T) {
	// Signals must target a child process, never the parallel test runner.
	if os.Getenv("WIRETAP_TEST_LOGGING_CHILD") == "1" {
		logger, _ := logging.New(os.Stdout, "json", "info")
		if err := runWithLogger("127.0.0.1:0", os.Getenv("WIRETAP_TEST_DB"), "admin-secret", logger.With("component", "relay", "version", "test")); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	if !supportsTerminationSignal() {
		t.Skip("requires Unix SIGTERM")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRelayLifecycleLogging$")
	cmd.Env = append(os.Environ(), "WIRETAP_TEST_LOGGING_CHILD=1", "WIRETAP_TEST_DB="+filepath.Join(t.TempDir(), "relay.db"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	var events []string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		event, _ := record["msg"].(string)
		events = append(events, event)
		if record["component"] != "relay" || record["version"] != "test" {
			t.Fatal(record)
		}
		if event == "relay_listening" {
			// Serve and signal registration follow binding; a successful request
			// establishes the server goroutine is running before sending SIGTERM.
			resp, err := http.Get("http://" + record["addr"].(string) + "/health")
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err, stderr.String(), events)
	}
	want := []string{"relay_starting", "database_opened", "database_migrations_completed", "relay_listening", "relay_shutdown_requested", "relay_draining", "http_shutdown_completed", "relay_workers_stopped", "database_closed", "relay_stopped"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v", events)
	}
}

func supportsTerminationSignal() bool { return os.PathSeparator != '\\' }

func TestRelayFailedBindDoesNotLogReady(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var out bytes.Buffer
	logger, _ := logging.New(&out, "json", "debug")
	if err := runWithLogger(ln.Addr().String(), filepath.Join(t.TempDir(), "relay.db"), "secret", logger); err == nil {
		t.Fatal("expected bind failure")
	}
	if strings.Contains(out.String(), "relay_listening") || !strings.Contains(out.String(), "relay_listen_failed") || !strings.Contains(out.String(), "database_closed") {
		t.Fatal(out.String())
	}
}

// freshRelayServer builds a real relayd server backed by an in-memory SQLite
// (migrated), wrapped in an httptest.Server. Returns the URL for clients.
func freshRelayServer(t *testing.T) *httptest.Server {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenInMemory("relayd-main-" + t.Name())
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
// name and returns 404 for projects without subscribers (see relayd/server.go).
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
