// Command wiretap-relay is the standalone relay server deployed on the VPS.
// It serves the admin API, webhook ingress, and the WebSocket tunnel used by
// wiretap clients to receive captured webhooks.
//
// TLS is expected to be terminated by a reverse proxy (Caddy, nginx, etc.)
// in front of this binary; the relay itself listens on plain HTTP. The PC
// connects via wss:// to the proxy, which forwards to this server's /tunnel.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/plutack/wiretap/internal/relayd"
	"github.com/plutack/wiretap/internal/store"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	addr := flag.String("addr", "", "listen address (default :8443, or WIRETAP_RELAY_ADDR)")
	dbPath := flag.String("db", "", "path to the relay SQLite database (default relay.db, or WIRETAP_RELAY_DB)")
	adminToken := flag.String("admin-token", "", "admin token for /register and /admin/* (required, or set WIRETAP_ADMIN_TOKEN)")
	flag.Parse()

	// Configuration resolves flag first, then env var, then default. The env
	// vars are how container platforms (Coolify, Docker, Kubernetes) configure
	// the relay without touching the command line.
	resolve := func(flagValue, envVar, fallback string) string {
		if flagValue != "" {
			return flagValue
		}
		if v := os.Getenv(envVar); v != "" {
			return v
		}
		return fallback
	}
	listenAddr := resolve(*addr, "WIRETAP_RELAY_ADDR", ":8443")
	db := resolve(*dbPath, "WIRETAP_RELAY_DB", "relay.db")

	// Resolve the admin token from the flag, falling back to the env var.
	token := *adminToken
	if token == "" {
		token = os.Getenv("WIRETAP_ADMIN_TOKEN")
	}
	if token == "" {
		log.Fatal("wiretap-relay: --admin-token (or WIRETAP_ADMIN_TOKEN env var) is required")
	}

	if err := run(listenAddr, db, token); err != nil {
		log.Fatal(err)
	}
}

// run starts the relay server and blocks until it receives SIGINT/SIGTERM.
// Splitting it from main makes the server lifecycle reachable from tests
// without involving os.Args.
func run(addr, dbPath, adminToken string) error {
	ctx := context.Background()

	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("wiretap-relay: open database %s: %w", dbPath, err)
	}
	defer db.Close()

	if err := store.MigrateRelay(ctx, db); err != nil {
		return fmt.Errorf("wiretap-relay: migrate database: %w", err)
	}

	st := store.NewRelayStore(db)
	srv := relayd.NewServer(st,
		relayd.WithAdminToken(adminToken),
		relayd.WithVersion(version),
	)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("wiretap-relay %s listening on %s (db: %s)", version, addr, dbPath)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-stop:
	case err := <-errCh:
		_ = httpSrv.Shutdown(context.Background())
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return nil
}
