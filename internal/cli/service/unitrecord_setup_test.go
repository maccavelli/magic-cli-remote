//go:build unix

// Setup and refresh render systemd units here, as refresh_test.go does.
// Windows registers its task under the product's name (setup_schtasks.go).

package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupRecordsAndRemoveDeletesTheUnitName: setup-service records the
// name it installed under; --refresh and --remove find that unit with no
// --unit-name; removing it removes the record (MADR 0201 D30).
func TestSetupRecordsAndRemoveDeletesTheUnitName(t *testing.T) {
	record := withUnitRecord(t, "mcremote", "")
	defer OverrideInstallOS("linux")()
	defer OverrideRunSystemctl(func(...string) error { return nil })()
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(dir, "units")
	if _, err := Setup(Options{
		Product: "mcremote", UnitName: "custom", Binary: bin, UnitDir: unitDir,
		ConfigPath: filepath.Join(dir, "config.yaml"), NoEnable: true, NoStart: true, NoLinger: true,
	}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(record); err != nil || strings.TrimSpace(string(b)) != "custom" {
		t.Fatalf("record = %q, %v; want custom", b, err)
	}

	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: unitDir}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got.Path) != "custom.service" {
		t.Errorf("refresh found %q, want the recorded custom.service", got.Path)
	}

	res, err := Remove(Options{Product: "mcremote", UnitDir: unitDir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Removed || filepath.Base(res.UnitPath) != "custom.service" {
		t.Errorf("remove = %+v, want custom.service removed", res)
	}
	if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the record survived the removal: %v", err)
	}
}
