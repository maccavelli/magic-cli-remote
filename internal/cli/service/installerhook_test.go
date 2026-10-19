package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeHookBackend is the service manager --from-installer cycles: it records
// each call, and Stop stops it.
type fakeHookBackend struct {
	running, enabled bool
	startErr         error
	calls            []string
	// onStop runs when the service stops.
	onStop func()
}

func (f *fakeHookBackend) Installed(context.Context, string) (bool, error) { return true, nil }

func (f *fakeHookBackend) Enabled(context.Context, string) (bool, error) {
	f.calls = append(f.calls, "enabled")
	return f.enabled, nil
}

func (f *fakeHookBackend) Running(context.Context, string) (bool, error) {
	f.calls = append(f.calls, "running")
	return f.running, nil
}

func (f *fakeHookBackend) Stop(context.Context, string) error {
	f.calls = append(f.calls, "stop")
	f.running = false
	if f.onStop != nil {
		f.onStop()
	}
	return nil
}

func (f *fakeHookBackend) Start(context.Context, string) error {
	f.calls = append(f.calls, "start")
	return f.startErr
}

func (f *fakeHookBackend) WaitHealthy(context.Context, string) error { return nil }

// withHookBackend makes fb the backend --from-installer cycles.
func withHookBackend(t *testing.T, fb *fakeHookBackend) {
	t.Helper()
	prev := newHookBackend
	newHookBackend = func(string, string) (Backend, error) { return fb, nil }
	t.Cleanup(func() { newHookBackend = prev })
}

