//go:build windows

package cli

import "os"

// processAlive reports whether pid is a running process. On Windows,
// os.FindProcess opens a real process handle (unlike Unix, where it always
// succeeds), so a lookup failure means the process is gone.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	defer p.Release()
	return true
}

// terminateProcess stops pid. Windows has no SIGTERM; TerminateProcess (via
// Process.Kill) is the standard forceful stop. The `intercept stop` command
// still resets startup files itself afterwards, so cleanup does not depend
// on the killed process running its own teardown.
func terminateProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	defer p.Release()
	return p.Kill()
}
