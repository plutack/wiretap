// PID-file plumbing for `wiretap intercept`. `start` records its own PID in
// <configDir>/intercept.pid; `stop` reads it, asks the process to terminate
// (SIGTERM on Unix, TerminateProcess on Windows — see daemon_unix.go /
// daemon_windows.go), and waits for the listeners to be released before
// resetting the shell startup files.
package cli

import (
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

func writePIDFile(configDir string, pid int) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", configDir, err)
	}
	return os.WriteFile(filepath.Join(configDir, pidFileName), []byte(strconv.Itoa(pid)), 0o644)
}

func readPIDFile(configDir string) (int, error) {
	b, err := os.ReadFile(filepath.Join(configDir, pidFileName))
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, fmt.Errorf("parse pid file: %w", err)
	}
	return pid, nil
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
