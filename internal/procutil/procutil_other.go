//go:build !unix && !windows

package procutil

import (
	"os"
	"os/exec"
	"time"
)

// SetProcessGroup is a no-op on platforms with neither process groups nor
// job objects (js/wasm, plan9). Unix uses setpgid; Windows uses a Job Object
// (MADR 0116 D8).
func SetProcessGroup(cmd *exec.Cmd) {}

// KillProcessGroup falls back to Process.Kill.
func KillProcessGroup(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}

// TerminateProcessGroup has no graceful phase on this residual set of
// platforms: there is no portable "ask nicely" signal, so it kills immediately
// and reports false (SIGTERM did not suffice, because none was sent). Unix
// sends SIGTERM; Windows sends CTRL_BREAK_EVENT (MADR 0116 D8).
func TerminateProcessGroup(p *os.Process, exited <-chan struct{}, timeout time.Duration) bool {
	_, _ = exited, timeout
	if p == nil {
		return true
	}
	_ = p.Kill()
	return false
}
