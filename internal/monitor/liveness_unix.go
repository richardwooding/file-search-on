//go:build !windows

package monitor

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with the given PID is currently
// running. Signal 0 performs error checking without delivering a signal:
// a nil error means the process exists and we can signal it; EPERM means
// it exists but is owned by another user (still alive); ESRCH means no
// such process (dead). Used to prune crashed instances from the monitor
// registry.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
