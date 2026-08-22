package goose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// gooseHome points the credstore helpers at an isolated goose config dir and
// returns it. GOOSE_DISABLE_KEYRING is unset, not blanked: a set-but-empty
// variable disables the keyring under goose's presence-only env rule.
func keyringHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GOOSE_DISABLE_KEYRING", "")
	os.Unsetenv("GOOSE_DISABLE_KEYRING")
	dir := filepath.Join(home, ".config", "goose")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeGooseConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeGooseSecrets(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "secrets.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEffectiveKeyringDisabledTable(t *testing.T) {
	cases := []struct {
		name             string
		cfgValue         bool
		wantDisabled     bool
		wantHostControls bool
	}{
		{"config true", true, true, false},
		{"config false", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyringHome(t)
			disabled, host := EffectiveKeyringDisabled(tc.cfgValue)
			if disabled != tc.wantDisabled || host != tc.wantHostControls {
				t.Fatalf("got (%v,%v), want (%v,%v)", disabled, host, tc.wantDisabled, tc.wantHostControls)
			}
		})
	}
}

// TestEffectiveKeyringHostEnvAlwaysWins covers MADR 0110 D4. Every value,
// including "0" and "", means the host is in control: goose's env branch is
// presence-only, so mcremote cannot express "enabled" through it and must not
// pretend the value means anything.
func TestEffectiveKeyringHostEnvAlwaysWins(t *testing.T) {
	for _, v := range []string{"1", "true", "0", "false", "", "maybe"} {
		for _, cfgValue := range []bool{true, false} {
			keyringHome(t)
			t.Setenv("GOOSE_DISABLE_KEYRING", v)
			disabled, host := EffectiveKeyringDisabled(cfgValue)
			if !host {
				t.Fatalf("env=%q cfg=%v: hostControls=false, want true", v, cfgValue)
			}
			if !disabled {
				t.Fatalf("env=%q: disabled=false, but presence alone disables", v)
			}
		}
	}
}

// TestGuardHoldsWhenSecretsElsewhere is the safety property behind defaulting
// to true: a host whose secrets live only in the keyring must not be switched
// to an empty file store.
func TestGuardHoldsWhenSecretsElsewhere(t *testing.T) {
	dir := keyringHome(t)
	writeGooseConfig(t, dir, "active_provider: openrouter\nproviders:\n  openrouter:\n    model: x\n")
	// No secrets.yaml at all.

	res, err := Reconcile(true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeHold {
		t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeHold)
	}
	if !strings.Contains(res.Reason, "GOOSE_DISABLE_KEYRING=1 goose configure") {
		t.Fatalf("reason does not name the remediation: %q", res.Reason)
	}
	if credstore.GooseKeyringDisabled(filepath.Join(dir, "config.yaml")) {
		t.Fatal("the guard wrote the key anyway")
	}
}

// TestGuardSwitchesOnColdHost proves a host with nothing configured is safe to
// switch: there are no credentials to lose.
func TestGuardSwitchesOnColdHost(t *testing.T) {
	dir := keyringHome(t)
	writeGooseConfig(t, dir, "# nothing configured yet\n")

	res, err := Reconcile(true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeSwitch {
		t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeSwitch)
	}
	if !credstore.GooseKeyringDisabled(filepath.Join(dir, "config.yaml")) {
		t.Fatal("key was not written")
	}
}

func TestGuardSwitchesWhenFileStorePopulated(t *testing.T) {
	dir := keyringHome(t)
	writeGooseConfig(t, dir, "active_provider: openrouter\nproviders:\n  openrouter:\n    model: x\n")
	writeGooseSecrets(t, dir, "OPENROUTER_API_KEY: sk-or-test\n")

	res, err := Reconcile(true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeSwitch {
		t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeSwitch)
	}
}

