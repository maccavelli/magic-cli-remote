//go:build unix

package procutil_test

import (
	"os/exec"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

func TestKillProcessGroupKillsChildShell(t *testing.T) {
	// bash -lc starts a child that would outlive a plain Process.Kill without pgid.
	cmd := exec.Command("/bin/bash", "-lc", "sleep 60")
	procutil.SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	time.Sleep(50 * time.Millisecond)
	if err := procutil.KillProcessGroup(cmd.Process); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("process group did not exit after KillProcessGroup")
	}
}
