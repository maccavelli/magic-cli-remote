package codex

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// These tests are MADR 0137's amendment of 2026-10-15: the reality cache's
// lock covers the cache, never the `codex doctor` probe. Each stub writes a
// marker before it sleeps, so a test knows a probe is running without
// guessing from a fixed delay.

// waitForFile fails the test unless path exists within 10 s.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared: the probe did not start", filepath.Base(path))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitForRefreshIdle waits for a background refresh to finish, so one test's
// probe never lands in the next test's cache.
func waitForRefreshIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		realityCache.mu.Lock()
		busy := realityCache.refreshing
		realityCache.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a background reality refresh never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestNonBlockingProbeDoesNotWaitForARunningProbe: the phone's providers.list
// path must answer at once even while a refresh is running. Before the
// amendment the refresh held the cache lock for the whole probe, and this call
// waited behind it.
func TestNonBlockingProbeDoesNotWaitForARunningProbe(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	InvalidateRealityCache()
	t.Cleanup(func() { waitForRefreshIdle(t) })

	running := filepath.Join(t.TempDir(), "running")
	stub := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "codex-stub"),
		": > "+running+"\nsleep 2\nexit 1")

	if got := ObserveCredentialStoreCachedNonBlocking(stub, time.Minute); got != RealityUnknown {
		t.Fatalf("cold cache returned %v, want RealityUnknown", got)
	}
	waitForFile(t, running)

	start := time.Now()
	got := ObserveCredentialStoreCachedNonBlocking(stub, time.Minute)
	waited := time.Since(start)
	if got != RealityUnknown {
		t.Errorf("returned %v while the first refresh runs, want RealityUnknown", got)
	}
	if waited > 100*time.Millisecond {
		t.Fatalf("the non-blocking call waited %v for a running probe; it must return at once",
			waited.Round(time.Millisecond))
	}
}

// TestBlockingCallersShareOneProbe: with the lock off the probe, concurrent
// blocking callers (recovery, CheckBackend) must still share one probe rather
// than each starting `codex doctor`.
func TestBlockingCallersShareOneProbe(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	InvalidateRealityCache()

	runs := filepath.Join(t.TempDir(), "runs")
	stub := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "codex-stub"),
		"echo ran >> "+runs+"\nsleep 1\nexit 1")

	const callers = 5
	results := make([]StoreReality, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _ = ObserveCredentialStoreCached(context.Background(), stub, time.Minute)
		}()
	}
	wg.Wait()

	data, err := os.ReadFile(runs)
	if err != nil {
		t.Fatalf("no probe ran: %v", err)
	}
	if n := len(splitLines(string(data))); n != 1 {
		t.Fatalf("%d concurrent blocking callers started %d probes; want 1", callers, n)
	}
	for i, r := range results {
		if r != results[0] {
			t.Errorf("caller %d got %v, caller 0 got %v: sharers must get the same answer", i, r, results[0])
		}
	}
}

// slowDoctorBin is fakeDoctorBin with a marker and a 2 s delay. It reads the
// fixture before it sleeps, so a probe answers with the credential state as it
// was when the probe started.
func slowDoctorBin(t *testing.T, fixture string) (bin, control, running string) {
	t.Helper()
	testexec.SkipIfNoPOSIXShell(t)
	dir := t.TempDir()
	control = filepath.Join(dir, "which-fixture")
	running = filepath.Join(dir, "running")
	setDoctorFixture(t, control, fixture)
	bin = filepath.Join(dir, "codex")
	body := "if [ \"$1\" = \"doctor\" ]; then f=\"$(cat " + control + ")\"; : > " + running +
		"; sleep 2; cat \"$f\"; exit 1; fi\nexit 0\n"
	testexec.WriteShellStub(t, bin, body)
	return bin, control, running
}

// TestInvalidationDiscardsAProbeStartedBeforeIt: a sign-in invalidates the
// cache. A probe that started before it saw the old credential, so its answer
// must not be cached, and no later caller may wait on it. While the lock was
// held across the probe, invalidation could not run until the probe had
// stored its answer, so neither could happen.
func TestInvalidationDiscardsAProbeStartedBeforeIt(t *testing.T) {
	startStale := func(t *testing.T) (bin string, done <-chan struct{}) {
		t.Setenv("CODEX_HOME", t.TempDir())
		InvalidateRealityCache()
		bin, control, running := slowDoctorBin(t, "incomplete-file.json")
		ch := make(chan struct{})
		go func() {
			defer close(ch)
			_, _ = ObserveCredentialStoreCached(context.Background(), bin, time.Minute)
		}()
		waitForFile(t, running)
		// A sign-in lands while that probe runs.
		setDoctorFixture(t, control, "file-protected.json")
		InvalidateRealityCache()
		return bin, ch
	}

	t.Run("its answer is not cached", func(t *testing.T) {
		bin, done := startStale(t)
		<-done
		got, err := ObserveCredentialStoreCached(context.Background(), bin, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if got != RealityFileProtected {
			t.Fatalf("reality = %q after the sign-in, want %q: a probe started before "+
				"the invalidation cached its answer", got, RealityFileProtected)
		}
	})

	t.Run("a later caller does not wait on it", func(t *testing.T) {
		bin, done := startStale(t)
		defer func() { <-done }()
		got, err := ObserveCredentialStoreCached(context.Background(), bin, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if got != RealityFileProtected {
			t.Fatalf("reality = %q after the sign-in, want %q: the caller shared a probe "+
				"started before the invalidation", got, RealityFileProtected)
		}
	})
}