// TestGuardNeverLogsASecret proves the guard reads names, not values.
func TestGuardNeverLogsASecret(t *testing.T) {
	const sentinel = "sk-SENTINELsecretVALUE"
	dir := keyringHome(t)
	writeGooseConfig(t, dir, "active_provider: openrouter\nproviders:\n  openrouter:\n    model: x\n")
	writeGooseSecrets(t, dir, "OPENROUTER_API_KEY: "+sentinel+"\n")

	res, err := Reconcile(true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Reason, sentinel) {
		t.Fatalf("reason leaked the secret: %q", res.Reason)
	}
	if strings.Contains(string(res.Outcome), sentinel) {
		t.Fatal("outcome leaked the secret")
	}
}

// TestGuardRunsNoSubprocessAndTouchesNoKeyring is the property that keeps this
// work from causing the very prompt it removes. A PATH with nothing on it
// makes any exec attempt fail loudly rather than silently succeeding.
func TestGuardRunsNoSubprocessAndTouchesNoKeyring(t *testing.T) {
	dir := keyringHome(t)
	writeGooseConfig(t, dir, "active_provider: openrouter\nproviders:\n  openrouter:\n    model: x\n")
	writeGooseSecrets(t, dir, "OPENROUTER_API_KEY: sk-x\n")
	t.Setenv("PATH", "")

	if _, err := Reconcile(true); err != nil {
		t.Fatalf("guard needed something from PATH: %v", err)
	}
}

// TestReconcileOutcomes walks one case per GuardOutcome.
func TestReconcileOutcomes(t *testing.T) {
	t.Run(string(OutcomeHostControls), func(t *testing.T) {
		dir := keyringHome(t)
		writeGooseConfig(t, dir, "active_provider: x\n")
		t.Setenv("GOOSE_DISABLE_KEYRING", "1")

		res, err := Reconcile(true)
		if err != nil {
			t.Fatal(err)
		}
		if res.Outcome != OutcomeHostControls {
			t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeHostControls)
		}
		got, readErr := os.ReadFile(filepath.Join(dir, "config.yaml"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(got), "GOOSE_DISABLE_KEYRING") {
			t.Fatal("config was written while the host was in control")
		}
	})

	t.Run(string(OutcomeNoChange), func(t *testing.T) {
		dir := keyringHome(t)
		writeGooseConfig(t, dir, "# cold\n")
		if _, err := Reconcile(true); err != nil {
			t.Fatal(err)
		}
		res, err := Reconcile(true) // second run
		if err != nil {
			t.Fatal(err)
		}
		if res.Outcome != OutcomeNoChange {
			t.Fatalf("outcome = %s, want %s on a second run", res.Outcome, OutcomeNoChange)
		}
	})

	t.Run(string(OutcomeOperatorOwned), func(t *testing.T) {
		dir := keyringHome(t)
		writeGooseConfig(t, dir, "GOOSE_DISABLE_KEYRING: true\nactive_provider: x\n")

		res, err := Reconcile(false)
		if err != nil {
			t.Fatal(err)
		}
		if res.Outcome != OutcomeOperatorOwned {
			t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeOperatorOwned)
		}
		got, readErr := os.ReadFile(filepath.Join(dir, "config.yaml"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.Contains(string(got), "GOOSE_DISABLE_KEYRING: true") {
			t.Fatal("a hand-set line was removed")
		}
	})

	t.Run("disable false removes our line", func(t *testing.T) {
		dir := keyringHome(t)
		// A cold config, so the initial Reconcile(true) actually switches
		// rather than holding: with a configured provider and no secrets.yaml
		// the guard would correctly refuse to write anything.
		writeGooseConfig(t, dir, "# cold\n")
		if _, err := Reconcile(true); err != nil {
			t.Fatal(err)
		}
		res, err := Reconcile(false)
		if err != nil {
			t.Fatal(err)
		}
		if res.Outcome != OutcomeSwitch {
			t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeSwitch)
		}
		got, readErr := os.ReadFile(filepath.Join(dir, "config.yaml"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != "# cold\n" {
			t.Fatalf("file not restored byte-identically: %q", got)
		}
	})
}
