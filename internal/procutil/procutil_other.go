//go:build !unix && !windows

package procutil

import (
	"context"
	"os"
	"os/exec"
	"time"
)

// newCommand implements [Command]. On this residual set of platforms it is
// exec.CommandContext and nothing else, for the same reason the rest of this
// file is: there is no process group to join and no console to hide.
func newCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	SetProcessGroup(cmd)
	return cmd
}

// SetProcessGroup is a no-op on platforms with neither process groups nor
// job objects (js/wasm, plan9). Unix uses setpgid; Windows uses a Job Object
// (MADR 0116 D8).
func SetProcessGroup(cmd *exec.Cmd) {}

// SuperviseStarted is a no-op on this residual set of platforms: with neither
// process groups nor job objects there is nothing to attach a descendant tree
// to. The returned release does nothing (MADR 0150 D1).
func SuperviseStarted(p *os.Process) (release func(), err error) {
	_ = p
	return func() {}, nil
}

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
