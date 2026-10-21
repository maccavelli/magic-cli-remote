package procutil

import (
	"fmt"
	"log/slog"
	"os"
)

// The marker keys the reaper tests stamp and sweep for. They are unique to
// this test process, so no other process's sweep matches them: every daemon
// another package's test starts sweeps for EnvEngineID, and go test runs
// packages at the same time (MADR 0204).
var (
	TestEnvEngineID    = fmt.Sprintf("%s_TEST_%d", EnvEngineID, os.Getpid())
	TestEnvEngineOwner = fmt.Sprintf("%s_TEST_%d", EnvEngineOwner, os.Getpid())
	testMarkers        = markerKeys{id: TestEnvEngineID, owner: TestEnvEngineOwner}
)

// FindTestOrphans is FindOrphanEngines for the test marker keys.
func FindTestOrphans() []Orphan { return findOrphanEngines(testMarkers) }

// ReapTestOrphans is ReapOrphanEngines's environment-marker sweep for the test
// marker keys. It sweeps no registry.
func ReapTestOrphans(log *slog.Logger) int {
	if log == nil {
		log = slog.Default()
	}
	return reapMarkedOrphans(log, testMarkers)
}

// StubSweep replaces the sweep's scan, environment read and termination, so a
// test can record what a production sweep asks for without a process. The
// returned function restores them.
func StubSweep(find func(key string) []int, env func(pid int) (map[string]string, bool),
	terminate func(pid int) (found, graceful bool)) (restore func()) {
	prevFind, prevEnv, prevTerminate := findByEnv, processEnv, terminateOrphan
	findByEnv, processEnv, terminateOrphan = find, env, terminate
	return func() { findByEnv, processEnv, terminateOrphan = prevFind, prevEnv, prevTerminate }
}
