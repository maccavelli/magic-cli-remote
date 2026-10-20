package updateclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/go-selfupdate-lib/buildinfo"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/selfupdatetest"
	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// fakeService is a service manager seen through the library backend's
// interface. It records what the update asks of it.
type fakeService struct {
	installed, enabled, running bool
	stops, starts               int
}

var _ service.Backend = (*fakeService)(nil)

func (f *fakeService) Installed(context.Context, string) (bool, error) { return f.installed, nil }
func (f *fakeService) Enabled(context.Context, string) (bool, error)   { return f.enabled, nil }
func (f *fakeService) Running(context.Context, string) (bool, error)   { return f.running, nil }
func (f *fakeService) WaitHealthy(context.Context, string) error       { return nil }

func (f *fakeService) Stop(context.Context, string) error {
	f.stops++
	f.running = false
	return nil
}

func (f *fakeService) Start(context.Context, string) error {
	f.starts++
	f.running = true
	return nil
}

// withBackend makes the update see backend, or fail to build one with err.
func withBackend(t *testing.T, backend service.Backend, err error) {
	t.Helper()
	prev := newBackend
	newBackend = func(string, string, service.HealthOptions) (service.Backend, error) { return backend, err }
	t.Cleanup(func() { newBackend = prev })
}

// withDefinition makes the fallback probe report installed, or fail with err.
func withDefinition(t *testing.T, installed bool, err error) {
	t.Helper()
	prev := definitionInstalled
	definitionInstalled = func(string) (bool, error) { return installed, err }
	t.Cleanup(func() { definitionInstalled = prev })
}

// refreshStdout is what the real `setup-service --refresh --json` prints for
// res: the receipt the library's ExecReconciler reads.
func refreshStdout(t *testing.T, res service.RefreshResult) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := service.PrintRefreshResult(&buf, res, "mcremote", true); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withChild makes the reconcile child answer with out, and records each run.
func withChild(t *testing.T, out libservice.Output) *[]libservice.Command {
	t.Helper()
	var runs []libservice.Command
	prev := reconcileRunner
	reconcileRunner = libservice.RunnerFunc(func(_ context.Context, c libservice.Command) (libservice.Output, error) {
		runs = append(runs, c)
		return out, nil
	})
	t.Cleanup(func() { reconcileRunner = prev })
	return &runs
}

func releaseBody(tag string) func(selfupdate.Platform) []byte {
	return func(p selfupdate.Platform) []byte { return []byte("mcremote " + tag + " " + p.OS + "/" + p.Arch) }
}

// runUpdate runs `mcremote update --yes` from v0.1.0 to v0.2.0 through New,
// against a fake GitHub release, on a temporary executable. It returns the
// executable's path.
func runUpdate(t *testing.T) (string, error) {
	t.Helper()
	return runUpdateWith(t, service.HealthOptions{})
}

// runUpdateWith is runUpdate with the product's health wiring.
func runUpdateWith(t *testing.T, health service.HealthOptions) (string, error) {
	t.Helper()
	spec, err := releaseSpec()
	if err != nil {
		t.Fatal(err)
	}
	gh := selfupdatetest.NewGitHubServer(t, Repository.Owner, Repository.Name,
		selfupdatetest.NewRelease("mcremote", "v0.1.0", spec.Targets(), releaseBody("v0.1.0")),
		selfupdatetest.NewRelease("mcremote", "v0.2.0", spec.Targets(), releaseBody("v0.2.0")))
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := "mcremote"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe := filepath.Join(dir, name)
	if err := os.WriteFile(exe, []byte("mcremote v0.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Product: "mcremote", Identity: buildinfo.Info{Version: "v0.1.0", Kind: buildinfo.KindRelease}, Managed: true,
		HTTPClient: gh.Client, APIBaseURL: gh.APIBase.String(), TargetPath: exe,
		Health: health,
	}
	up, err := New(opts)
	if err != nil {
		return exe, err
	}
	res, err := up.Run(context.Background(), selfupdate.Request{
		Product: "mcremote", CurrentVersion: "v0.1.0", CurrentBuild: selfupdate.ReleaseBuild, Yes: true,
	})
	if err == nil && !res.Applied {
		err = fmt.Errorf("the update applied nothing: %+v", res)
	}
	return exe, err
}

func requireReplaced(t *testing.T, exe string) {
	t.Helper()
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	want := releaseBody("v0.2.0")(selfupdate.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH})
	if !bytes.Equal(got, want) {
		t.Fatalf("executable = %q, want the v0.2.0 binary %q", got, want)
	}
}

