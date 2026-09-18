package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type interceptPIDRecord struct {
	PID       int   `json:"pid"`
	SessionID int64 `json:"session_id"`
}

// ActiveInterceptSessionID returns a session only when its PID file identifies
// it and the recorded process is still alive. Legacy PID-only files cannot be
// associated safely with a database row and therefore return zero.
func (a *App) ActiveInterceptSessionID() int64 {
	dir, err := a.mgr.Dir()
	if err != nil {
		return 0
	}
	b, err := os.ReadFile(filepath.Join(dir, "intercept.pid"))
	if err != nil {
		return 0
	}
	var record interceptPIDRecord
	if err := json.Unmarshal(b, &record); err != nil {
		// Accept the legacy format for validation, but it has no session ID.
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(b)))
		if parseErr != nil || !interceptProcessAlive(pid) {
			return 0
		}
		return 0
	}
	if record.PID <= 0 || record.SessionID <= 0 || !interceptProcessAlive(record.PID) {
		return 0
	}
	return record.SessionID
}

// interceptReconcileGrace protects a session that is starting up right now:
// `intercept start` inserts its session row shortly before it publishes the PID
// file, so a session younger than this may not yet be identifiable as active.
const interceptReconcileGrace = time.Minute

// reconcileInterceptSessions closes out sessions left open by a crash, so their
// rows carry a real end time by the time anything lists them. The session owned
// by a live process is left alone.
//
// Best-effort by design: a store that cannot be written still serves historical
// traffic, so this must never prevent the app from opening.
func (a *App) reconcileInterceptSessions(ctx context.Context) {
	_, _ = a.store.ReconcileInterceptSessions(
		ctx, a.ActiveInterceptSessionID(), time.Now(), interceptReconcileGrace,
	)
}
