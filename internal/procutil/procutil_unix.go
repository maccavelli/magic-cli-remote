//go:build unix

// Package procutil helps manage child process trees (process groups).
package procutil

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// SetProcessGroup configures cmd so it starts in a new process group.
// KillProcessGroup can then signal the whole tree (bash -lc children, etc.).
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// KillProcessGroup sends SIGKILL to the process group of p.
// Falls back to Process.Kill if pgid is unavailable.
func KillProcessGroup(p *os.Process) error {
	if p == nil {
		return nil
	}
	// Negative pid = process group (requires Setpgid on start).
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		return p.Kill()
	}
	return nil
}

// TerminateProcessGroup stops p's process group politely: SIGTERM first, then
// SIGKILL if the group has not exited within timeout. It reports whether
// SIGTERM alone was enough.
//
// exited, when non-nil, must be a channel that is CLOSED once the process has
// been reaped — closed rather than written to, so this can wait on it without
// stealing a value from the owner of cmd.Wait. When it is nil the group's
// liveness is polled instead.
//
// Prefer this over [KillProcessGroup] for children that own persistent state
// (a database, a session store): SIGKILL gives them no chance to flush. Use
// KillProcessGroup directly only where the child is known to be wedged or
// where its state does not matter.
func TerminateProcessGroup(p *os.Process, exited <-chan struct{}, timeout time.Duration) bool {
	if p == nil {
		return true
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGTERM); err != nil {
		if err == syscall.ESRCH {
			// Group is already gone; nothing to escalate to.
			return true
		}
		// No process group (Setpgid was not used) — signal the process alone.
		_ = p.Signal(syscall.SIGTERM)
	}

	deadline := time.Now().Add(timeout)
	for {
		if exited != nil {
			select {
			case <-exited:
				return true
			case <-time.After(20 * time.Millisecond):
			}
		} else {
			// No reap signal available: probe the group with signal 0. This is
			// inherently racy against PID reuse, which is exactly why callers
			// that can supply `exited` should.
			if syscall.Kill(-p.Pid, 0) == syscall.ESRCH {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		if time.Now().After(deadline) {
			break
		}
	}

	// Escalate. If the process was reaped in the gap between the last check and
	// here, -Pid could name a recycled group; the window is sub-millisecond and
	// closing `exited` promptly on reap is what keeps it that way.
	_ = KillProcessGroup(p)
	return false
}
