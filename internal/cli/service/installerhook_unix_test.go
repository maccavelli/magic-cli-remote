//go:build unix

package service

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// hookHost is uninstallHost with product's backend recorded as kind, so the
// hook looks where setup-service would have written.
func hookHost(t *testing.T, kind Kind) (string, string) {
	t.Helper()
	bin, other, _ := uninstallHost(t, "linux")
	t.Cleanup(OverrideDetectedKind(kind))
	for _, p := range []string{"mcremote", "mcrelay"} {
		if err := recordBackend(p, kind); err != nil {
			t.Fatal(err)
		}
	}
	return bin, other
}

// TestFromInstallerFirstInstall: with no definition, mcremote's hook is a
// first install and mcrelay's does nothing (MADR 0054 amendment S7, H1).
func TestFromInstallerFirstInstall(t *testing.T) {
	hookHost(t, KindSystemdUser)
	for product, want := range map[string]string{"mcremote": HookSetup, "mcrelay": HookNone} {
		plan, err := PlanInstallerHook(Options{Product: product})
		if err != nil || plan.Action != want {
			t.Errorf("%s: plan %+v, %v; want %s", product, plan, err, want)
		}
	}
	// MCREMOTE_FORCE_SERVICE=1 sets up no relay that was never asked for.
	if plan, err := PlanInstallerHook(Options{Product: "mcrelay", Force: true}); err != nil || plan.Action != HookNone {
		t.Errorf("mcrelay with Force: plan %+v, %v; want none", plan, err)
	}
}

// TestFromInstallerLeavesAnotherInstallsDefinition: a unit that runs another
// folder's binary is reported and left as it is, even with Force, and the
// service is not touched (install.sh:666-670).
func TestFromInstallerLeavesAnotherInstallsDefinition(t *testing.T) {
	_, other := hookHost(t, KindSystemdUser)
	unit := writeDefinition(t, KindSystemdUser, "mcremote", "mcremote", filepath.Join(other, "mcremote"))
	before, err := os.ReadFile(unit)
	if err != nil {
		t.Fatal(err)
	}
	fb := &fakeHookBackend{running: true}
	withHookBackend(t, fb)
	for _, force := range []bool{false, true} {
		plan, err := PlanInstallerHook(Options{Product: "mcremote", Force: force})
		if err != nil || plan.Action != HookForeign || !strings.Contains(plan.Item.Reason, filepath.Join(other, "mcremote")) {
			t.Fatalf("force=%v: plan %+v, %v; want foreign, naming the other binary", force, plan, err)
		}
	}
	var out bytes.Buffer
	handled, err := RunInstallerHook(&out, Options{Product: "mcremote"})
	if !handled || err != nil || !strings.Contains(out.String(), "left alone: mcremote systemd-user service "+unit) {
		t.Fatalf("handled %v, %v, output:\n%s", handled, err, out.String())
	}
	if after, _ := os.ReadFile(unit); !bytes.Equal(before, after) {
		t.Fatalf("another install's unit was rewritten:\n%s", after)
	}
	if len(fb.calls) != 0 {
		t.Fatalf("another install's service was cycled: %v", fb.calls)
	}
}

// TestFromInstallerUpgradeKeepsBakedOptions: a second install refreshes the
// unit this folder owns, keeping --listen-port and dropping what the old
// template wrote, and restarts the service onto the new binary. Plain
// setup-service refuses this unit, because it differs from a default render.
func TestFromInstallerUpgradeKeepsBakedOptions(t *testing.T) {
	bin, _ := hookHost(t, KindSystemdUser)
	env := pinnedEnv()
	opts := Options{Product: "mcremote", UnitName: "mcremote", Binary: filepath.Join(bin, "mcremote"),
		ListenPort: 9099, WorkingDirectory: "/home/u", Description: "d"}
	unit := writeUnit(t, filepath.Join(xdgConfigHome(), "systemd", "user"), opts, &env)
	staleify(t, unit)
	fb := &fakeHookBackend{running: true}
	withHookBackend(t, fb)

	var out bytes.Buffer
	handled, err := RunInstallerHook(&out, Options{Product: "mcremote"})
	if !handled || err != nil {
		t.Fatalf("handled %v, %v; output:\n%s", handled, err, out.String())
	}
	b, err := os.ReadFile(unit)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "--listen-port 9099") {
		t.Errorf("the baked --listen-port was lost:\n%s", b)
	}
	if strings.Contains(string(b), "RestrictNamespaces=true") {
		t.Errorf("the old template's directive survived the refresh:\n%s", b)
	}
	if !slices.Equal(fb.calls, []string{"running", "stop", "start"}) {
		t.Errorf("calls %v, want a restart", fb.calls)
	}
	for _, w := range []string{"service definition refreshed", "mcremote service restarted on the new binary", "Running:           yes"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("output lacks %q:\n%s", w, out.String())
		}
	}
}

// TestFromInstallerPrintsTheAdvisories: after an upgrade, and after leaving
// another install's unit, mcremote's hook prints the AppArmor notice; mcrelay's
// does not (MADR 0054 amendment S7d, A1).
func TestFromInstallerPrintsTheAdvisories(t *testing.T) {
	bin, other := hookHost(t, KindSystemdUser)
	withApparmor(t, "1")
	withHookBackend(t, &fakeHookBackend{running: true})
	writeDefinition(t, KindSystemdUser, "mcremote", "mcremote", filepath.Join(bin, "mcremote"))
	writeDefinition(t, KindSystemdUser, "mcrelay", "mcrelay", filepath.Join(other, "mcrelay"))
	for product, want := range map[string]bool{"mcremote": true, "mcrelay": false} {
		var out bytes.Buffer
		if handled, err := RunInstallerHook(&out, Options{Product: product}); !handled || err != nil {
			t.Fatalf("%s: handled %v, %v", product, handled, err)
		}
		if has := strings.Contains(out.String(), "AppArmor restricts"); has != want {
			t.Errorf("%s: notice shown %v, want %v:\n%s", product, has, want, out.String())
		}
	}
}

