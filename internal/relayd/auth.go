// Package relayd is the wiretap-relay HTTP server. It serves three kinds of
// routes:
//   - /health (no auth)
//   - POST /:project (webhook ingress, no auth — projects are write-open)
//   - /admin/* (requires X-Admin-Token)
//   - /register (requires X-Admin-Token)
//   - /tunnel (WebSocket; requires client_id/client_token)
//
// All handlers are wired by the Server.Routes method so production code and
// tests both build the same mux. Deps are injected via functional options so
// tests can swap in an in-memory store, fake clock, and fake id generator.
package relayd

import (
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

// requireClient asserts that the request was authenticated as a specific
// client_id by the tunnel layer. Not used yet by HTTP routes (admin token
// covers owner-only ops in the MVP); included here as the seam the replay
// route will use once we add per-client auth on top of admin.
//
//nolint:unused // reserved for the next phase of HTTP routes
func (s *Server) requireClient(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := r.Context().Value(clientIDKey{}).(string)
		if !ok || id == "" {
			writeErr(w, http.StatusUnauthorized, "auth_failed", "client auth required")
			return
		}
		h(w, r)
	}
}

// clientIDKey is a private context key for the authenticated client_id set
// by the tunnel layer. Defined here so handlers can read it without
// cross-package coupling.
type clientIDKey struct{}

// clientAndProject resolves the client_id that owns path, used by ingress
// to look up routing. Returns the empty string and a 404-shaped *api.Error
// when no client owns the path. Callers translate that into a writeErr call.
func (s *Server) clientAndProject(r *http.Request, path string) (string, error) {
	clientID, err := s.store.ClientByProject(r.Context(), path)
	if err != nil {
		return "", err
	}
	return clientID, nil
}

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
