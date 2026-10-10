// Command wiretap-relay serves webhook ingress, the admin API and client tunnels.
// TLS is terminated by the deployment's reverse proxy.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/plutack/wiretap/internal/logging"
	"github.com/plutack/wiretap/internal/relayd"
	"github.com/plutack/wiretap/internal/store"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	addr := flag.String("addr", "", "listen address (default :8443, or WIRETAP_RELAY_ADDR)")
	dbPath := flag.String("db", "", "SQLite database path (default relay.db, or WIRETAP_RELAY_DB)")
	adminToken := flag.String("admin-token", "", "admin token (required, or WIRETAP_ADMIN_TOKEN)")
	logFormat := flag.String("log-format", "", "text or json (default text, or WIRETAP_LOG_FORMAT)")
	logLevel := flag.String("log-level", "", "debug, info, warn, error (default info, or WIRETAP_LOG_LEVEL)")
	flag.Parse()
	logger, err := logging.New(os.Stderr, resolve(*logFormat, "WIRETAP_LOG_FORMAT", "text"), resolve(*logLevel, "WIRETAP_LOG_LEVEL", "info"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "wiretap-relay:", err)
		os.Exit(1)
	}
	logger = logger.With("component", "relay", "version", version)
	token := resolve(*adminToken, "WIRETAP_ADMIN_TOKEN", "")
	if token == "" {
		logger.Error("relay_configuration_failed", "reason", "admin_token_required")
		os.Exit(1)
	}
	if err := runWithLogger(resolve(*addr, "WIRETAP_RELAY_ADDR", ":8443"), resolve(*dbPath, "WIRETAP_RELAY_DB", "relay.db"), token, logger); err != nil {
		// The failing operation logs its safe context; do not duplicate arbitrary
		// error text here (DSNs/paths and peer errors can contain secrets).
		logger.Error("relay_exited", "reason", "startup_or_runtime_failure")
		os.Exit(1)
	}
}

func resolve(value, env, fallback string) string {
	if value != "" {
		return value
	}
	if value = os.Getenv(env); value != "" {
		return value
	}
	return fallback
}

// run preserves the testable entry point without changing global loggers.
func run(addr, dbPath, adminToken string) error {
	logger, _ := logging.New(os.Stderr, "text", "info")
	return runWithLogger(addr, dbPath, adminToken, logger.With("component", "relay", "version", version))
}

func runWithLogger(addr, dbPath, adminToken string, logger *slog.Logger) (result error) {
	ctx := context.Background()
	logger.Info("relay_starting")
	db, err := store.Open(dbPath)
	if err != nil {
		logger.Error("database_open_failed")
		return fmt.Errorf("wiretap-relay: open database: %w", err)
	}
	logger.Info("database_opened")
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("database_close_failed")
			result = errors.Join(result, err)
		} else {
			logger.Info("database_closed")
		}
		if result == nil {
			logger.Info("relay_stopped")
		}
	}()
	started := time.Now()
	if err := store.MigrateRelay(ctx, db); err != nil {
		logger.Error("database_migration_failed")
		return fmt.Errorf("wiretap-relay: migrate database: %w", err)
	}
	logger.Info("database_migrations_completed", "duration", time.Since(started))
	srv := relayd.NewServer(store.NewRelayStore(db), relayd.WithAdminToken(adminToken), relayd.WithVersion(version), relayd.WithLogger(logger))
	httpSrv := &http.Server{Addr: addr, Handler: srv.Routes(), ReadHeaderTimeout: 10 * time.Second}
	// Bind before logging readiness, so a failed bind never emits listening.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("relay_listen_failed")
		return err
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	logger.Info("relay_listening", "addr", ln.Addr().String())
	errCh := make(chan error, 1)
	go func() {
		err := httpSrv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	select {
	case sig := <-stop:
		logger.Info("relay_shutdown_requested", "signal", sig.String(), "timeout", 5*time.Second)
	case err := <-errCh:
		if err != nil {
			logger.Error("relay_serve_failed")
		}
		_ = httpSrv.Shutdown(context.Background())
		return err
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started = time.Now()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http_shutdown_failed", "timed_out", errors.Is(err, context.DeadlineExceeded), "duration", time.Since(started))
		return err
	}
	// HTTP shutdown does not yet drain upgraded WebSockets; do not claim it does.
	logger.Info("http_shutdown_completed", "duration", time.Since(started))
	return nil
}
