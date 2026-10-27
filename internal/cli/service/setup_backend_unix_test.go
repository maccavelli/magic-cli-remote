//go:build unix

package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// TestSetupBuildsItsBackendByKindWithTheProductsHealth is MADR 0207 D40: setup
// builds the backend for the kind it detected, even where a record an earlier
// setup left names another, with the product's health wiring.
func TestSetupBuildsItsBackendByKindWithTheProductsHealth(t *testing.T) {
	defer OverrideInstallOS("linux")()
	defer OverrideRunSystemctl(func(...string) error { return nil })()
	withUnitRecord(t, "mcremote", "")
	if err := recordBackend("mcremote", KindRunit); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	builds := withSetupBackend(t, &setupFake{})
	health := HealthOptions{StopGrace: 3 * time.Second}
	if _, err := Setup(Options{Product: "mcremote", Binary: bin, UnitDir: filepath.Join(dir, "units"),
		ConfigPath: filepath.Join(dir, "c.yaml"), NoLinger: true, Health: health}); err != nil {
		t.Fatal(err)
	}
	if len(*builds) != 1 {
		t.Fatalf("builds %+v, want one", *builds)
	}
	b := (*builds)[0]
	if b.Kind != KindSystemdUser || b.Product != "mcremote" || b.Unit != "mcremote" || b.Health.StopGrace != health.StopGrace {
		t.Fatalf("built %+v; want systemd-user for mcremote with the product's health, whatever the record says", b)
	}
}

// TestSetupRefusesAWaitShorterThanTheDrain: on Windows the daemon is given
// Health.StopGrace to drain inside the timeout, and Task Scheduler refuses a
// grace that is not shorter (taskscheduler/task.go:149-150), so setup refuses
// such a wait before anything is written (MADR 0207 D42).
func TestSetupRefusesAWaitShorterThanTheDrain(t *testing.T) {
	defer OverrideInstallOS("windows")()
	s := withTaskScheduler(t)
	// A principal, so that without the refusal setup would go on to register.
	withTaskPrincipal(t, "S-1-5-21-7-7-7-1001", nil, `CORP\dev`, nil)
	dir := t.TempDir()
	opts := windowsOpts()
	// Setup checks the binary before the wait, and writes the default config
	// after it: both in a scratch directory.
	opts.Binary, opts.ConfigPath = filepath.Join(dir, "mcremote"), filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(opts.Binary, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	opts.Health = HealthOptions{StopGrace: 30 * time.Second, RequestStop: func(context.Context, ServeArgs) error { return nil }}
	t.Setenv(waitEnv, "30")
	if _, err := Setup(opts); err == nil || !strings.Contains(err.Error(), "drain") {
		t.Fatalf("a 30 s wait with a 30 s drain: %v, want a refusal naming the drain", err)
	}
	if len(s.calls) != 0 {
		t.Fatalf("Task Scheduler was called %q before the refusal", s.calls)
	}
	if _, err := os.Stat(opts.ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("a refused wait wrote %s: %v", opts.ConfigPath, err)
	}
	// mcrelay sends no stop request, so it has no drain to fit.
	opts.Health = HealthOptions{}
	if _, err := Setup(opts); err != nil && strings.Contains(err.Error(), "drain") {
		t.Fatalf("no stop request, refused for its drain: %v", err)
	}
}

// systemdHost is a Linux host with systemd detected and systemctl stubbed, the
// records in a scratch directory, and the host's own PATH, where setup's
// preflight looks for systemctl. It returns the unit directory and a binary.
func systemdHost(t *testing.T) (string, string) {
	t.Helper()
	t.Cleanup(OverrideInstallOS("linux"))
	t.Cleanup(OverrideRunSystemctl(func(...string) error { return nil }))
	withUnitRecord(t, "mcremote", "")
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "units"), bin
}

