package procutil

import (
	"fmt"
	"log/slog"
	"os"
	"time"
)

// Environment variables stamped onto every engine subprocess mcremote spawns
// (goose, opencode, codex — anything started via [SetProcessGroup] that
// outlives a single request).
//
// They exist so an engine can be attributed to the daemon that started it.
// Nothing else is a safe basis for killing a process: `opencode serve
// --hostname 127.0.0.1 --port N` looks identical whether we launched it or an
// operator did by hand, so argv matching would eventually kill someone's
// terminal. Only a process carrying our marker is ever a candidate.
const (
	// EnvEngineID uniquely identifies one engine instance.
	EnvEngineID = "MCREMOTE_ENGINE_ID"
	// EnvEngineOwner is the [OwnerToken] of the owning daemon — "<pid>:<starttime>",
	// so a recycled pid does not read as a live owner.
	EnvEngineOwner = "MCREMOTE_ENGINE_OWNER"
)

// orphanStopTimeout bounds the graceful phase when reaping an orphan. It is
// shorter than a normal shutdown: nothing is waiting on this engine, but the
// daemon's own startup is.
const orphanStopTimeout = 3 * time.Second

// Orphan describes an engine process left behind by a daemon that is gone.
type Orphan struct {
	PID      int
	EngineID string
	Owner    string
}

func (o Orphan) String() string {
	return fmt.Sprintf("pid=%d engine=%s owner=%s", o.PID, o.EngineID, o.Owner)
}

// FindOrphanEngines returns engine processes whose owning daemon is no longer
// running. An engine whose owner is alive belongs to a concurrently running
// mcremote — a developer's foreground daemon next to the systemd unit, say —
// and is deliberately left alone: reaping it would break a working daemon.
//
// Processes carrying our marker but no owner token are treated as orphans;
// they can only have come from a build of ours that failed to stamp one.
//
// Linux only. Elsewhere [FindByEnv] reports nothing and this returns no
// orphans, so the caller degrades to "nothing to reap" rather than to a wrong
// answer.
func FindOrphanEngines() []Orphan {
	self := os.Getpid()
	var out []Orphan
	for _, pid := range FindByEnv(EnvEngineID) {
		if pid == self {
			continue
		}
		env, ok := ProcessEnv(pid)
		if !ok {
			// Exited between the scan and now, or not ours to read.
			continue
		}
		owner := env[EnvEngineOwner]
		if owner != "" && OwnerAlive(owner) {
			continue
		}
		out = append(out, Orphan{PID: pid, EngineID: env[EnvEngineID], Owner: owner})
	}
	return out
}

// ReapOrphanEngines terminates every engine whose owning daemon is gone and
// returns how many it stopped. Intended to run once at daemon startup, before
// any engine of our own is spawned.
//
// registryDir, when non-empty, is also scanned (MADR 0059 engine registry).
// Env-marker discovery remains the Linux defense-in-depth path.
//
// This is the backstop for the cases the death signal and the graceful
// shutdown path both miss — `kill -9`, a panic, a power loss — which is
// exactly how an engine came to be found still listening 19 hours after its
// daemon died (MADR 0019).
func ReapOrphanEngines(log *slog.Logger, registryDir ...string) int {
	if log == nil {
		log = slog.Default()
	}
	reaped := 0
	for _, dir := range registryDir {
		reaped += ReapRegistryOrphans(log, dir)
	}
	if defaultEngineRegistryDir != "" {
		// Avoid double-counting when the same dir is passed explicitly.
		dup := false
		for _, dir := range registryDir {
			if dir == defaultEngineRegistryDir {
				dup = true
				break
			}
		}
		if !dup {
			reaped += ReapRegistryOrphans(log, defaultEngineRegistryDir)
		}
	}
	orphans := FindOrphanEngines()
	for _, o := range orphans {
		proc, err := os.FindProcess(o.PID)
		if err != nil {
			continue
		}
		graceful := TerminateProcessGroup(proc, nil, orphanStopTimeout)
		reaped++
		log.Info("reaped orphaned agent engine",
			slog.Int("pid", o.PID),
			slog.String("engine_id", o.EngineID),
			slog.String("dead_owner", o.Owner),
			slog.Bool("graceful", graceful),
		)
	}
	if reaped > 0 {
		log.Info("orphaned engine sweep complete", slog.Int("reaped", reaped))
	}
	return reaped
}
