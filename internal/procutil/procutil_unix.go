//go:build unix

// Package procutil helps manage child process trees (process groups).
package procutil

import (
	"os"
	"os/exec"
	"syscall"
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
