package daemon

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/launchd"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/taskscheduler"
)

// The two backends whose handoffs outlive their run fit handOffCleaner, on
// every OS this compiles for (MADR 0210 D5, D11).
var (
	_ handOffCleaner = (*launchd.Job)(nil)
	_ handOffCleaner = (*taskscheduler.Task)(nil)
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

// TestCleanupHandOffsOnMacOSAndWindows: the daemon clears the finished
// one-shot handoffs once at start, launchd's jobs on macOS (MADR 0210 D5) and
// Task Scheduler's tasks on Windows (D11).
func TestCleanupHandOffsOnMacOSAndWindows(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		c := &fakeCleaner{}
		if built, logged := cleanupWith(goos, c, nil); built != 1 || c.calls != 1 || logged != "" {
			t.Fatalf("%s: built %d, cleanup calls %d, logged %q; want 1, 1, nothing", goos, built, c.calls, logged)
		}
	}
}

// TestCleanupHandOffsNotOnLinux: a systemd handoff is a transient unit,
// which leaves nothing behind, so on Linux no backend is built and nothing is
// called.
func TestCleanupHandOffsNotOnLinux(t *testing.T) {
	c := &fakeCleaner{}
	if built, _ := cleanupWith("linux", c, nil); built != 0 || c.calls != 0 {
		t.Fatalf("built %d, cleanup calls %d; want none", built, c.calls)
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

// TestHandOffBackendCleansUp: the backend the daemon builds on macOS is
// launchd's, and on Windows Task Scheduler's, and each has CleanupHandOffs,
// so the start-up cleanup is not a silent no-op. Building it runs neither
// launchctl nor Task Scheduler; TestMain isolates HOME.
func TestHandOffBackendCleansUp(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("a systemd handoff leaves nothing to clean up")
	}
	b, err := newHandOffBackend()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.(handOffCleaner); !ok {
		t.Fatalf("the %s backend %T has no CleanupHandOffs", runtime.GOOS, b)
	}
}

// TestCleanupHandOffsSkipsAnotherBackend: a backend without CleanupHandOffs
// is left alone.
func TestCleanupHandOffsSkipsAnotherBackend(t *testing.T) {
	if built, logged := cleanupWith("darwin", struct{}{}, nil); built != 1 || logged != "" {
		t.Fatalf("built %d, logged %q; want 1, nothing", built, logged)
	}
}