// TestUpdateStartsAStoppedServiceOnlyWhenEnabled is MADR 0054 D2 and the
// draft's amendment 1, "enabled ⇒ heal". The library used before (v1.4.1, the
// fleet library's selfupdate package) started any installed service after an
// update. go-selfupdate-lib starts a stopped one only when the backend reports
// it enabled, which the old adapter could not do, so a rehome without the
// backends left every stopped service down (MADR 0054, 2026-10-13 amendment).
func TestUpdateStartsAStoppedServiceOnlyWhenEnabled(t *testing.T) {
	for _, tc := range []struct {
		name                string
		enabled, running    bool
		wantStops, wantRuns int
	}{
		{"running is restarted", true, true, 1, 1},
		{"enabled and stopped is started", true, false, 0, 1},
		{"disabled and stopped stays stopped", false, false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{installed: true, enabled: tc.enabled, running: tc.running}
			withBackend(t, svc, nil)
			withChild(t, libservice.Output{Stdout: refreshStdout(t, service.RefreshResult{Verdict: service.VerdictUnchanged})})
			exe, err := runUpdate(t)
			if err != nil {
				t.Fatal(err)
			}
			requireReplaced(t, exe)
			if svc.stops != tc.wantStops || svc.starts != tc.wantRuns {
				t.Errorf("stops=%d starts=%d, want stops=%d starts=%d", svc.stops, svc.starts, tc.wantStops, tc.wantRuns)
			}
		})
	}
}

// probingService is a backend whose WaitHealthy runs the product's probe, as
// the systemd, launchd and Task Scheduler backends run Options.Probe once
// their own check has passed.
type probingService struct {
	fakeService
	probe  service.ServeProbe
	probes int
}

func (p *probingService) WaitHealthy(ctx context.Context, _ string) error {
	if p.probe == nil {
		return nil
	}
	return libservice.PollHealthy(ctx, func(ctx context.Context) (libservice.Health, error) {
		p.probes++
		return p.probe(ctx, service.ServeArgs{})
	}, libservice.PollOptions{Interval: 10 * time.Millisecond, Timeout: 300 * time.Millisecond, Settle: -1})
}

// withProbingBackend makes the update see svc, wired with the health options
// the update passes it.
func withProbingBackend(t *testing.T, svc *probingService) {
	t.Helper()
	prev := newBackend
	newBackend = func(_, _ string, h service.HealthOptions) (service.Backend, error) {
		svc.probe = h.Probe
		return svc, nil
	}
	t.Cleanup(func() { newBackend = prev })
}

