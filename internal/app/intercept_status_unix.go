//go:build !windows

package app

import (
	"errors"
	"syscall"
)

func interceptProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
