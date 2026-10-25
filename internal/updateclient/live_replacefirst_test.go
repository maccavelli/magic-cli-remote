package updateclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/buildinfo"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/selfupdatetest"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/launchd"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/taskscheduler"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// The live failing probe of MADR 0207 D36, run by `make live-replacefirst` on
// a host whose service runs a scratch daemon (the 0210 PLAN's kit; on Windows
// the real task, by W1 to W4). It is off unless
// MCREMOTE_REQUIRE_REPLACEFIRST=1, and then it requires:
//
//   - MCREMOTE_REPLACEFIRST_UNIT, the name the service was set up under, such
//     as mcremote-d3probe;
//   - MCREMOTE_REPLACEFIRST_BINARY, the absolute path of the binary it runs.
const (
	replaceFirstRequireEnv = "MCREMOTE_REQUIRE_REPLACEFIRST"
	replaceFirstUnitEnv    = "MCREMOTE_REPLACEFIRST_UNIT"
	replaceFirstBinaryEnv  = "MCREMOTE_REPLACEFIRST_BINARY"
)

// countingBackend is the real backend, with its Stop and Start calls counted.
type countingBackend struct {
	service.Backend
	stops, starts atomic.Int32
}

func (c *countingBackend) Stop(ctx context.Context, product string) error {
	c.stops.Add(1)
	return c.Backend.Stop(ctx, product)
}

func (c *countingBackend) Start(ctx context.Context, product string) error {
	c.starts.Add(1)
	return c.Backend.Start(ctx, product)
}