// newRelease puts release's binaries in the new folder, as the library's
// install.ps1 does, and makes the new folder this binary's.
func newRelease(t *testing.T, release string, products ...string) {
	t.Helper()
	if err := os.MkdirAll(newWindowsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range products {
		if err := os.WriteFile(filepath.Join(newWindowsDir(), p+".exe"), []byte(p+" "+release+" (release)\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	prev := uninstallExeDir
	uninstallExeDir = func() (string, error) { return newWindowsDir(), nil }
	t.Cleanup(func() { uninstallExeDir = prev })
}

// TestFromInstallerMovesAnOldTaskWithoutDowngrading is an install.ps1 host
// upgraded by the library's installer: the new release is in the new folder,
// the task still runs the old one. The hook moves the task to the new
// release, keeps that release rather than copying the old one over it, moves
// the PATH, removes the old folders, and restarts the daemon (MADR 0054
// amendment S7, H1 and M1).
func TestFromInstallerMovesAnOldTaskWithoutDowngrading(t *testing.T) {
	ft, store := migrationHost(t, []string{"mcremote", "mcrelay"}, "mcremote")
	newRelease(t, "v0.3.0", "mcremote", "mcrelay")
	fb := &fakeHookBackend{running: true}
	withHookBackend(t, fb)

	plan, err := PlanInstallerHook(Options{Product: "mcremote"})
	if err != nil || plan.Action != HookUpgrade {
		t.Fatalf("plan %+v, %v; want an upgrade of the old task", plan, err)
	}
	res, err := UpgradeFromInstaller(plan)
	if err != nil {
		t.Fatal(err)
	}
	newExe := filepath.Join(newWindowsDir(), "mcremote.exe")
	if got := taskCommand(t, ft.xml["mcremote"]); !samePath(got, newExe) {
		t.Fatalf("the task runs %s, want %s", got, newExe)
	}
	for _, p := range []string{"mcremote", "mcrelay"} {
		if b, err := os.ReadFile(filepath.Join(newWindowsDir(), p+".exe")); err != nil || string(b) != p+" v0.3.0 (release)\n" {
			t.Errorf("%s in the new folder is %q (%v), want the new release kept", p, b, err)
		}
	}
	if _, err := os.Stat(oldWindowsDir("mcremote")); !os.IsNotExist(err) {
		t.Errorf("the old mcremote folder survived: %v", err)
	}
	if want := `C:\Windows;C:\Tools;` + newWindowsDir(); store.value != want {
		t.Errorf("User PATH %q, want %q", store.value, want)
	}
	if !slices.Equal(fb.calls, []string{"running", "stop", "start"}) || res.Cycle != CycleRestarted || res.Health != HealthRunning {
		t.Fatalf("calls %v, cycle %q, health %q; want a confirmed restart", fb.calls, res.Cycle, res.Health)
	}
}

// TestFromInstallerRemovesTheCopyTheRestartFreed: during the move the daemon
// still runs the old copy, which Windows will not delete; once the hook has
// restarted the daemon on the new folder's copy, the old one and its folder go.
func TestFromInstallerRemovesTheCopyTheRestartFreed(t *testing.T) {
	migrationHost(t, []string{"mcremote"}, "mcremote")
	newRelease(t, "v0.3.0", "mcremote")
	old := filepath.Join(oldWindowsDir("mcremote"), "mcremote.exe")
	inUse := true
	prev := removeOldCopy
	removeOldCopy = func(p string) error {
		if inUse && samePath(p, old) {
			return errors.New("the process cannot access the file")
		}
		return prev(p)
	}
	t.Cleanup(func() { removeOldCopy = prev })
	withHookBackend(t, &fakeHookBackend{running: true, onStop: func() { inUse = false }})

	plan, err := PlanInstallerHook(Options{Product: "mcremote"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpgradeFromInstaller(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldWindowsDir("mcremote")); !os.IsNotExist(err) {
		t.Fatalf("the old folder survived the restart that freed its copy: %v", err)
	}
}

// TestFromInstallerMovesAnOldTaskEvenWithForce: MCREMOTE_FORCE_SERVICE=1 on
// an old-folder host is still the move, because a plain setup --force would
// leave the old folders on the User PATH.
func TestFromInstallerMovesAnOldTaskEvenWithForce(t *testing.T) {
	migrationHost(t, []string{"mcremote"}, "mcremote")
	newRelease(t, "v0.3.0", "mcremote")
	plan, err := PlanInstallerHook(Options{Product: "mcremote", Force: true})
	if err != nil || plan.Action != HookUpgrade {
		t.Fatalf("plan %+v, %v; want the move", plan, err)
	}
}

// TestMigrationKeepsANewerBinary: a product already in the new folder is not
// overwritten with the old folder's copy; one that is not there is copied.
func TestMigrationKeepsANewerBinary(t *testing.T) {
	ft, _ := migrationHost(t, []string{"mcremote", "mcrelay"}, "mcremote")
	newRelease(t, "v0.3.0", "mcremote")
	if _, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := taskCommand(t, ft.xml["mcremote"]); !samePath(got, filepath.Join(newWindowsDir(), "mcremote.exe")) {
		t.Fatalf("the task runs %s", got)
	}
	for p, want := range map[string]string{"mcremote": "v0.3.0", "mcrelay": "v0.2.0"} {
		if b, err := os.ReadFile(filepath.Join(newWindowsDir(), p+".exe")); err != nil || string(b) != p+" "+want+" (release)\n" {
			t.Errorf("%s in the new folder is %q (%v), want %s", p, b, err, want)
		}
	}
}

// TestMigrationRefusesANewFolderBinaryThatDoesNotRun: a binary in the new
// folder that cannot report its identity is not switched to.
func TestMigrationRefusesANewFolderBinaryThatDoesNotRun(t *testing.T) {
	ft, _ := migrationHost(t, []string{"mcremote"}, "mcremote")
	newRelease(t, "v0.3.0", "mcremote")
	prev := binaryIdentity
	binaryIdentity = func(bin string) (string, error) {
		if strings.Contains(bin, installerName) {
			return "", errors.New("not a valid application")
		}
		return prev(bin)
	}
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Migrated != "" || !strings.Contains(strings.Join(res.Warnings, "\n"), "does not run") {
		t.Fatalf("res = %+v, want no move and the reason", res)
	}
	if got := taskCommand(t, ft.xml["mcremote"]); !inOldLayout("mcremote", got) {
		t.Fatalf("the task was switched to %s", got)
	}
}

// TestPrintInstallerHook: what the hook says for each plan it carries out.
func TestPrintInstallerHook(t *testing.T) {
	for _, tc := range []struct {
		plan HookPlan
		res  HookResult
		want []string
	}{
		{HookPlan{Product: "mcrelay", Action: HookNone}, HookResult{},
			[]string{"no mcrelay service is set up", "mcrelay setup-service", EnvWithRelayService + "=1"}},
		{HookPlan{Product: "mcremote", Action: HookForeign, Item: UninstallItem{Kind: KindRunit, Path: "/x/run", Reason: "runs /o/mcremote, not a program from /b"}}, HookResult{},
			[]string{"left alone: mcremote runit service /x/run — runs /o/mcremote"}},
		{HookPlan{Product: "mcremote", Action: HookUpgrade}, HookResult{Refresh: RefreshResult{Verdict: VerdictUnchanged, Path: "/u"}, Cycle: CycleRestarted, Health: HealthRunning},
			[]string{"service definition unchanged: /u", "mcremote service restarted on the new binary", "Running:           yes"}},
		{HookPlan{Product: "mcremote", Action: HookUpgrade}, HookResult{Refresh: RefreshResult{Verdict: VerdictUnchanged, Path: "/u"}, Cycle: CycleLeftStopped},
			[]string{"left stopped: it is not enabled"}},
	} {
		var out bytes.Buffer
		PrintInstallerHook(&out, tc.plan, tc.res)
		for _, w := range tc.want {
			if !strings.Contains(out.String(), w) {
				t.Errorf("%s/%s: output lacks %q:\n%s", tc.plan.Product, tc.plan.Action, w, out.String())
			}
		}
	}
}

// TestCheckFromInstaller: the flag refuses the other modes.
func TestCheckFromInstaller(t *testing.T) {
	if err := CheckFromInstaller(false, false, false, false); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][4]bool{{true}, {false, true}, {false, false, true}, {false, false, false, true}} {
		if err := CheckFromInstaller(c[0], c[1], c[2], c[3]); err == nil {
			t.Errorf("%v: accepted", c)
		}
	}
}
