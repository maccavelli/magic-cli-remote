package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// TestNonBlockingProbeRunsNoBinaryOnAColdCache is MADR 0137 F1.
//
// ObserveCredentialStoreCached runs `codex doctor --json` when the cache is
// cold — a subprocess plus a network-dependent call, measured at ~1.4 s. That
// was reachable from providers.list, which the phone issues on every connect,
// so a routine screen refresh could block on codex talking to its backend.
//
// The stub records every invocation to a file, so "no binary ran" is observed
// rather than inferred from timing.
func TestNonBlockingProbeRunsNoBinaryOnAColdCache(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	InvalidateRealityCache()

	marker := filepath.Join(t.TempDir(), "invoked")
	stub := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "codex-stub"),
		"echo ran >> "+marker+"\nexit 1")

	start := time.Now()
	got := ObserveCredentialStoreCachedNonBlocking(stub, time.Minute)
	elapsed := time.Since(start)

	if got != RealityUnknown {
		t.Fatalf("cold cache returned %v, want RealityUnknown: the caller must "+
			"get an answer now, not a correct one later", got)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("the call took %v; it must not wait for a probe", elapsed)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the binary ran before the call returned: the probe is still " +
			"synchronous on the caller's goroutine")
	}

	// The background refresh does run, so the next caller gets a real answer.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the background refresh never ran; a non-blocking probe that " +
				"never probes would leave the cache cold forever")
		}
		time.Sleep(20 * time.Millisecond)
	}
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

	for i := 0; i < 20; i++ {
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
