package codex

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// TestNonBlockingProbeReturnsWhileTheProbeRuns is MADR 0137 F1, as MADR 0225
// D1 restates it.
//
// ObserveCredentialStoreCached runs `codex doctor --json` when the cache is
// cold — a subprocess plus a network-dependent call, measured at ~1.4 s. That
// was reachable from providers.list, which the phone issues on every connect,
// so a routine screen refresh could block on codex talking to its backend.
//
// The stub stands in for a doctor that has not finished: it records that it
// started, then waits until the test releases it. A call that returns while the
// stub still waits cannot have waited for the probe, however the goroutines are
// scheduled, and a call that waited for it cannot return within the bound. The
// test this replaces asserted instead that the stub had not started when the
// call returned; the background refresh is free to start it first, and in CI it
// did (MADR 0225 F1).
func TestNonBlockingProbeReturnsWhileTheProbeRuns(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	InvalidateRealityCache()

	const (
		// callBound is far above a non-blocking call and far below
		// providerauth.ProbeTimeout, which would end a probe the call waited on.
		callBound = 5 * time.Second
		// stubWait bounds the stub's own wait, so a stub the test never
		// releases cannot outlive the run.
		stubWait = 30 * time.Second
	)
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")
	finished := filepath.Join(dir, "finished")
	stub := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "codex-stub"),
		"echo ran >> "+started+"\n"+
			"i=0\n"+
			"while [ ! -e "+release+" ] && [ $i -lt "+strconv.Itoa(int(stubWait/(50*time.Millisecond)))+" ]; do\n"+
			"\tsleep 0.05\n\ti=$((i+1))\ndone\n"+
			"echo done >> "+finished+"\nexit 1")
	// However the test ends, a waiting stub is let go.
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })

	got := make(chan StoreReality, 1)
	go func() { got <- ObserveCredentialStoreCachedNonBlocking(stub, time.Minute) }()
	select {
	case r := <-got:
		if r != RealityUnknown {
			t.Fatalf("cold cache returned %v, want RealityUnknown: the caller must "+
				"get an answer now, not a correct one later", r)
		}
	case <-time.After(callBound):
		t.Fatalf("the call had not returned after %v with the probe still running: "+
			"it waits for the probe on the caller's goroutine", callBound)
	}
	// The stub cannot finish before it is released, so the call above returned
	// while the probe was unfinished, whether or not the probe had started.
	if _, err := os.Stat(finished); err == nil {
		t.Fatal("the stub finished before it was released: it did not stand in for a running probe")
	}

	// The background refresh does run, and runs to its end once released, so
	// the next caller gets a real answer.
	waitForFile(t, started, "the background refresh never started the probe; a "+
		"non-blocking probe that never probes would leave the cache cold forever")
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, finished, "the released probe never finished")
}

// TestNonBlockingProbeCoalescesConcurrentCallers proves a burst of
// providers.list calls spawns one probe, not one each.
//
// Without the in-flight flag every phone reconnect on a cold cache would fork
// a `codex doctor` — the opposite of the problem F1 set out to fix.
func TestNonBlockingProbeCoalescesConcurrentCallers(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	InvalidateRealityCache()

	marker := filepath.Join(t.TempDir(), "invocations")
	// Sleep so every caller in the burst arrives while the first probe is
	// still running; without coalescing each would start its own.
	stub := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "codex-stub"),
		"echo ran >> "+marker+"\nsleep 1\nexit 1")

	for i := range 20 {
		if got := ObserveCredentialStoreCachedNonBlocking(stub, time.Minute); got != RealityUnknown {
			t.Fatalf("call %d returned %v, want RealityUnknown", i, got)
		}
	}
	time.Sleep(2500 * time.Millisecond)

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("no probe ran at all: %v", err)
	}
	if n := len(splitLines(string(data))); n > 2 {
		t.Fatalf("20 concurrent callers started %d probes; they must coalesce", n)
	}
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
