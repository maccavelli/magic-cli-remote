//go:build linux

package httpagent_test

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// spawnMarked starts a long-running stand-in for an engine, stamped with the
// ownership env vars a real one carries.
func spawnMarked(t *testing.T, engineID, owner string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "while :; do sleep 0.05; done")
	env := append(os.Environ(), httpagent.EnvEngineID+"="+engineID)
	if owner != "" {
		env = append(env, httpagent.EnvEngineOwner+"="+owner)
	}
	cmd.Env = env
	procutil.SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = procutil.KillProcessGroup(cmd.Process)
		_ = cmd.Wait()
	})
	// Let it appear in /proc with its environment populated.
	time.Sleep(200 * time.Millisecond)
	return cmd
}

func orphanPIDs(t *testing.T) map[int]httpagent.Orphan {
	t.Helper()
	out := map[int]httpagent.Orphan{}
	for _, o := range httpagent.FindOrphanEngines() {
		out[o.PID] = o
	}
	return out
}

// An engine whose owning daemon is gone is an orphan and must be reaped.
func TestFindOrphanEnginesDetectsDeadOwner(t *testing.T) {
	// A pid that cannot exist stands in for a daemon that has exited.
	cmd := spawnMarked(t, "engine-dead-owner", "999999999:1")

	found, ok := orphanPIDs(t)[cmd.Process.Pid]
	if !ok {
		t.Fatalf("engine %d with a dead owner was not reported as an orphan", cmd.Process.Pid)
	}
	if found.EngineID != "engine-dead-owner" {
		t.Fatalf("engine id = %q", found.EngineID)
	}
}

// The safety property that matters most: an engine owned by a LIVE daemon
// (a developer's foreground mcremote beside the systemd unit) must be left
// strictly alone, or starting one daemon would break another.
func TestFindOrphanEnginesSkipsLiveOwner(t *testing.T) {
	cmd := spawnMarked(t, "engine-live-owner", procutil.OwnerToken())

	if o, ok := orphanPIDs(t)[cmd.Process.Pid]; ok {
		t.Fatalf("engine owned by this live process was reported as an orphan: %s", o)
	}

	// And it must survive a sweep.
	httpagent.ReapOrphanEngines(nil)
	time.Sleep(300 * time.Millisecond)
	if !alive(cmd.Process.Pid) {
		t.Fatal("sweep killed an engine whose owner is alive")
	}
}

// alive reports whether pid still exists (signal 0 probe).
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// A process without our marker must never be touched, however much it looks
// like an engine — a human may have started `opencode serve` by hand.
func TestFindOrphanEnginesIgnoresUnmarkedProcesses(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "while :; do sleep 0.05; done")
	// Deliberately no marker, but an engine-shaped environment otherwise.
	cmd.Env = append(os.Environ(), "SOME_OTHER_VAR=opencode serve --port 1")
	procutil.SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = procutil.KillProcessGroup(cmd.Process)
		_ = cmd.Wait()
	})
	time.Sleep(200 * time.Millisecond)

	if _, ok := orphanPIDs(t)[cmd.Process.Pid]; ok {
		t.Fatal("an unmarked process was reported as an orphan")
	}
	httpagent.ReapOrphanEngines(nil)
	time.Sleep(300 * time.Millisecond)
	if !alive(cmd.Process.Pid) {
		t.Fatal("sweep killed an unmarked process")
	}
}

// End to end: a marked, owner-dead engine is actually stopped by the sweep.
func TestReapOrphanEnginesKillsOrphan(t *testing.T) {
	cmd := spawnMarked(t, "engine-to-reap", "999999999:1")
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	if n := httpagent.ReapOrphanEngines(nil); n < 1 {
		t.Fatalf("sweep reaped %d processes, want at least the orphan", n)
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("orphan survived the sweep")
	}
}

// A marker with no owner token at all can only come from a build of ours that
// failed to stamp one, so it is treated as an orphan rather than left forever.
func TestFindOrphanEnginesTreatsMissingOwnerAsOrphan(t *testing.T) {
	cmd := spawnMarked(t, "engine-no-owner", "")
	if _, ok := orphanPIDs(t)[cmd.Process.Pid]; !ok {
		t.Fatal("engine with no owner token should be treated as an orphan")
	}
}
