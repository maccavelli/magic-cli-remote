//go:build unix

package service

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// fastSettle makes this package's polls tick without waiting, for one test.
// It and withRealVerifyStart are here, under the unix tag, because only unix
// tests use them.
func fastSettle(t *testing.T) {
	t.Helper()
	prev := settleTick
	settleTick = time.Millisecond
	t.Cleanup(func() { settleTick = prev })
}

// withRealVerifyStart puts the start check TestMain replaced back, for one
// test.
func withRealVerifyStart(t *testing.T) {
	t.Helper()
	prev := verifyStart
	verifyStart = realVerifyStart
	t.Cleanup(func() { verifyStart = prev })
}

// TestSetupVerifiesTheStart: a started service is confirmed by the backend
// that started it; one confirmed not running fails setup, with the definition
// and its records left for --remove. --no-start touches no backend. A wait the
// check cannot use is refused before anything is written (MADR 0207 D42).
func TestSetupVerifiesTheStart(t *testing.T) {
	defer OverrideInstallOS("linux")()
	defer OverrideRunSystemctl(func(...string) error { return nil })()
	withRealVerifyStart(t)
	record := withUnitRecord(t, "mcremote", "")
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	opts := func(unitDir string) Options {
		return Options{Product: "mcremote", Binary: bin, UnitDir: unitDir, ConfigPath: filepath.Join(dir, "c.yaml"), NoLinger: true}
	}

	f := &setupFake{}
	withSetupBackend(t, f)
	res, err := Setup(opts(filepath.Join(dir, "ok")))
	if err != nil || res.Health != HealthRunning {
		t.Fatalf("a running service: %+v, %v", res, err)
	}
	if want := []string{"stop", "start", "wait"}; !slices.Equal(f.got(), want) {
		t.Fatalf("calls %q, want %q", f.got(), want)
	}

	withSetupBackend(t, &setupFake{waitErr: libservice.ErrUnhealthy})
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

	f = &setupFake{}
	withSetupBackend(t, f)
	o := opts(filepath.Join(dir, "nostart"))
	o.NoStart = true
	if res, err := Setup(o); err != nil || res.Health != "" || len(f.got()) != 0 {
		t.Fatalf("--no-start: %+v, %v, calls %q; want no backend call", res, err, f.got())
	}

	for _, bad := range []string{"abc", "10"} {
		t.Setenv(waitEnv, bad)
		badDir := filepath.Join(dir, "bad"+bad)
		if _, err := Setup(opts(badDir)); err == nil || !strings.Contains(err.Error(), waitEnv) {
			t.Fatalf("a wait of %q: %v, want a refusal naming %s", bad, err, waitEnv)
		}
		if _, err := os.Stat(badDir); !os.IsNotExist(err) {
			t.Fatalf("a refused wait wrote %s: %v", badDir, err)
		}
	}
}
