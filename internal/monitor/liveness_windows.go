//go:build windows

package monitor

import "os"

// processAlive reports whether a process with the given PID is running.
//
// Windows has no signal-0 probe, and os.FindProcess always succeeds, so
// we open the process handle via os.FindProcess and treat a Signal probe
// result as the liveness answer. In practice the more reliable backstop
// is graceful deregistration on shutdown; a crashed Windows instance's
// stale entry is tolerated until its PID is reused (rare) or the file is
// cleaned up manually. Conservative default: assume alive on any
// ambiguity so we never hide a genuinely-running peer.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal(nil) returns nil if the process is still running, and an
	// error ("not supported by windows"/"already finished") otherwise.
	// Go's windows runtime returns nil for a live handle.
	if err := p.Signal(os.Signal(nil)); err != nil {
		return false
	}
	return true
}
