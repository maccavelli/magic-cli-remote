//go:build unix

package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// relayHost is a Linux systemd host with an mcremote binary, and optionally
// an mcrelay beside it. It returns the folder and the unit directory.
func relayHost(t *testing.T, withRelay bool) (string, string) {
	t.Helper()
	t.Cleanup(OverrideInstallOS("linux"))
	t.Cleanup(OverrideRunSystemctl(func(...string) error { return nil }))
	dir := t.TempDir()
	// Setup checks only the executable bit; the stubs are never run here.
	testexec.WriteShellStub(t, filepath.Join(dir, "mcremote"), "")
	if withRelay {
		testexec.WriteShellStub(t, filepath.Join(dir, "mcrelay"), "")
	}
	return dir, filepath.Join(t.TempDir(), "units")
}

// TestSetupRelayBeside: mcrelay is set up from the binary beside mcremote's,
// with mcremote's start and linger choices (MADR 0054 amendment S5-1).
func TestSetupRelayBeside(t *testing.T) {
	dir, units := relayHost(t, true)
	remote := Result{Binary: filepath.Join(dir, "mcremote")}
	res, err := SetupRelayBeside(remote, Options{UnitDir: units, NoLinger: true, NoStart: true}, HealthOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Binary != filepath.Join(dir, "mcrelay") || res.UnitPath != filepath.Join(units, "mcrelay.service") {
		t.Fatalf("relay result %+v, want mcrelay.service running %s", res, filepath.Join(dir, "mcrelay"))
	}
	if res.Started || res.LingerEnabled {
		t.Fatalf("relay result %+v: mcremote's --no-start and --no-linger were not carried over", res)
	}
}

func TestSetupRelayBesideWithNoRelayBinary(t *testing.T) {
	dir, units := relayHost(t, false)
	_, err := SetupRelayBeside(Result{Binary: filepath.Join(dir, "mcremote")}, Options{UnitDir: units, NoStart: true}, HealthOptions{})
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "mcrelay")) || !strings.Contains(err.Error(), EnvWithRelayService) {
		t.Fatalf("no mcrelay beside mcremote: %v, want an error naming the path and the variable", err)
	}
}

// TestSetupRelayBesideWhenTheRelayFails: a relay that does not start leaves
// mcremote alone, and the error says mcremote is set up.
func TestSetupRelayBesideWhenTheRelayFails(t *testing.T) {
	dir, units := relayHost(t, true)
	prev := verifyStart
	verifyStart = func(_ Kind, res Result) (Result, error) { return res, errors.New("mcrelay is not running after setup") }
	t.Cleanup(func() { verifyStart = prev })
	_, err := SetupRelayBeside(Result{Binary: filepath.Join(dir, "mcremote")}, Options{UnitDir: units}, HealthOptions{})
	if err == nil || !strings.Contains(err.Error(), "mcremote is set up; mcrelay is not") {
		t.Fatalf("a relay that does not start: %v, want it to say mcremote is set up", err)
	}
}

// TestSetupRelayBesideConfirmsWithTheRelaysProbe is MADR 0207 D40: the relay
// set up beside mcremote is built with the relay's health wiring, not
// mcremote's, so its start is confirmed with its own /healthz.
func TestSetupRelayBesideConfirmsWithTheRelaysProbe(t *testing.T) {
	dir, units := relayHost(t, true)
	builds := withSetupBackend(t, &setupFake{})
	probed := false
	relay := HealthOptions{Probe: func(context.Context, ServeArgs) (libservice.Health, error) {
		probed = true
		return libservice.Health{Ready: true}, nil
	}}
	remote := Options{UnitDir: units, NoLinger: true, Health: HealthOptions{Probe: func(context.Context, ServeArgs) (libservice.Health, error) {
		t.Error("mcremote's probe was given to the relay")
		return libservice.Health{}, nil
	}}}
	if _, err := SetupRelayBeside(Result{Binary: filepath.Join(dir, "mcremote")}, remote, relay); err != nil {
		t.Fatal(err)
	}
	if len(*builds) == 0 {
		t.Fatal("the relay was started with no backend")
	}
	for _, b := range *builds {
		if b.Product != "mcrelay" || b.Health.Probe == nil {
			t.Fatalf("a backend built for %q with probe %v; want mcrelay's", b.Product, b.Health.Probe)
		}
		if _, err := b.Health.Probe(context.Background(), ServeArgs{}); err != nil || !probed {
			t.Fatalf("the relay's backend probe is not the relay's: %v, probed %v", err, probed)
		}
	}
}
