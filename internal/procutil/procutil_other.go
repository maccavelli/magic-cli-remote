//go:build !unix

package procutil

import (
	"os"
	"os/exec"
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
