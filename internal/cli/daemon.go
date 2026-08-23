// PID-file plumbing for `wiretap intercept`. `start` records its own PID in
// <configDir>/intercept.pid; `stop` reads it, asks the process to terminate
// (SIGTERM on Unix, TerminateProcess on Windows — see daemon_unix.go /
// daemon_windows.go), and waits for the listeners to be released before
// resetting the shell startup files.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	pidFileName    = "intercept.pid"
	pidStopTimeout = 5 * time.Second
	pidStopPoll    = 100 * time.Millisecond
)

type interceptPIDRecord struct {
	PID       int   `json:"pid"`
	SessionID int64 `json:"session_id,omitempty"`
}

func writePIDFile(configDir string, pid int, sessionID int64) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", configDir, err)
	}
	b, err := json.Marshal(interceptPIDRecord{PID: pid, SessionID: sessionID})
	if err != nil {
		return fmt.Errorf("encode pid file: %w", err)
	}
	return os.WriteFile(filepath.Join(configDir, pidFileName), b, 0o644)
}

func readPIDRecord(configDir string) (interceptPIDRecord, error) {
	b, err := os.ReadFile(filepath.Join(configDir, pidFileName))
	if err != nil {
		return interceptPIDRecord{}, err
	}
	var record interceptPIDRecord
	if err := json.Unmarshal(b, &record); err == nil && record.PID > 0 {
		return record, nil
	}
	// Backward compatibility with the pre-session-ID plain-number format.
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return interceptPIDRecord{}, fmt.Errorf("parse pid file: %w", err)
	}
	return interceptPIDRecord{PID: pid}, nil
}

func readPIDFile(configDir string) (int, error) {
	record, err := readPIDRecord(configDir)
	return record.PID, err
}

func removePIDFile(configDir string) {
	_ = os.Remove(filepath.Join(configDir, pidFileName))
}

// waitProcessExit polls until pid is gone or the stop timeout elapses.
// Returns true when the process exited.
func waitProcessExit(pid int) bool {
	deadline := time.Now().Add(pidStopTimeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(pidStopPoll)
	}
	return !processAlive(pid)
}
