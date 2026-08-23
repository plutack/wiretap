package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
