package store

import "errors"

// Sentinel errors for the store package. Callers use errors.Is to detect
// these without matching error strings.
var (
	// ErrNotFound is returned when a row lookup returns no rows.
	ErrNotFound = errors.New("store: not found")

	// ErrConflict is returned when an insert or update violates a
	// uniqueness constraint (e.g., registering a client_id twice or
	// binding a project path that's already taken).
	ErrConflict = errors.New("store: conflict")
)
