//go:build windows

package procutil

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestOwnerAliveForLiveProcess is the direct MADR 0116 F7 regression. The
// pre-0116 fallback used Signal(0), which Windows does not support, so
// OwnerAlive returned false for EVERY live process — inverted, not merely
// weak.
func TestOwnerAliveForLiveProcess(t *testing.T) {
	if got := OwnerAlive(OwnerToken()); !got {
		t.Error("OwnerAlive(OwnerToken()) = false for this very process")
	}
}

// TestOwnerAliveRejectsRecycledPID proves the creation-time half of the token
// is load-bearing: the same pid with a different start time reads as dead.
func TestOwnerAliveRejectsRecycledPID(t *testing.T) {
	token := OwnerToken()
	if got := OwnerAlive(token + "0"); got {
		t.Error("OwnerAlive accepted a token with a mismatched creation time")
	}
	if got := OwnerAlive("999999999"); got {
		t.Error("OwnerAlive accepted an implausible pid")
	}
	if got := OwnerAlive("not-a-pid"); got {
		t.Error("OwnerAlive accepted a malformed token")
	}
}

// TestProcessStartToken proves a start token is available, which is what
// restores pid-recycle detection to Linux parity.
func TestProcessStartToken(t *testing.T) {
	tok, ok := ProcessStartToken(os.Getpid())
	if !ok || tok == "" {
		t.Fatalf("ProcessStartToken(self) = %q, %v; want a value", tok, ok)
	}
	if _, ok := ProcessStartToken(-1); ok {
		t.Error("ProcessStartToken accepted a negative pid")
	}
}

// TestSuperviseStartedKillsTree proves the job object kills a grandchild, the
// guarantee the no-op fallback never provided (MADR 0116 D8).
func TestSuperviseStartedKillsTree(t *testing.T) {
	// cmd.exe spawns ping as a child; killing the job must take both.
	cmd := exec.Command("cmd.exe", "/c", "ping -n 30 127.0.0.1 > NUL")
	SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	release, err := SuperviseStarted(cmd.Process)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("SuperviseStarted: %v", err)
	}
	pid := cmd.Process.Pid
	if !processAlive(pid) {
		t.Fatal("child died before the job was closed")
	}

	release() // closing the job kills the tree

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			_ = cmd.Wait()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	t.Fatal("process survived the job object closing")
}

// TestSuperviseStartedNilProcess proves the nil guard matches the other
// entry points rather than panicking.
func TestSuperviseStartedNilProcess(t *testing.T) {
	release, err := SuperviseStarted(nil)
	if err != nil {
		t.Fatalf("SuperviseStarted(nil): %v", err)
	}
	release()
}

// TestTerminateProcessGroupNil mirrors the Unix contract for a nil process.
func TestTerminateProcessGroupNil(t *testing.T) {
	if !TerminateProcessGroup(nil, nil, time.Second) {
		t.Error("TerminateProcessGroup(nil) = false, want true")
	}
	if err := KillProcessGroup(nil); err != nil {
		t.Errorf("KillProcessGroup(nil) = %v, want nil", err)
	}
}
