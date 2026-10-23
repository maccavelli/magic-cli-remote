package daemon

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"testing"
)

// fakeCleaner is a backend that clears finished handoff jobs, as launchd's
// does, and records each call.
type fakeCleaner struct {
	calls int
	err   error
}

func (f *fakeCleaner) CleanupHandOffs(context.Context) error {
	f.calls++
	return f.err
}

// cleanupWith runs cleanupHandOffs for goos against backend, and returns
// how many times the backend was built and what was logged.
func cleanupWith(goos string, backend any, buildErr error) (built int, logged string) {
	var b bytes.Buffer
	log := slog.New(slog.NewTextHandler(&b, nil))
	cleanupHandOffs(context.Background(), goos, func() (any, error) {
		built++
		return backend, buildErr
	}, log)
	return built, b.String()
}

// TestCleanupHandOffsOnMacOS: on macOS the daemon clears launchd's finished
// one-shot handoff jobs once at start (MADR 0210 D5).
func TestCleanupHandOffsOnMacOS(t *testing.T) {
	c := &fakeCleaner{}
	if built, logged := cleanupWith("darwin", c, nil); built != 1 || c.calls != 1 || logged != "" {
		t.Fatalf("built %d, cleanup calls %d, logged %q; want 1, 1, nothing", built, c.calls, logged)
	}
}

// TestCleanupHandOffsOnlyOnMacOS: only launchd leaves a finished handoff
// job behind, so elsewhere no backend is built and nothing is called.
func TestCleanupHandOffsOnlyOnMacOS(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		c := &fakeCleaner{}
		if built, _ := cleanupWith(goos, c, nil); built != 0 || c.calls != 0 {
			t.Fatalf("%s: built %d, cleanup calls %d; want none", goos, built, c.calls)
		}
	}
}

// TestCleanupHandOffsLogsAFailure: a failure to clean up, or to build the
// backend, is logged and never stops the daemon.
func TestCleanupHandOffsLogsAFailure(t *testing.T) {
	c := &fakeCleaner{err: errors.New("bootout failed")}
	if _, logged := cleanupWith("darwin", c, nil); !strings.Contains(logged, "bootout failed") ||
		!strings.Contains(logged, "level=WARN") {
		t.Fatalf("cleanup failure logged %q", logged)
	}
	if _, logged := cleanupWith("darwin", nil, errors.New("home dir: unset")); !strings.Contains(logged,
		"home dir: unset") {
		t.Fatalf("backend failure logged %q", logged)
	}
}

// TestHandOffBackendCleansUpOnMacOS: the backend the daemon builds on macOS is
// launchd's, and has CleanupHandOffs, so the start-up cleanup is not a silent
// no-op. Building it runs no launchctl; TestMain isolates HOME.
func TestHandOffBackendCleansUpOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only launchd leaves finished handoff jobs")
	}
	b, err := newHandOffBackend()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.(handOffCleaner); !ok {
		t.Fatalf("the macOS backend %T has no CleanupHandOffs", b)
	}
}

// TestCleanupHandOffsSkipsAnotherBackend: a backend without CleanupHandOffs
// is left alone.
func TestCleanupHandOffsSkipsAnotherBackend(t *testing.T) {
	if built, logged := cleanupWith("darwin", struct{}{}, nil); built != 1 || logged != "" {
		t.Fatalf("built %d, logged %q; want 1, nothing", built, logged)
	}
}