// TestFromInstallerForceIsAFullSetup: with MCREMOTE_FORCE_SERVICE=1, a unit
// this folder owns is set up again with --force, as --force-service did.
func TestFromInstallerForceIsAFullSetup(t *testing.T) {
	bin, _ := hookHost(t, KindSystemdUser)
	writeDefinition(t, KindSystemdUser, "mcremote", "mcremote", filepath.Join(bin, "mcremote"))
	plan, err := PlanInstallerHook(Options{Product: "mcremote", Force: true})
	if err != nil || plan.Action != HookSetup {
		t.Fatalf("plan %+v, %v; want a setup", plan, err)
	}
}

// TestFromInstallerCyclesAsUpdateDoes: after the refresh, a running service is
// restarted, a stopped and enabled one is started, and a stopped and disabled
// one is left stopped (MADR 0054 PLAN S5f's rule), on a runit host.
func TestFromInstallerCyclesAsUpdateDoes(t *testing.T) {
	for _, tc := range []struct {
		name             string
		running, enabled bool
		calls            []string
		cycle, health    string
	}{
		{"running", true, true, []string{"running", "stop", "start"}, CycleRestarted, HealthRunning},
		{"stopped and enabled", false, true, []string{"running", "enabled", "start"}, CycleStarted, HealthRunning},
		{"stopped and disabled", false, false, []string{"running", "enabled"}, CycleLeftStopped, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, _ := hookHost(t, KindRunit)
			writeDefinition(t, KindRunit, "mcrelay", "mcrelay", filepath.Join(bin, "mcrelay"))
			fb := &fakeHookBackend{running: tc.running, enabled: tc.enabled}
			withHookBackend(t, fb)
			plan, err := PlanInstallerHook(Options{Product: "mcrelay"})
			if err != nil || plan.Action != HookUpgrade || plan.Kind != KindRunit {
				t.Fatalf("plan %+v, %v; want a runit upgrade", plan, err)
			}
			res, err := UpgradeFromInstaller(plan)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(fb.calls, tc.calls) || res.Cycle != tc.cycle || res.Health != tc.health {
				t.Fatalf("calls %v, cycle %q, health %q; want %v, %q, %q", fb.calls, res.Cycle, res.Health, tc.calls, tc.cycle, tc.health)
			}
		})
	}
}

// TestFromInstallerConfirmsOnTheBackendItCycled is MADR 0207 D4 and D40: the
// hook's backend is built with the product's health wiring, and the start
// check waits on that same backend, whose Stop recorded the instance the
// restart replaces.
func TestFromInstallerConfirmsOnTheBackendItCycled(t *testing.T) {
	bin, _ := hookHost(t, KindRunit)
	writeDefinition(t, KindRunit, "mcremote", "mcremote", filepath.Join(bin, "mcremote"))
	withRealVerifyStart(t)
	f := &setupFake{running: true}
	builds := withSetupBackend(t, f)
	health := HealthOptions{StopGrace: 7 * time.Second}
	plan, err := PlanInstallerHook(Options{Product: "mcremote", Health: health})
	if err != nil || plan.Action != HookUpgrade {
		t.Fatalf("plan %+v, %v; want an upgrade", plan, err)
	}
	res, err := UpgradeFromInstaller(plan)
	if err != nil || res.Health != HealthRunning {
		t.Fatalf("res %+v, %v; want a confirmed restart", res, err)
	}
	if want := []string{"stop", "start", "wait"}; !slices.Equal(f.got(), want) {
		t.Fatalf("calls %q, want %q on one backend", f.got(), want)
	}
	if len(*builds) != 1 || (*builds)[0].Kind != KindRunit || (*builds)[0].Health.StopGrace != health.StopGrace {
		t.Fatalf("builds %+v, want one runit backend with the plan's health", *builds)
	}
}

// TestFromInstallerFailsWhenTheStartFails: a start that fails, or a service
// the verified start finds not running, fails the hook, so the installer
// exits 3.
func TestFromInstallerFailsWhenTheStartFails(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		bin, _ := hookHost(t, KindRunit)
		writeDefinition(t, KindRunit, "mcremote", "mcremote", filepath.Join(bin, "mcremote"))
		withHookBackend(t, &fakeHookBackend{running: true, startErr: errors.New("sv: fatal")})
		plan, _ := PlanInstallerHook(Options{Product: "mcremote"})
		if _, err := UpgradeFromInstaller(plan); err == nil || !strings.Contains(err.Error(), "sv: fatal") {
			t.Fatalf("err = %v, want the start's", err)
		}
	})
	t.Run("verified start", func(t *testing.T) {
		bin, _ := hookHost(t, KindRunit)
		writeDefinition(t, KindRunit, "mcremote", "mcremote", filepath.Join(bin, "mcremote"))
		withHookBackend(t, &fakeHookBackend{running: true})
		prev := verifyStart
		verifyStart = func(_ Kind, res Result) (Result, error) {
			res.Health = HealthFailed
			return res, errors.New("mcremote is not running after setup")
		}
		t.Cleanup(func() { verifyStart = prev })
		plan, _ := PlanInstallerHook(Options{Product: "mcremote"})
		res, err := UpgradeFromInstaller(plan)
		if err == nil || res.Health != HealthFailed {
			t.Fatalf("res %+v, %v; want a failure", res, err)
		}
	})
}