// TestLiveReplaceFirstProbeFailure is D36's live failing probe: against the
// host's real service backend, an update to a release that passes its staged
// probe but fails the same probe once installed is rolled back while the
// daemon runs. The service is never stopped or started, the daemon is the same
// process afterwards, by PID and start token, and the binary's bytes are
// unchanged.
func TestLiveReplaceFirstProbeFailure(t *testing.T) {
	if os.Getenv(replaceFirstRequireEnv) != "1" {
		t.Skipf("set %s=1, %s and %s to run it against a scratch service", replaceFirstRequireEnv,
			replaceFirstUnitEnv, replaceFirstBinaryEnv)
	}
	unit := os.Getenv(replaceFirstUnitEnv)
	target := os.Getenv(replaceFirstBinaryEnv)
	if unit == "" || target == "" || !filepath.IsAbs(target) {
		t.Fatalf("%s=1 needs %s and an absolute %s; got %q and %q", replaceFirstRequireEnv,
			replaceFirstUnitEnv, replaceFirstBinaryEnv, unit, target)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	target = resolved
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	// Whatever happens below, the scratch service keeps the binary it had.
	t.Cleanup(func() { restoreTarget(t, target, original) })

	backend, err := service.NewBackendWith("mcremote", unit, service.HealthOptions{})
	if err != nil {
		t.Fatalf("the real backend for %s: %v", unit, err)
	}
	ctx := context.Background()
	if running, err := backend.Running(ctx, "mcremote"); err != nil || !running {
		t.Fatalf("%s must be running before the test: running=%v, err=%v", unit, running, err)
	}
	pid, err := daemonPID(ctx, backend, target)
	if err != nil {
		t.Fatal(err)
	}
	token, ok := procutil.ProcessStartToken(pid)
	if !ok || token == "" {
		t.Fatalf("no start token for the daemon's PID %d", pid)
	}

	counted := &countingBackend{Backend: backend}
	prev := newBackend
	newBackend = func(string, string, service.HealthOptions) (service.Backend, error) { return counted, nil }
	t.Cleanup(func() { newBackend = prev })
	t.Setenv(fixtureFailNamedEnv, filepath.Base(target))

	spec, err := releaseSpec()
	if err != nil {
		t.Fatal(err)
	}
	gh := selfupdatetest.NewGitHubServer(t, Repository.Owner, Repository.Name,
		selfupdatetest.NewRelease("mcremote", "v0.2.0", spec.Targets(), releaseBody("v0.2.0")))
	up, err := New(Options{
		Product: "mcremote", Identity: buildinfo.Info{Version: "v0.1.0", Kind: buildinfo.KindRelease}, Managed: true,
		HTTPClient: gh.Client, APIBaseURL: gh.APIBase.String(), TargetPath: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := up.Run(ctx, selfupdate.Request{
		Product: "mcremote", CurrentVersion: "v0.1.0", CurrentBuild: selfupdate.ReleaseBuild, Yes: true,
	})
	if err == nil || !strings.Contains(err.Error(), "installed binary failed its probe") {
		t.Fatalf("err = %v, want the post-install probe's refusal", err)
	}
	t.Logf("result: applied=%v replaced_before_stop=%v rolled_back=%v; err: %v",
		res.Applied, res.ReplacedBeforeStop, res.RolledBack, err)
	if !res.ReplacedBeforeStop {
		t.Error("the binary was not replaced before the stop, so this run did not test D33")
	}
	if n, m := counted.stops.Load(), counted.starts.Load(); n != 0 || m != 0 {
		t.Errorf("the update called Stop %d and Start %d times, want neither", n, m)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
		t.Errorf("the binary is %s (%v), want its original bytes, %s", describe(got), err, describe(original))
	}
	if after, ok := procutil.ProcessStartToken(pid); !ok || after != token {
		t.Errorf("PID %d's start token is %q (ok=%v), want %q: the daemon was replaced", pid, after, ok, token)
	}
	if running, err := backend.Running(ctx, "mcremote"); err != nil || !running {
		t.Errorf("%s is no longer running: running=%v, err=%v", unit, running, err)
	}
}

// daemonPID is the PID of the service's running process: launchd's and Task
// Scheduler's own report on macOS and Windows, and on Linux, under any
// supervisor, the process whose executable is target.
func daemonPID(ctx context.Context, backend service.Backend, target string) (int, error) {
	switch b := backend.(type) {
	case interface {
		Status(context.Context, string) (launchd.Status, error)
	}:
		s, err := b.Status(ctx, "mcremote")
		if err != nil || s.PID <= 0 {
			return 0, fmt.Errorf("launchd reports no running PID: %+v, %v", s, err)
		}
		return s.PID, nil
	case interface {
		Status(context.Context, string) (taskscheduler.Status, error)
	}:
		s, err := b.Status(ctx, "mcremote")
		if err != nil || len(s.PIDs) != 1 {
			return 0, fmt.Errorf("Task Scheduler reports %d running instances, want one: %+v, %v", len(s.PIDs), s, err)
		}
		return int(s.PIDs[0]), nil
	}
	if runtime.GOOS != "linux" {
		return 0, fmt.Errorf("no way to find the daemon's PID on %s for %T", runtime.GOOS, backend)
	}
	return pidByExecutable(target)
}

// pidByExecutable finds the one process whose executable is target, through
// /proc.
func pidByExecutable(target string) (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil || exe != target || pid == os.Getpid() {
			continue
		}
		pids = append(pids, pid)
	}
	if len(pids) != 1 {
		return 0, fmt.Errorf("%d processes run %s, want exactly one daemon: %v", len(pids), target, pids)
	}
	return pids[0], nil
}

// restoreTarget puts original back at target when the run left other bytes
// there, through a temporary file and a rename, as an update would.
func restoreTarget(t *testing.T, target string, original []byte) {
	got, err := os.ReadFile(target)
	if err == nil && bytes.Equal(got, original) {
		return
	}
	t.Errorf("the binary at %s was left changed (%s, %v); restoring its original bytes", target, describe(got), err)
	info, statErr := os.Stat(target)
	mode := os.FileMode(0o755)
	if statErr == nil {
		mode = info.Mode().Perm()
	}
	tmp := target + ".replacefirst-restore"
	if err := os.WriteFile(tmp, original, mode); err != nil {
		t.Errorf("restore: %v", err)
		return
	}
	if err := os.Rename(tmp, target); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("restore: %v; the original bytes are at %s", err, tmp)
	}
}