// TestRemoveStopsThroughTheBackend is MADR 0207 D4 for systemd and the script
// kinds: --remove stops through the backend before the definition goes, and a
// stop refused because this process runs inside the service removes nothing.
func TestRemoveStopsThroughTheBackend(t *testing.T) {
	for _, kind := range []Kind{KindSystemdUser, KindRunit, KindOpenRCUser} {
		t.Run(string(kind), func(t *testing.T) {
			var root, bin string
			if kind == KindSystemdUser {
				root, bin = systemdHost(t)
			} else {
				root, bin = supervisorHost(t, kind)
				recordSessionStarts(t)
			}
			unitDir := root
			if kind == KindOpenRCUser {
				unitDir = openRCRoot(t, root)
			}
			o := Options{Product: "mcremote", Binary: bin, UnitDir: unitDir, ConfigPath: filepath.Join(t.TempDir(), "c.yaml"), NoStart: true, NoLinger: true}
			res, err := Setup(o)
			if err != nil {
				t.Fatal(err)
			}

			f := &setupFake{stopErr: fmt.Errorf("%w: mcremote", libservice.ErrInsideService)}
			withSetupBackend(t, f)
			if _, err := Remove(Options{Product: "mcremote", UnitDir: unitDir}); err == nil || !strings.Contains(err.Error(), "inside") {
				t.Fatalf("Remove from inside: %v, want the refusal", err)
			}
			if _, err := os.Stat(res.UnitPath); err != nil {
				t.Fatalf("the definition was removed after a refused stop: %v", err)
			}

			f = &setupFake{running: true, onStop: func() {
				if _, err := os.Stat(res.UnitPath); err != nil {
					t.Errorf("the definition was gone before the stop: %v", err)
				}
			}}
			builds := withSetupBackend(t, f)
			if _, err := Remove(Options{Product: "mcremote", UnitDir: unitDir}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(f.got(), []string{"stop"}) || len(*builds) != 1 || (*builds)[0].Kind != kind {
				t.Fatalf("calls %q, builds %+v; want one stop on a %s backend", f.got(), *builds, kind)
			}
			if _, err := os.Stat(res.UnitPath); !os.IsNotExist(err) {
				t.Fatalf("the definition survived Remove: %v", err)
			}
		})
	}
}

// TestRemoveRecordsAFailedStop is MADR 0207 PLAN U6b: a stop that fails
// otherwise than from inside is tolerated, the definition removed, and the
// failure recorded, so the result does not say the service stopped. A service
// that is not installed had nothing to stop.
func TestRemoveRecordsAFailedStop(t *testing.T) {
	for _, kind := range []Kind{KindSystemdUser, KindRunit} {
		t.Run(string(kind), func(t *testing.T) {
			var root, bin string
			if kind == KindSystemdUser {
				root, bin = systemdHost(t)
			} else {
				root, bin = supervisorHost(t, kind)
				recordSessionStarts(t)
			}
			o := Options{Product: "mcremote", Binary: bin, UnitDir: root, ConfigPath: filepath.Join(t.TempDir(), "c.yaml"), NoStart: true, NoLinger: true}
			res, err := Setup(o)
			if err != nil {
				t.Fatal(err)
			}
			withSetupBackend(t, &setupFake{stopErr: fmt.Errorf("%w: mcremote still runs 75s after it was stopped", libservice.ErrTimeout)})
			got, err := Remove(Options{Product: "mcremote", UnitDir: root})
			if err != nil {
				t.Fatalf("Remove with a failed stop: %v, want it tolerated", err)
			}
			if !strings.Contains(got.StopFailure, "still runs 75s") {
				t.Fatalf("StopFailure = %q, want the stop's error", got.StopFailure)
			}
			if _, err := os.Stat(res.UnitPath); !os.IsNotExist(err) {
				t.Fatalf("the definition survived Remove: %v", err)
			}
		})
	}
	t.Run("not installed", func(t *testing.T) {
		root, bin := systemdHost(t)
		if _, err := Setup(Options{Product: "mcremote", Binary: bin, UnitDir: root, ConfigPath: filepath.Join(t.TempDir(), "c.yaml"), NoStart: true, NoLinger: true}); err != nil {
			t.Fatal(err)
		}
		withSetupBackend(t, &setupFake{stopErr: libservice.ErrNotInstalled})
		if got, err := Remove(Options{Product: "mcremote", UnitDir: root}); err != nil || got.StopFailure != "" {
			t.Fatalf("Remove of a service not installed: %+v, %v; want no failure", got, err)
		}
	})
}
