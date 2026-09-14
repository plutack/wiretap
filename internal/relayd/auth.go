// Package relayd is the wiretap-relay HTTP server. It serves three kinds of
// routes:
//   - /health (no auth)
//   - POST /:project (webhook ingress, no auth — projects are write-open)
//   - /admin/* (requires X-Admin-Token)
//   - /register (requires X-Admin-Token)
//   - /client/* (requires client_id/client_token)
//   - /tunnel (WebSocket; requires client_id/client_token)
//
// All handlers are wired by the Server.Routes method so production code and
// tests both build the same mux. Deps are injected via functional options so
// tests can swap in an in-memory store, fake clock, and fake id generator.
package relayd

import (
	"context"
	"crypto/subtle"
	"net/http"

	"github.com/plutack/wiretap/internal/store"
)

// requireAdmin wraps a handler so that it returns 401 unless the request
// carries an X-Admin-Token matching the server's configured admin token.
// Constant-time comparison avoids leaking the token's expected length or
// early-mismatch prefix via timing.
func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Admin-Token")
		if s.adminToken == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.adminToken)) != 1 {
			writeErr(w, http.StatusUnauthorized, "auth_failed", "missing or invalid admin token")
			return
		}
		h(w, r)
	}
}

// requireClient authenticates client HTTP routes with the same basic
// credentials used by the tunnel and exposes the client id to the handler.
func (s *Server) requireClient(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, err := s.authClientByBasic(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "auth_failed", "missing or invalid client credentials")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), clientIDKey{}, client.ClientID)))
	}
}

// clientIDKey is a private context key for the authenticated client_id set
// by the tunnel layer. Defined here so handlers can read it without
// cross-package coupling.
type clientIDKey struct{}

// authClientByBasic extracts client_id / client_token from HTTP basic auth
// and returns the matching ClientRow or an *api.Error-shaped error. Used by
// the tunnel handler.
func (s *Server) authClientByBasic(r *http.Request) (*store.ClientRow, error) {
	id, token, ok := r.BasicAuth()
	if !ok {
		return nil, apiError(http.StatusUnauthorized, "auth_failed", "missing basic auth")
	}
	c, err := s.store.Client(r.Context(), id)
	if err != nil {
		return nil, apiError(http.StatusUnauthorized, "auth_failed", "client not found")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(c.ClientToken)) != 1 {
		return nil, apiError(http.StatusUnauthorized, "auth_failed", "bad token")
	}
	return c, nil
}
