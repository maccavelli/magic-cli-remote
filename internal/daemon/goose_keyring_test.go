package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/provider/goose"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// gooseTestHome isolates goose's config dir. GOOSE_DISABLE_KEYRING is unset
// rather than blanked, because a set-but-empty variable disables the keyring
// under goose's presence-only environment rule.
func gooseTestHome(t *testing.T) string {
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

// TestReconcileGooseKeyringSwitches proves the daemon's helper writes the key
// on a host it is safe to switch.
func TestReconcileGooseKeyringSwitches(t *testing.T) {
	dir := gooseTestHome(t)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("# cold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logged := reconcileGooseKeyring(true, quietLog())

	if logged.Outcome != goose.OutcomeSwitch {
		t.Fatalf("outcome = %s, want switch", logged.Outcome)
	}
	if !credstore.GooseKeyringDisabled(filepath.Join(dir, "config.yaml")) {
		t.Fatal("key was not written")
	}
}

// TestReconcileGooseKeyringHoldsAndExplains is the case on the machine that
// motivated MADR 0110: secrets live in the keyring, so switching would strand
// them. The daemon must not write, and must say what to do.
func TestReconcileGooseKeyringHoldsAndExplains(t *testing.T) {
	dir := gooseTestHome(t)
	body := "active_provider: openrouter\nproviders:\n  openrouter:\n    model: x\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	res := reconcileGooseKeyring(true, quietLog())
	if res.Outcome != goose.OutcomeHold {
		t.Fatalf("outcome = %s, want hold", res.Outcome)
	}
	if !strings.Contains(res.Reason, "GOOSE_DISABLE_KEYRING=1 goose configure") {
		t.Fatalf("reason does not name the remediation: %q", res.Reason)
	}
	got, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatal("the config was modified on a hold")
	}
}

// TestReconcileGooseKeyringHostControls proves the daemon defers to a host that
// set the variable itself, and writes nothing.
func TestReconcileGooseKeyringHostControls(t *testing.T) {
	dir := gooseTestHome(t)
	body := "active_provider: x\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOSE_DISABLE_KEYRING", "1")

	res := reconcileGooseKeyring(true, quietLog())
	if res.Outcome != goose.OutcomeHostControls {
		t.Fatalf("outcome = %s, want host_controls", res.Outcome)
	}
	got, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatal("config was written while the host was in control")
	}
}

// TestReconcileGooseKeyringErrorIsNotFatal proves a failure to write a
// preference cannot take the provider down.
func TestReconcileGooseKeyringErrorIsNotFatal(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	dir := gooseTestHome(t)
	// Make the config path a directory so any write fails.
	if err := os.MkdirAll(filepath.Join(dir, "config.yaml"), 0o700); err != nil {
		t.Fatal(err)
	}
	res := reconcileGooseKeyring(true, quietLog())
	// Whatever it decides, it must return rather than panic or block.
	if res.Outcome == "" && res.Reason == "" {
		t.Log("reconciliation reported nothing actionable, which is acceptable here")
	}
}
