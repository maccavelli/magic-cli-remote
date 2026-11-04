package acpagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// useEngineRegistry points the engine registry at a temp dir for one test.
func useEngineRegistry(t *testing.T, dir string) {
	t.Helper()
	procutil.SetDefaultEngineRegistryDir(dir)
	t.Cleanup(func() { procutil.SetDefaultEngineRegistryDir("") })
}

// engineRecords lists dir's records, failing the test on a read error.
func engineRecords(t *testing.T, dir string) []procutil.EngineRecord {
	t.Helper()
	recs, err := procutil.ListEngineRecords(dir)
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

// TestSpawnedAgentIsRecordedUntilItExits: spawnAgent records the agent in the
// engine registry under the Spec's id and with its PID, so `mcremote engines`
// lists it and an orphan is reaped; the record goes when the agent is reaped
// (0214 PLAN P8 Deviation 1).
func TestSpawnedAgentIsRecordedUntilItExits(t *testing.T) {
	dir := t.TempDir()
	useEngineRegistry(t, dir)
	p, pidFile := testAgentProvider(t)
	s, err := p.spawnAgent(context.Background(), nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recs := engineRecords(t, dir)
	if len(recs) != 1 {
		closeAndWait(t, s)
		t.Fatalf("a running agent left %d registry records, want 1", len(recs))
	}
	if pid := testAgentPID(t, pidFile); recs[0].Provider != "testagent" || recs[0].PID != pid {
		closeAndWait(t, s)
		t.Fatalf("the agent was recorded as provider %q, pid %d; want testagent, pid %d",
			recs[0].Provider, recs[0].PID, pid)
	}
	closeAndWait(t, s)
	if recs := engineRecords(t, dir); len(recs) != 0 {
		t.Fatalf("the record outlived the agent: %+v", recs)
	}
}

// TestFailedStartDropsTheRecord: an agent that dies before initialize answers
// is killed by the start's failure path, and its record goes with it.
func TestFailedStartDropsTheRecord(t *testing.T) {
	dir := t.TempDir()
	useEngineRegistry(t, dir)
	p, _ := testAgentProvider(t)
	t.Setenv(testAgentEnv, "die")
	if _, err := p.spawnAgent(context.Background(), nil, t.TempDir()); err == nil {
		t.Fatal("an agent that died at start was started")
	}
	if recs := engineRecords(t, dir); len(recs) != 0 {
		t.Fatalf("a failed start left its record: %+v", recs)
	}
}

// TestUnwritableRegistryEndsTheStart: a record that cannot be written ends the
// start and the agent, as httpagent's engines do, so no agent runs that
// `mcremote engines` cannot see.
func TestUnwritableRegistryEndsTheStart(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	useEngineRegistry(t, notADir)
	p, pidFile := testAgentProvider(t)
	_, err := p.spawnAgent(context.Background(), nil, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "register testagent") {
		t.Fatalf("an unwritable registry gave %v, want the registration's error", err)
	}
	// The helper writes its PID as it starts. Given time to, it must not be
	// running: the failed start killed it.
	time.Sleep(500 * time.Millisecond)
	if _, statErr := os.Stat(pidFile); statErr == nil {
		if _, alive := procutil.ProcessStartToken(testAgentPID(t, pidFile)); alive {
			t.Fatal("the agent was left running after its record could not be written")
		}
	}
}
