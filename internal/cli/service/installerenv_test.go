package service

import (
	"os"
	"strings"
	"testing"
)

// TestEnvFlag: "1" is on; unset and "0" are off; anything else is refused,
// naming the variable (MADR 0054 PLAN S5d).
func TestEnvFlag(t *testing.T) {
	const name = EnvForceService
	t.Setenv(name, "") // restored afterwards
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	if on, err := EnvFlag(name); err != nil || on {
		t.Fatalf("unset: %v, %v; want off", on, err)
	}
	for v, want := range map[string]bool{"0": false, "1": true} {
		t.Setenv(name, v)
		if on, err := EnvFlag(name); err != nil || on != want {
			t.Errorf("%q: %v, %v; want %v", v, on, err, want)
		}
	}
	for _, bad := range []string{"", "true", "yes", "2", " 1"} {
		t.Setenv(name, bad)
		if _, err := EnvFlag(name); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%q: %v, want a refusal naming %s", bad, err, name)
		}
	}
}

// TestApplyInstallerEnv: the variables turn the options on, a flag already on
// stays on, and a bad value is refused.
func TestApplyInstallerEnv(t *testing.T) {
	t.Setenv(EnvNoLinger, "1")
	t.Setenv(EnvForceService, "0")
	got, err := ApplyInstallerEnv(Options{})
	if err != nil || !got.NoLinger || got.Force {
		t.Fatalf("NO_LINGER=1 FORCE=0: %+v, %v", got, err)
	}
	t.Setenv(EnvNoLinger, "0")
	if got, err := ApplyInstallerEnv(Options{NoLinger: true}); err != nil || !got.NoLinger {
		t.Fatalf("the --no-linger flag was turned off by NO_LINGER=0: %+v, %v", got, err)
	}
	t.Setenv(EnvForceService, "yes")
	if _, err := ApplyInstallerEnv(Options{}); err == nil || !strings.Contains(err.Error(), EnvForceService) {
		t.Fatalf("FORCE=yes: %v, want a refusal", err)
	}
}