// TestUpdateRunsTheProductsProbe: the product's probe reaches the backend,
// and a ready probe completes the update (MADR 0054 D5).
func TestUpdateRunsTheProductsProbe(t *testing.T) {
	svc := &probingService{fakeService: fakeService{installed: true, enabled: true, running: true}}
	withProbingBackend(t, svc)
	withChild(t, libservice.Output{Stdout: refreshStdout(t, service.RefreshResult{Verdict: service.VerdictUnchanged})})
	exe, err := runUpdateWith(t, service.HealthOptions{Probe: func(context.Context, service.ServeArgs) (libservice.Health, error) {
		return libservice.Health{Ready: true}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	requireReplaced(t, exe)
	if svc.probes == 0 {
		t.Fatal("the update never ran the product's probe")
	}
}

// TestUpdateRollsBackWhenTheProbeNeverPasses: a new daemon the service
// manager reports running, but whose own check never passes, such as one
// crash-looping before its admin socket opens, fails health, and the update
// puts the previous binary back (MADR 0054 D5; PLAN S8 Verification).
func TestUpdateRollsBackWhenTheProbeNeverPasses(t *testing.T) {
	svc := &probingService{fakeService: fakeService{installed: true, enabled: true, running: true}}
	withProbingBackend(t, svc)
	withChild(t, libservice.Output{Stdout: refreshStdout(t, service.RefreshResult{Verdict: service.VerdictUnchanged})})
	exe, err := runUpdateWith(t, service.HealthOptions{Probe: func(context.Context, service.ServeArgs) (libservice.Health, error) {
		return libservice.Health{Detail: "admin ping: the daemon keeps restarting"}, nil
	}})
	if err == nil {
		t.Fatal("the update succeeded although the probe never passed")
	}
	if !errors.Is(err, libservice.ErrTimeout) {
		t.Errorf("err = %v, want the health wait's timeout", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "mcremote v0.1.0" {
		t.Errorf("executable = %q, want the previous binary restored", got)
	}
}

// TestUpdateRunsTheNewBinarysRefresh: reconciliation runs the binary the
// update just installed, which alone can render the definition its release
// ships (MADR 0100, 0054 D4).
func TestUpdateRunsTheNewBinarysRefresh(t *testing.T) {
	withBackend(t, &fakeService{installed: true, enabled: true, running: true}, nil)
	runs := withChild(t, libservice.Output{Stdout: refreshStdout(t, service.RefreshResult{Verdict: service.VerdictUnchanged})})
	exe, err := runUpdate(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(*runs) != 1 {
		t.Fatalf("reconcile ran %d times, want once: %+v", len(*runs), *runs)
	}
	got := (*runs)[0]
	if got.Path != exe || !slices.Equal(got.Args, []string{"setup-service", "--refresh", "--json"}) {
		t.Errorf("reconcile ran %s %q, want %s setup-service --refresh --json", got.Path, got.Args, exe)
	}
}

// TestUpdateWithoutAServiceManager is the rule for a backend that cannot be
// built: refuse when a definition exists, since the update could neither stop
// nor restart it; replace the binary alone when none does (MADR 0054 PLAN S1).
func TestUpdateWithoutAServiceManager(t *testing.T) {
	unreachable := errors.New("selfupdate: systemd: user scope needs XDG_RUNTIME_DIR")

	t.Run("a definition exists: refuse", func(t *testing.T) {
		withBackend(t, nil, unreachable)
		withDefinition(t, true, nil)
		exe, err := runUpdate(t)
		if err == nil || !errors.Is(err, unreachable) || !strings.Contains(err.Error(), "service is installed") {
			t.Fatalf("err = %v, want a refusal naming the installed service and %v", err, unreachable)
		}
		if got, _ := os.ReadFile(exe); string(got) != "mcremote v0.1.0" {
			t.Errorf("the refused update changed the executable: %q", got)
		}
	})

	t.Run("no definition: binary only", func(t *testing.T) {
		withBackend(t, nil, unreachable)
		withDefinition(t, false, nil)
		runs := withChild(t, libservice.Output{})
		exe, err := runUpdate(t)
		if err != nil {
			t.Fatal(err)
		}
		requireReplaced(t, exe)
		if len(*runs) != 0 {
			t.Errorf("a binary-only update ran the reconcile child: %+v", *runs)
		}
	})

	t.Run("cannot tell: refuse", func(t *testing.T) {
		withBackend(t, nil, unreachable)
		withDefinition(t, false, errors.New("stat failed"))
		if _, err := runUpdate(t); err == nil || !errors.Is(err, unreachable) {
			t.Fatalf("err = %v, want a refusal", err)
		}
	})
}

// TestReconcilerReadsTheRefreshReceipt round-trips what `setup-service
// --refresh --json` prints through the library's ExecReconciler. The receipt
// carries no schema_version, which the library reads as schema 1 (MADR 0054
// D4, amendment of 2026-10-19).
func TestReconcilerReadsTheRefreshReceipt(t *testing.T) {
	warning := "the definition runs /opt/old/mcremote, not this binary"
	withChild(t, libservice.Output{Stdout: refreshStdout(t, service.RefreshResult{
		Verdict: service.VerdictRefreshed, Path: "/u/mcremote.service", BackupPath: "/u/mcremote.service.prev",
		Changed: true, Reloaded: true, Warnings: []string{warning},
	})})
	var restored []string
	prev := restoreUnitBackup
	restoreUnitBackup = func(path, backup string) error { restored = []string{path, backup}; return nil }
	t.Cleanup(func() { restoreUnitBackup = prev })

	rec, err := newReconciler()
	if err != nil {
		t.Fatal(err)
	}
	res, err := rec.Reconcile(context.Background(), "mcremote", "/new/mcremote")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || !strings.Contains(res.Detail, "refreshed /u/mcremote.service") {
		t.Errorf("result = %+v", res)
	}
	if !slices.Contains(res.Warnings.List(), warning) {
		t.Errorf("warnings = %q, want %q carried through", res.Warnings.List(), warning)
	}
	if err := rec.Restore(context.Background(), "mcremote", res); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(restored, []string{"/u/mcremote.service", "/u/mcremote.service.prev"}) {
		t.Errorf("restored %q, want the receipt's path and backup", restored)
	}
}

// TestReconcilerRestoresOnlyAChangedDefinition: a receipt that changed
// nothing has no backup to put back.
func TestReconcilerRestoresOnlyAChangedDefinition(t *testing.T) {
	withChild(t, libservice.Output{Stdout: refreshStdout(t, service.RefreshResult{Verdict: service.VerdictUnchanged, Path: "/u/mcremote.service"})})
	prev := restoreUnitBackup
	restoreUnitBackup = func(string, string) error { t.Error("restored an unchanged definition"); return nil }
	t.Cleanup(func() { restoreUnitBackup = prev })
	rec, err := newReconciler()
	if err != nil {
		t.Fatal(err)
	}
	res, err := rec.Reconcile(context.Background(), "mcremote", "/new/mcremote")
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Restore(context.Background(), "mcremote", res); err != nil {
		t.Fatal(err)
	}
}

// TestReconcilerRefusesAFailedChild: a child that fails, as an older binary
// without --refresh does, or that prints no receipt, is an error, and the
// update rolls back (MADR 0005, a reconcile failure is fatal).
func TestReconcilerRefusesAFailedChild(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  libservice.Output
		want string
	}{
		{"older binary", libservice.Output{Stderr: []byte("unknown flag: --refresh"), ExitCode: 1}, "unknown flag: --refresh"},
		{"not a receipt", libservice.Output{Stdout: []byte("not json at all")}, "malformed receipt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withChild(t, tc.out)
			rec, err := newReconciler()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := rec.Reconcile(context.Background(), "mcremote", "/new/mcremote"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestSignOptionsUseTheFixedIdentifier: the update signs under the identifier
// `make codesign-maybe` uses, so TCC grants survive it (MADR 0069). The old
// transformer took codesign's default, the staging file's name.
func TestSignOptionsUseTheFixedIdentifier(t *testing.T) {
	for _, product := range []string{"mcremote", "mcrelay"} {
		o := signOptions(product, "Developer ID")
		if o.Identifier != "com.magiccliremote."+product || o.Identity != "Developer ID" {
			t.Errorf("signOptions(%s) = %+v", product, o)
		}
		if o.Codesign != "" {
			t.Errorf("codesign path %q, want the library's /usr/bin/codesign", o.Codesign)
		}
	}
}

// TestNoTransformWithoutAnIdentityOrOffMacOS: signing is a macOS concern, and
// only with an identity configured.
func TestNoTransformWithoutAnIdentityOrOffMacOS(t *testing.T) {
	for _, id := range []string{"", "   ", "\t"} {
		if tr, err := newTransformer("mcremote", id); tr != nil || err != nil {
			t.Errorf("newTransformer(%q) = %v, %v; want nil, nil", id, tr, err)
		}
	}
	if runtime.GOOS != "darwin" {
		if tr, err := newTransformer("mcremote", "Developer ID"); tr != nil || err != nil {
			t.Errorf("newTransformer off macOS = %v, %v; want nil, nil", tr, err)
		}
	}
}
