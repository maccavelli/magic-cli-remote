//go:build live_codex || live_codex_turn || live_codex_review

package codex

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func liveEngine(t *testing.T) (*conn, func()) {
	t.Helper()
	p := NewWithLogger(Config{Bin: "codex"}, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fr, err := p.ensureEngine(ctx)
	if err != nil {
		t.Fatalf("engine start: %v", err)
	}
	return fr, p.Shutdown
}

func isParamError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "-32600") || strings.Contains(s, "Invalid request")
}

// liveThreadCwd returns a directory to hand codex as a thread's working
// directory. PASS THE PARENT TEST'S t, not a subtest's — the whole point is the
// lifetime.
//
// Measured on Windows against codex 0.155.1 (PLAN 0163 P14):
//
//	removal immediately after thread/start        -> succeeds
//	removal ~3s after start, engine still running -> "The process cannot access
//	                                                  the file because it is being
//	                                                  used by another process"
//	removal after p.Shutdown                      -> succeeds
//
// So codex opens a handle on a thread's cwd shortly after the thread starts and
// holds it for the engine's lifetime, for every sandbox mode. Since liveEngine
// hands the parent a shutdown func that the parent runs from `defer`, and deferred
// calls run BEFORE t.Cleanup functions, a cleanup registered on the parent runs
// after the engine is down and the removal succeeds. A t.TempDir() inside a
// subtest is removed while the engine is still up, which is why
// TestLiveThreadStartSandboxShape failed with no assertion having failed — and why
// Unix never noticed, since it permits unlinking an open directory.
func liveThreadCwd(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "codex-live-cwd-")
	if err != nil {
		t.Fatalf("create thread cwd: %v", err)
	}
	t.Cleanup(func() {
		// Short retry only. With no turn in flight the engine is down by the time
		// this runs and the release is immediate, so the window is absorbing
		// Windows handle lag rather than waiting for anything.
		//
		// It is NOT unusual for the window to expire in the live_codex_turn suite.
		// Measured on 2026-09-21 across the release 3 acceptance run: 2 expiries in
		// live-codex-turn, 0 in live-codex and live-codex-review, leaving two empty
		// directories. A turn's child processes can hold the cwd open past the
		// engine's own shutdown, which the 0163 P14 probe — taken with no turn
		// running — had no way to observe. So an expiry here means a turn was in
		// flight, not that something is broken.
		deadline := time.Now().Add(3 * time.Second)
		for {
			err := os.RemoveAll(dir)
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				// A leftover temp directory is harness residue, not a product
				// defect, so this logs rather than failing. The path and error are
				// named so a genuine process leak is still visible.
				t.Logf("could not remove thread cwd %s after 3s: %v "+
					"(left for the OS; expected while a turn is in flight, since a "+
					"turn's children can outlive engine shutdown — investigate only "+
					"if it happens with no turn running, per 0163 P14)", dir, err)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return dir
}
