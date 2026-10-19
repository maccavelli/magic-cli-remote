package service

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestUninstallOnWindows removes this folder's task, and only a task: the
// definition is read from Task Scheduler's export (MADR 0054 PLAN S5e).
func TestUninstallOnWindows(t *testing.T) {
	defer OverrideInstallOS("windows")()
	withUnitRecord(t, "mcremote", "")
	bin := t.TempDir()
	prevExe := uninstallExeDir
	uninstallExeDir = func() (string, error) { return bin, nil }
	t.Cleanup(func() { uninstallExeDir = prevExe })

	xml, err := renderTaskXML(Options{Product: "mcremote", UnitName: "mcremote", Binary: filepath.Join(bin, "mcremote.exe"),
		WorkingDirectory: bin, ConfigPath: filepath.Join(bin, "config.yaml")}, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	prevState, prevRun := taskState, runSchtasks
	t.Cleanup(func() { taskState, runSchtasks = prevState, prevRun })
	taskState = func(name string) (int, bool, error) { return 3, name == "mcremote", nil }
	var calls []string
	runSchtasks = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if slices.Equal(args, []string{"/query", "/tn", "mcremote", "/xml", "ONE"}) {
			return xml, nil
		}
		return "", nil
	}
	res, err := Uninstall()
	if err != nil || len(res.Removed) != 1 || res.Removed[0].Kind != KindWindowsTask || res.Removed[0].Product != "mcremote" {
		t.Fatalf("Uninstall: %+v, %v; want mcremote's task removed", res, err)
	}
	if !slices.Contains(calls, "/delete /tn mcremote /f") {
		t.Fatalf("schtasks calls %v, want the task deleted", calls)
	}
}
