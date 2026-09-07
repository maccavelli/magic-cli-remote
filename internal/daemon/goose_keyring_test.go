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
	// Assert the setup took effect. A helper whose effect is assumed is how an
	// environment failure surfaces as an outcome failure: CI run 33871538458
	// reported `outcome = ` with no way to tell whether the reconciliation had
	// misbehaved or the environment had never been what the test required
	// (MADR 0139).
	got, err := credstore.GooseConfigPath()
	if err != nil {
		t.Fatalf("precondition: goose config path does not resolve: %v", err)
	}
	if want := filepath.Join(dir, "config.yaml"); got != want {
		t.Fatalf("precondition: goose config resolves to %s, want %s — the isolated environment is not in effect", got, want)
	}
	return dir
}

// requireWritableDir skips t unless dir is genuinely not writable, by trying to
// write rather than by inspecting the mode or the uid.
//
// root ignores the permission bits, and CI containers commonly run as root, so
// `os.Geteuid() == 0` would be the usual guard — but that asserts a proxy for
// the property. Probing asserts the property.
func requireUnwritableDir(t *testing.T, dir string) {
	t.Helper()
	probe := filepath.Join(dir, ".mcremote-write-probe")
	f, err := os.Create(probe) //nolint:gosec // test-owned temp path
	if err == nil {
		_ = f.Close()
		_ = os.Remove(probe)
		t.Skip("this user can write the directory despite its mode (root ignores permission bits); " +
			"the unwritable-directory state cannot be induced here")
	}
}

