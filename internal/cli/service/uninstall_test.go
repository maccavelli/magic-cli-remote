package service

import (
	"path/filepath"
	"slices"
	"testing"
)

// TestUninstallOnWindows removes this folder's task, and only a task: the
// program is read from the definition Task Scheduler stores (MADR 0054 PLAN
// S5e), and the task is deleted through it (MADR 0207 D17).
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
	s := withTaskScheduler(t)
	s.xml["mcremote"] = xml
	res, err := Uninstall()
	if err != nil || len(res.Removed) != 1 || res.Removed[0].Kind != KindWindowsTask || res.Removed[0].Product != "mcremote" {
		t.Fatalf("Uninstall: %+v, %v; want mcremote's task removed", res, err)
	}
	if !slices.Contains(s.calls, "definition mcremote") || !slices.Contains(s.calls, "delete mcremote") {
		t.Fatalf("Task Scheduler calls %q, want the program read from the definition and the task deleted", s.calls)
	}
	if _, ok := s.xml["mcremote"]; ok {
		t.Error("the task is still registered")
	}
}

// TestUninstallLeavesAnotherFoldersTask: a task whose definition runs a binary
// in another folder is not this folder's, and is neither reported removed nor
// deleted.
func TestUninstallLeavesAnotherFoldersTask(t *testing.T) {
	defer OverrideInstallOS("windows")()
	withUnitRecord(t, "mcremote", "")
	bin, other := t.TempDir(), t.TempDir()
	prevExe := uninstallExeDir
	uninstallExeDir = func() (string, error) { return bin, nil }
	t.Cleanup(func() { uninstallExeDir = prevExe })

	xml, err := renderTaskXML(Options{Product: "mcremote", UnitName: "mcremote", Binary: filepath.Join(other, "mcremote.exe"),
		WorkingDirectory: other}, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	s := withTaskScheduler(t)
	s.xml["mcremote"] = xml
	res, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 0 || slices.Contains(s.calls, "delete mcremote") {
		t.Errorf("removed %+v with calls %q; another folder's task must be left", res.Removed, s.calls)
	}
}
