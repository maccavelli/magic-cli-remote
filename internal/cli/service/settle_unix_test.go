//go:build unix

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupVerifiesTheStart: a started service is watched; one confirmed not
// running fails setup, with the definition and its records left for --remove.
// --no-start watches nothing. A bad wait is refused before anything is written.
func TestSetupVerifiesTheStart(t *testing.T) {
	defer OverrideInstallOS("linux")()
	defer OverrideRunSystemctl(func(...string) error { return nil })()
	prev := verifyStart
	verifyStart = realVerifyStart
	t.Cleanup(func() { verifyStart = prev })
	fastSettle(t)
	t.Setenv(waitEnv, "3")
	record := withUnitRecord(t, "mcremote", "")
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	opts := func(unitDir string) Options {
		return Options{Product: "mcremote", Binary: bin, UnitDir: unitDir, ConfigPath: filepath.Join(dir, "c.yaml"), NoLinger: true}
	}

	systemdShows(t, show("active", "running", 0))
	res, err := Setup(opts(filepath.Join(dir, "ok")))
	if err != nil || res.Health != HealthRunning {
		t.Fatalf("a running service: %+v, %v", res, err)
	}

	systemdShows(t, show("failed", "failed", 0))
	failDir := filepath.Join(dir, "fail")
	if _, err := Setup(opts(failDir)); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("a failed service: %v, want setup to fail", err)
	}
	if _, err := os.Stat(filepath.Join(failDir, "mcremote.service")); err != nil {
		t.Fatalf("the definition of a service that failed to start is gone: %v", err)
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatalf("the record of a service that failed to start is gone: %v", err)
	}

	systemdShows(t, show("failed", "failed", 0))
	o := opts(filepath.Join(dir, "nostart"))
	o.NoStart = true
	if res, err := Setup(o); err != nil || res.Health != "" {
		t.Fatalf("--no-start: %+v, %v; want no watch", res, err)
	}

	t.Setenv(waitEnv, "abc")
	badDir := filepath.Join(dir, "bad")
	if _, err := Setup(opts(badDir)); err == nil || !strings.Contains(err.Error(), waitEnv) {
		t.Fatalf("a bad wait: %v, want a refusal naming %s", err, waitEnv)
	}
	if _, err := os.Stat(badDir); !os.IsNotExist(err) {
		t.Fatalf("a refused wait wrote %s: %v", badDir, err)
	}
}
