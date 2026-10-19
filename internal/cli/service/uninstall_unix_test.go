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
)

// uninstallHost is a scratch Linux host: its own home and XDG directories,
// per-product records, the fakes on PATH, and this binary's folder pinned. It
// returns that folder, another folder, and the records' root.
func uninstallHost(t *testing.T, osName string) (string, string, string) {
	t.Helper()
	t.Cleanup(OverrideInstallOS(osName))
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(k, filepath.Join(home, k))
	}
	rec := t.TempDir()
	prevRec := unitRecordDir
	unitRecordDir = func(product string) (string, error) { return filepath.Join(rec, product), nil }
	t.Cleanup(func() { unitRecordDir = prevRec })
	fakeHost(t, fakeStub(t), map[string]map[string]int{
		"systemctl": {}, "launchctl": {}, "sv": {}, "s6-svc": {}, "s6-svscanctl": {}, "rc-service": {}, "rc-update": {},
	})
	fakeProc(t)
	t.Cleanup(OverrideRunSystemctl(func(...string) error { return nil }))
	bin := t.TempDir()
	prevExe := uninstallExeDir
	uninstallExeDir = func() (string, error) { return bin, nil }
	t.Cleanup(func() { uninstallExeDir = prevExe })
	return bin, t.TempDir(), rec
}

// writeDefinition writes kind's definition of product named name, running
// binary, where setup-service would put it.
func writeDefinition(t *testing.T, kind Kind, product, name, binary string) string {
	t.Helper()
	env := pinnedEnv()
	opts := Options{Product: product, UnitName: name, Binary: binary, WorkingDirectory: "/home/u", Description: "d"}
	var path, body string
	var err error
	switch kind {
	case KindSystemdUser:
		path = filepath.Join(xdgConfigHome(), "systemd", "user", name+".service")
		body, err = renderWith(opts, &env)
	default:
		path = scriptDefFor(kind, name, "").Path
		body, err = renderScript(kind, opts, &env)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func items(list []UninstallItem) []string {
	var out []string
	for _, it := range list {
		out = append(out, it.Product+" "+string(it.Kind)+" "+filepath.Base(it.Path))
	}
	slices.Sort(out)
	return out
}

// TestUninstallRemovesWhatThisFolderRuns: every definition of either product,
// on every Linux service manager, that runs a program from this binary's
// folder is removed, its records with it; one that runs another folder's, or
// whose program cannot be read, is left and reported (MADR 0054 PLAN S5e).
func TestUninstallRemovesWhatThisFolderRuns(t *testing.T) {
	bin, other, rec := uninstallHost(t, "linux")
	remote, relay := filepath.Join(bin, "mcremote"), filepath.Join(bin, "mcrelay")
	gone := []string{
		writeDefinition(t, KindSystemdUser, "mcremote", "mcremote", remote),
		writeDefinition(t, KindSystemdUser, "mcremote", "custom", remote),
		writeDefinition(t, KindRunit, "mcrelay", "mcrelay", relay),
	}
	if err := recordUnitName("mcremote", "custom"); err != nil {
		t.Fatal(err)
	}
	if err := recordBackend("mcremote", KindSystemdUser); err != nil {
		t.Fatal(err)
	}
	kept := []string{writeDefinition(t, KindS6, "mcrelay", "mcrelay", filepath.Join(other, "mcrelay"))}
	handEdited := scriptDefFor(KindOpenRCUser, "mcremote", "").Path
	if err := os.MkdirAll(filepath.Dir(handEdited), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handEdited, []byte("#!/sbin/openrc-run\ncommand=/opt/mine\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	kept = append(kept, handEdited)

	res, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := items(res.Removed), []string{"mcrelay runit run", "mcremote systemd-user custom.service", "mcremote systemd-user mcremote.service"}; !slices.Equal(got, want) {
		t.Errorf("removed %v, want %v", got, want)
	}
	if got, want := items(res.Left), []string{"mcrelay s6 run", "mcremote openrc-user mcremote"}; !slices.Equal(got, want) {
		t.Errorf("left %v, want %v", got, want)
	}
	for _, it := range res.Left {
		if it.Reason == "" {
			t.Errorf("%s left with no reason", it.Path)
		}
	}
	for _, p := range gone {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived --uninstall: %v", p, err)
		}
	}
	for _, p := range kept {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed though it is not this folder's: %v", p, err)
		}
	}
	for _, f := range []string{unitRecordName, backendRecordName} {
		if _, err := os.Stat(filepath.Join(rec, "mcremote", f)); !os.IsNotExist(err) {
			t.Errorf("mcremote's %s record survived: %v", f, err)
		}
	}
	var out bytes.Buffer
	PrintUninstallResult(&out, res)
	for _, want := range []string{"removed: mcremote systemd-user service", "left:    mcrelay s6 service", "runs " + filepath.Join(other, "mcrelay")} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestUninstallWithNothingInstalled(t *testing.T) {
	uninstallHost(t, "linux")
	res, err := Uninstall()
	if err != nil || len(res.Removed)+len(res.Left)+len(res.Failed) != 0 {
		t.Fatalf("nothing installed: %+v, %v", res, err)
	}
	var out bytes.Buffer
	PrintUninstallResult(&out, res)
	if !strings.Contains(out.String(), "no service definition of mcremote or mcrelay was found") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// TestUninstallKeepsGoingPastAFailure: a definition that cannot be removed is
// reported, the others are still removed, and the command fails.
func TestUninstallKeepsGoingPastAFailure(t *testing.T) {
	bin, _, _ := uninstallHost(t, "linux")
	t.Cleanup(OverrideRunSystemctl(func(args ...string) error {
		if slices.Contains(args, "daemon-reload") {
			return errors.New("user bus unreachable")
		}
		return nil
	}))
	unit := writeDefinition(t, KindSystemdUser, "mcremote", "mcremote", filepath.Join(bin, "mcremote"))
	run := writeDefinition(t, KindRunit, "mcrelay", "mcrelay", filepath.Join(bin, "mcrelay"))
	res, err := Uninstall()
	if err == nil || len(res.Failed) != 1 || res.Failed[0].Path != unit || !strings.Contains(res.Failed[0].Reason, "user bus unreachable") {
		t.Fatalf("Uninstall: %+v, %v; want the unit failed with the reload error", res, err)
	}
	if _, err := os.Stat(run); !os.IsNotExist(err) {
		t.Fatalf("the runit definition was not removed after the unit failed: %v", err)
	}
}

// TestUninstallOnMacOS removes this folder's LaunchAgent.
func TestUninstallOnMacOS(t *testing.T) {
	bin, _, _ := uninstallHost(t, "darwin")
	t.Cleanup(OverrideRunLaunchctl(func(...string) error { return nil }))
	t.Cleanup(OverrideRunLaunchctlCapture(func(...string) (string, error) { return "", errors.New("not loaded") }))
	body, err := renderPlist(Options{Product: "mcremote", UnitName: "mcremote", Binary: filepath.Join(bin, "mcremote"), WorkingDirectory: "/home/u"})
	if err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "com.magiccliremote.mcremote.plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plist, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Uninstall()
	if err != nil || !slices.Equal(items(res.Removed), []string{"mcremote launchd-agent com.magiccliremote.mcremote.plist"}) {
		t.Fatalf("Uninstall: %+v, %v", res, err)
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Fatalf("the plist survived: %v", err)
	}
}
