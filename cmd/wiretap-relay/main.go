// Command wiretap-relay is the standalone relay server deployed on the VPS.
// It serves the admin API, webhook ingress, and the WebSocket tunnel used by
// wiretap clients to receive captured webhooks.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	addr := flag.String("addr", ":8443", "listen address")
	flag.Parse()
	if err := run(*addr); err != nil {
		log.Fatal(err)
	}
}

// run starts the HTTP server and blocks until it receives SIGINT/SIGTERM.
// Splitting it from main makes the server lifecycle reachable from tests
// without involving os.Args.
func run(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	log.Printf("wiretap-relay %s listening on %s", version, addr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-stop:
	case err := <-errCh:
		_ = srv.Shutdown(context.Background())
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return err
	}
	return nil
}

// routes builds the HTTP mux. Exported indirectly via being a package-level
// function so main_test.go can exercise it with httptest.
func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	return mux
}

// health reports liveness. Returns a static payload for now; richer probes
// (DB connectivity, tunnel count) can be added as needed.
func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": version,
	})
}
