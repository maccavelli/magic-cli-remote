//go:build linux

package procutil_test

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// spawnMarked starts a long-running stand-in for an engine, stamped with the
// ownership env vars a real one carries, under this test process's own marker
// keys: a production sweep, such as another package's test daemon makes at
// start, cannot see it (MADR 0204).
func spawnMarked(t *testing.T, engineID, owner string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "while :; do sleep 0.05; done")
	// Strip any inherited engine markers so the test sees exactly what it sets.
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case procutil.EnvEngineID, procutil.EnvEngineOwner, procutil.TestEnvEngineID, procutil.TestEnvEngineOwner:
			continue
		}
		env = append(env, kv)
	}
	env = append(env, procutil.TestEnvEngineID+"="+engineID)
	if owner != "" {
		env = append(env, procutil.TestEnvEngineOwner+"="+owner)
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

func orphanPIDs(t *testing.T) map[int]procutil.Orphan {
	t.Helper()
	out := map[int]procutil.Orphan{}
	for _, o := range procutil.FindTestOrphans() {
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
	procutil.ReapTestOrphans(nil)
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
	procutil.ReapTestOrphans(nil)
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

	if n := procutil.ReapTestOrphans(nil); n < 1 {
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
	// Give the process extra time to appear in /proc and be readable.
	// Retry a few times to handle scheduler delays on a busy host.
	for i := 0; i < 5; i++ {
		if _, ok := orphanPIDs(t)[cmd.Process.Pid]; ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Probe one more time for the error message.
	pids := make([]int, 0)
	for _, o := range procutil.FindTestOrphans() {
		pids = append(pids, o.PID)
	}
	t.Fatalf("engine pid=%d with no owner token not found in orphans %v", cmd.Process.Pid, pids)
}

// TestProductionSweepsUseTheEngineMarkers pins what the tests above no longer
// exercise, since they sweep with keys of their own: FindOrphanEngines and
// ReapOrphanEngines scan for EnvEngineID and read the owner from
// EnvEngineOwner. The scan, the environment read and the termination are
// stubbed, so nothing is spawned or signalled.
func TestProductionSweepsUseTheEngineMarkers(t *testing.T) {
	const pid, dead = 4242, "999999999:1"
	var scanned []string
	var terminated []int
	t.Cleanup(procutil.StubSweep(
		func(key string) []int {
			scanned = append(scanned, key)
			return []int{pid}
		},
		func(int) (map[string]string, bool) {
			return map[string]string{procutil.EnvEngineID: "engine-pinned", procutil.EnvEngineOwner: dead}, true
		},
		func(p int) (bool, bool) {
			terminated = append(terminated, p)
			return true, true
		},
	))

	got := procutil.FindOrphanEngines()
	if len(got) != 1 || got[0].PID != pid || got[0].EngineID != "engine-pinned" || got[0].Owner != dead {
		t.Fatalf("FindOrphanEngines = %v, want pid %d, engine-pinned, owner %s", got, pid, dead)
	}
	if n := procutil.ReapOrphanEngines(nil); n != 1 || !slices.Equal(terminated, []int{pid}) {
		t.Fatalf("ReapOrphanEngines reaped %d, terminated %v; want pid %d once", n, terminated, pid)
	}
	if want := []string{procutil.EnvEngineID, procutil.EnvEngineID}; !slices.Equal(scanned, want) {
		t.Fatalf("the sweeps scanned for %q, want %q", scanned, want)
	}
}
