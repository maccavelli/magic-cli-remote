//go:build !unix

package procutil

import (
	"os"
	"os/exec"
	"time"
)

// SetProcessGroup is a no-op on non-Unix platforms.
func SetProcessGroup(cmd *exec.Cmd) {}

// KillProcessGroup falls back to Process.Kill.
func KillProcessGroup(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}

// TerminateProcessGroup has no graceful phase on non-Unix platforms: there is
// no portable "ask nicely" signal, so it kills immediately and reports false
// (SIGTERM did not suffice, because none was sent).
func TerminateProcessGroup(p *os.Process, exited <-chan struct{}, timeout time.Duration) bool {
	_, _ = exited, timeout
	if p == nil {
		return true
	}
	_ = p.Kill()
	return false
}
