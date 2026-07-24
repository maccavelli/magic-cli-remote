//go:build unix

package procutil_test

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// startGroup launches sh -c script in its own process group and returns the
// command plus a channel closed when it has been reaped.
func startGroup(t *testing.T, script string) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	procutil.SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() { _ = procutil.KillProcessGroup(cmd.Process) })
	return cmd, exited
}

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

// A child that exits on SIGTERM must be stopped by the graceful phase alone —
// no SIGKILL, so it gets to run its cleanup.
func TestTerminateProcessGroupGracefulPath(t *testing.T) {
	// Writes a marker on SIGTERM, proving the handler ran (SIGKILL cannot).
	marker := t.TempDir() + "/term-ran"
	cmd, exited := startGroup(t,
		`trap 'echo ok > `+marker+`; exit 0' TERM; while :; do sleep 0.05; done`)

	// Give the shell a moment to install the trap.
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	graceful := procutil.TerminateProcessGroup(cmd.Process, exited, 5*time.Second)
	if !graceful {
		t.Fatal("TerminateProcessGroup escalated to SIGKILL for a SIGTERM-honouring child")
	}
	if e := time.Since(start); e > 3*time.Second {
		t.Fatalf("graceful stop took %s; should return as soon as the child exits", e)
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("child not reaped")
	}
	if _, err := readFileEventually(marker, 2*time.Second); err != nil {
		t.Fatalf("SIGTERM handler did not run: %v", err)
	}
}

// A child that ignores SIGTERM must still die, via the SIGKILL escalation.
func TestTerminateProcessGroupEscalatesToKill(t *testing.T) {
	cmd, exited := startGroup(t, `trap '' TERM; while :; do sleep 0.05; done`)
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	graceful := procutil.TerminateProcessGroup(cmd.Process, exited, 300*time.Millisecond)
	if graceful {
		t.Fatal("reported graceful for a child that ignores SIGTERM")
	}
	if e := time.Since(start); e < 300*time.Millisecond {
		t.Fatalf("escalated after %s, before the %s timeout elapsed", e, 300*time.Millisecond)
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("child survived SIGKILL escalation")
	}
}

// The whole group dies, not just the direct child — the property that makes
// this safe for agents that spawn shells.
func TestTerminateProcessGroupKillsGrandchildren(t *testing.T) {
	cmd, exited := startGroup(t, `trap '' TERM; sleep 60 & while :; do sleep 0.05; done`)
	time.Sleep(200 * time.Millisecond)

	if procutil.TerminateProcessGroup(cmd.Process, exited, 300*time.Millisecond) {
		t.Fatal("expected escalation")
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("group survived")
	}
}

// An already-dead process must report success rather than signalling a pid
// that may since have been recycled.
func TestTerminateProcessGroupAlreadyExited(t *testing.T) {
	cmd, exited := startGroup(t, `exit 0`)
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("child did not exit")
	}
	if !procutil.TerminateProcessGroup(cmd.Process, exited, time.Second) {
		t.Fatal("expected success for an already-exited process")
	}
}

func TestTerminateProcessGroupNilProcess(t *testing.T) {
	if !procutil.TerminateProcessGroup(nil, nil, time.Second) {
		t.Fatal("nil process should report success")
	}
}

func readFileEventually(path string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			return b, nil
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	return nil, lastErr
}