// TestReconcileGooseKeyringSwitches proves the daemon's helper writes the key
// on a host it is safe to switch.
func TestReconcileGooseKeyringSwitches(t *testing.T) {
	dir := gooseTestHome(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("# cold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The reconciliation writes; a directory it cannot write into is an
	// environment failure, and this test must say so rather than report it as
	// an unexpected outcome (MADR 0139).
	probe := filepath.Join(dir, ".mcremote-write-probe")
	f, err := os.Create(probe) //nolint:gosec // test-owned temp path
	if err != nil {
		t.Fatalf("precondition: cannot write in %s: %v", dir, err)
	}
	_ = f.Close()
	_ = os.Remove(probe)

	logged := reconcileGooseKeyring(true, quietLog())

	if logged.Outcome != goose.OutcomeSwitch {
		t.Fatalf("outcome = %q reason = %q (config %s), want switch",
			logged.Outcome, logged.Reason, cfgPath)
	}
	if !credstore.GooseKeyringDisabled(cfgPath) {
		t.Fatalf("key was not written to %s", cfgPath)
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
// preference cannot take the provider down — and that it says what failed.
//
// This test used to assert nothing: it logged "reconciliation reported nothing
// actionable, which is acceptable here" and passed either way. That is how the
// empty outcome became normalised, and why CI run 33871538458 had no vocabulary
// to describe itself (MADR 0139).
//
// The induction changed too. Replacing config.yaml with a directory produces
// `hold`, not an error, because fileStoreCanServe reads an unreadable config as
// "assume something is configured". An unwritable parent directory is what
// actually reaches the error path.
func TestReconcileGooseKeyringErrorIsNotFatal(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	dir := gooseTestHome(t)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("# cold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	requireUnwritableDir(t, dir)

	res := reconcileGooseKeyring(true, quietLog())

	// It must return rather than panic or block — and it must name the failure.
	if res.Outcome != goose.OutcomeError {
		t.Fatalf("outcome = %q, want %q: a failure must be a named state, not the zero value",
			res.Outcome, goose.OutcomeError)
	}
	if strings.TrimSpace(res.Reason) == "" {
		t.Fatal("outcome is error with no reason; the cause is the only thing this state carries")
	}
	if !strings.Contains(res.Reason, dir) {
		t.Fatalf("reason = %q, want it to name the directory it could not write (%s)", res.Reason, dir)
	}
}

// TestReconcileReportsAMissingHomeDirectory is the first of the two states that
// produced an empty outcome before MADR 0139: no home directory at all.
func TestReconcileReportsAMissingHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	os.Unsetenv("HOME")
	os.Unsetenv("XDG_CONFIG_HOME")
	// On Windows os.UserHomeDir reads USERPROFILE, so unsetting HOME does not
	// break home resolution. Skip on the behaviour, not on the platform name.
	if _, err := credstore.GooseConfigPath(); err == nil {
		t.Skip("home resolution still succeeds without HOME on this platform; " +
			"the missing-home state cannot be induced here")
	}

	res, err := goose.Reconcile(true)

	if err == nil {
		t.Fatal("want an error when there is no home directory")
	}
	if res.Outcome != goose.OutcomeError {
		t.Fatalf("outcome = %q, want %q", res.Outcome, goose.OutcomeError)
	}
	if !strings.Contains(res.Reason, "HOME") {
		t.Fatalf("reason = %q, want it to name HOME — an operator seeing this needs to know which of the two states it is",
			res.Reason)
	}
}

// TestReconcileReportsAnUnwritableConfigDirectory is the second state.
func TestReconcileReportsAnUnwritableConfigDirectory(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	dir := gooseTestHome(t)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("# cold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	requireUnwritableDir(t, dir)

	res, err := goose.Reconcile(true)

	if err == nil {
		t.Fatal("want an error when the config directory cannot be written")
	}
	if res.Outcome != goose.OutcomeError {
		t.Fatalf("outcome = %q, want %q", res.Outcome, goose.OutcomeError)
	}
	if !strings.Contains(res.Reason, dir) {
		t.Fatalf("reason = %q, want it to name the path (%s)", res.Reason, dir)
	}
	if strings.Contains(res.Reason, "HOME") {
		t.Fatalf("reason = %q names HOME; the two failure states must stay distinguishable", res.Reason)
	}
}

// TestReconcileNeverReturnsAnEmptyOutcome is the invariant MADR 0139 rests on.
//
// Every reachable state is driven, including the two that used to yield the
// zero value. After this, an empty Outcome anywhere means an uninitialised
// Result and nothing else.
func TestReconcileNeverReturnsAnEmptyOutcome(t *testing.T) {
	t.Run("cold host switches", func(t *testing.T) {
		dir := gooseTestHome(t)
		writeGooseConfig(t, dir, "# cold\n")
		res, err := goose.Reconcile(true)
		assertNamedOutcome(t, res, err)
	})
	t.Run("configured host holds", func(t *testing.T) {
		dir := gooseTestHome(t)
		writeGooseConfig(t, dir, "active_provider: openrouter\nproviders:\n  openrouter:\n    model: x\n")
		res, err := goose.Reconcile(true)
		assertNamedOutcome(t, res, err)
	})
	t.Run("host controls", func(t *testing.T) {
		dir := gooseTestHome(t)
		writeGooseConfig(t, dir, "# cold\n")
		t.Setenv("GOOSE_DISABLE_KEYRING", "1")
		res, err := goose.Reconcile(true)
		assertNamedOutcome(t, res, err)
	})
	t.Run("already reconciled", func(t *testing.T) {
		dir := gooseTestHome(t)
		writeGooseConfig(t, dir, "# cold\n")
		if _, err := goose.Reconcile(true); err != nil {
			t.Fatal(err)
		}
		// Second pass: nothing left to change.
		res, err := goose.Reconcile(true)
		assertNamedOutcome(t, res, err)
	})
	t.Run("no home directory", func(t *testing.T) {
		t.Setenv("HOME", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		os.Unsetenv("HOME")
		os.Unsetenv("XDG_CONFIG_HOME")
		if _, err := credstore.GooseConfigPath(); err == nil {
			t.Skip("home resolution still succeeds without HOME on this platform")
		}
		res, err := goose.Reconcile(true)
		assertNamedOutcome(t, res, err)
	})
	t.Run("unwritable config directory", func(t *testing.T) {
		testexec.SkipIfNoPOSIXModes(t)
		dir := gooseTestHome(t)
		writeGooseConfig(t, dir, "# cold\n")
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		requireUnwritableDir(t, dir)
		res, err := goose.Reconcile(true)
		assertNamedOutcome(t, res, err)
	})
}

func writeGooseConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertNamedOutcome(t *testing.T, res goose.Result, err error) {
	t.Helper()
	if res.Outcome == "" {
		t.Fatalf("outcome is empty (err = %v); every state must be named, or a failure "+
			"prints `outcome = ` and says nothing — MADR 0139", err)
	}
}
