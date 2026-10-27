package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain points the unit-name record at a scratch directory for the whole
// test binary, the external service_test files included: Setup writes the
// record, and no test may write it into the developer's real state directory
// (MADR 0201 D30).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "service-unit-record-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	unitRecordDir = func(product string) (string, error) { return filepath.Join(dir, product), nil }
	// A Windows remove deletes the refresh's backup of the task (MADR 0210
	// D9). The real lookup is the account's state directory, where a test
	// would delete the real task's backup. Tests of the lookup use
	// realTaskBackupFile.
	realTaskBackupFile = taskBackupFile
	taskBackupFile = func(_, name string) (string, error) {
		return filepath.Join(dir, "state", name+"-task.prev.xml"), nil
	}
	// The verified start (MADR 0054 PLAN S5c) would watch a real service
	// manager for up to 15 seconds after every test that starts one. Tests of
	// it put realVerifyStart back.
	realVerifyStart = verifyStart
	verifyStart = func(_ Kind, res Result) (Result, error) {
		res.Health = HealthRunning
		return res, nil
	}
	// Setup stops and starts through the library's backends (MADR 0207 D4),
	// which run the real systemctl, launchctl, Task Scheduler or supervisor,
	// and no test may touch the host's services. Each setup gets a fake that
	// does nothing; tests of the calls use their own (setupFake).
	realSetupBackend = setupBackend
	setupBackend = func(Kind, string, string, HealthOptions) (Backend, error) { return &setupFake{}, nil }
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// realVerifyStart is the verified start TestMain replaced.
var realVerifyStart func(Kind, Result) (Result, error)

// realSetupBackend is the backend constructor TestMain replaced.
var realSetupBackend func(Kind, string, string, HealthOptions) (Backend, error)

// realTaskBackupFile is the backup lookup TestMain replaced.
var realTaskBackupFile func(product, name string) (string, error)

// withUnitRecord gives the test its own record directory, holding name for
// product when name is not empty.
func withUnitRecord(t *testing.T, product, name string) string {
	t.Helper()
	dir := t.TempDir()
	prev := unitRecordDir
	unitRecordDir = func(string) (string, error) { return dir, nil }
	t.Cleanup(func() { unitRecordDir = prev })
	if name != "" {
		if err := os.WriteFile(filepath.Join(dir, unitRecordName), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, unitRecordName)
}

// TestControlUsesTheRecordedUnitName is MADR 0201 D30 (0073 §6.10(d)): with a
// recorded unit name, every backend is built with it, and every control path
// reads it; with none, they get the product name.
func TestControlUsesTheRecordedUnitName(t *testing.T) {
	t.Run("recorded", func(t *testing.T) {
		withUnitRecord(t, "mcremote", "custom")
		names, err := namesFor("mcremote", "")
		if err != nil {
			t.Fatal(err)
		}
		if want := (unitNames{Unit: "custom.service", Label: "com.magiccliremote.custom", Task: "custom"}); names != want {
			t.Errorf("namesFor = %+v, want %+v", names, want)
		}
	})
	t.Run("none recorded", func(t *testing.T) {
		withUnitRecord(t, "mcrelay", "")
		names, err := namesFor("mcrelay", "")
		if err != nil {
			t.Fatal(err)
		}
		if want := (unitNames{Unit: "mcrelay.service", Label: "com.magiccliremote.mcrelay", Task: "mcrelay"}); names != want {
			t.Errorf("namesFor = %+v, want %+v", names, want)
		}
	})
	t.Run("an explicit name wins", func(t *testing.T) {
		withUnitRecord(t, "mcremote", "custom")
		if names, err := namesFor("mcremote", "other"); err != nil || names.Unit != "other.service" {
			t.Errorf("namesFor(other) = %+v, %v", names, err)
		}
	})
	t.Run("a damaged record is an error", func(t *testing.T) {
		withUnitRecord(t, "mcremote", "../escape")
		if _, err := ResolveUnitName("mcremote"); err == nil || !strings.Contains(err.Error(), "not a unit name") {
			t.Errorf("ResolveUnitName = %v, want a refusal", err)
		}
	})
	t.Run("IsInstalled asks for the recorded unit", func(t *testing.T) {
		withUnitRecord(t, "mcremote", "custom")
		defer OverrideInstallOS("linux")()
		var asked []string
		prev := runSystemctlCapture
		runSystemctlCapture = func(args ...string) (string, error) {
			asked = append(asked, strings.Join(args, " "))
			return "loaded\n", nil
		}
		t.Cleanup(func() { runSystemctlCapture = prev })
		if ok, err := IsInstalled("mcremote"); err != nil || !ok {
			t.Fatalf("IsInstalled = %v, %v", ok, err)
		}
		if len(asked) != 1 || !strings.HasSuffix(asked[0], " custom.service") {
			t.Errorf("systemctl was asked %q, want the custom unit", asked)
		}
	})
	t.Run("doctor probes the recorded unit", func(t *testing.T) {
		withUnitRecord(t, "mcremote", "custom")
		defer OverrideInstallOS("linux")()
		var built []string
		prev := newProbeBackend
		newProbeBackend = func(product, unitName string) (Backend, error) {
			names, err := namesFor(product, unitName)
			built = append(built, names.Unit)
			return fakeBackend{installed: true, enabled: true, running: true}, err
		}
		t.Cleanup(func() { newProbeBackend = prev })
		st := ProbeStatus("mcremote")
		if st.PlistOrUnit != "custom.service" || len(built) != 1 || built[0] != "custom.service" {
			t.Errorf("status names %q, backend built for %q; want custom.service", st.PlistOrUnit, built)
		}
	})
}
