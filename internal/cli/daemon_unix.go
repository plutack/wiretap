//go:build !windows

package cli

import (
	"errors"
	"syscall"
)

// processAlive reports whether pid is a running process we could signal.
// Signal 0 performs the permission/existence check without delivering
// anything; EPERM means "alive but not ours", which still counts as alive.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

// terminateProcess asks pid to shut down gracefully. SIGTERM gives the
// intercept session a chance to run its deferred cleanup (close listeners,
// reset startup files, remove the pid file).
func terminateProcess(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
