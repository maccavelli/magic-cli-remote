package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// runAuthRecovery executes the subcommand tree against a temporary data dir and
// returns stdout plus the resolved exit code.
func runAuthRecovery(t *testing.T, dataDir string, args ...string) (string, int) {
	t.Helper()
	return runAuthRecoveryWith(t, dataDir, "", args...)
}

// runAuthRecoveryWith is runAuthRecovery with a providers section: providers
// is YAML at that level, and empty means none. The config is the test's own,
// never the host's (0208 PLAN P8 Deviation 3).
func runAuthRecoveryWith(t *testing.T, dataDir, providers string, args ...string) (string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := ""
	if providers != "" {
		body = "providers:\n" + providers
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCREMOTE_CONFIG", path)
	root := &cobra.Command{Use: "mcremote", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().String("config", "", "")
	root.PersistentFlags().String("data-dir", "", "")
	root.AddCommand(newAuthRecoveryCmd())

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"auth-recovery", "--data-dir", dataDir}, args...))

	err := root.ExecuteContext(context.Background())
	return out.String(), ExitCode(err)
}

func codexCoordinator(t *testing.T, dataDir string) (*providerauth.Coordinator, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	live := filepath.Join(home, "auth.json")
	if err := os.WriteFile(live, []byte(`{"tokens":{"access_token":"a","refresh_token":"r"},"last_refresh":"2026-08-20T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := providerauth.NewCoordinator(dataDir, codex.NewCredentialAdapter("codex"), providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c, live
}

// TestAuthRecoveryStatusPrintsPublicStateOnly proves the operator surface never
// prints a path, fingerprint, generation id, or credential (MADR 0074 D24/D29).
func TestAuthRecoveryStatusPrintsPublicStateOnly(t *testing.T) {
	dataDir := t.TempDir()
	const secret = "SENTINELtokenVALUE"
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	live := filepath.Join(home, "auth.json")
	if err := os.WriteFile(live, []byte(`{"tokens":{"access_token":"`+secret+`","refresh_token":"`+secret+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := providerauth.NewCoordinator(dataDir, codex.NewCredentialAdapter("codex"), providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}

	out, code := runAuthRecovery(t, dataDir, "status")
	if code != 0 {
		t.Fatalf("exit = %d, want 0: %s", code, out)
	}
	if !strings.Contains(out, "codex") || !strings.Contains(out, "grok") {
		t.Fatalf("status did not report every provider: %s", out)
	}
	for _, banned := range []string{secret, home, live, ".auth", "provider-auth"} {
		if strings.Contains(out, banned) {
			t.Fatalf("status leaked %q: %s", banned, out)
		}
	}
}

// TestAuthRecoveryReachesADisabledProvider is 0208 PLAN P8 Deviation 4: the
// adapters are serve's, from daemon.CredentialAdapters, but a provider disabled
// since its credential was stored can still be inspected and recovered.
func TestAuthRecoveryReachesADisabledProvider(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	out, code := runAuthRecoveryWith(t, t.TempDir(), "  codex:\n    enabled: false\n  grok:\n    enabled: false\n", "status")
	if code != 0 {
		t.Fatalf("exit = %d, want 0: %s", code, out)
	}
	if !strings.Contains(out, "codex") || !strings.Contains(out, "grok") {
		t.Fatalf("status with both disabled = %q, want both listed", out)
	}
}

func TestAuthRecoveryStatusUnknownProviderIsUsage(t *testing.T) {
	dataDir := t.TempDir()
	out, code := runAuthRecovery(t, dataDir, "status", "mystery")
	if code != authRecoveryExitUsage {
		t.Fatalf("exit = %d, want %d: %s", code, authRecoveryExitUsage, out)
	}
}

func TestAuthRecoveryChooseUnknownChoiceIsUsage(t *testing.T) {
	dataDir := t.TempDir()
	out, code := runAuthRecovery(t, dataDir, "choose", "codex", "mystery")
	if code != authRecoveryExitUsage {
		t.Fatalf("exit = %d, want %d: %s", code, authRecoveryExitUsage, out)
	}
}

// TestAuthRecoveryChooseRefusesAHealthyProvider proves a resolution cannot be
// applied to a manifest that is not awaiting a decision.
func TestAuthRecoveryChooseRefusesAHealthyProvider(t *testing.T) {
	dataDir := t.TempDir()
	codexCoordinator(t, dataDir)

	out, code := runAuthRecovery(t, dataDir, "choose", "codex", "current")
	if code != authRecoveryExitFailed {
		t.Fatalf("exit = %d, want %d: %s", code, authRecoveryExitFailed, out)
	}
}

// TestAuthRecoveryChooseResolvesAmbiguity walks the real operator path: force
// the ambiguous state, resolve it, and prove the state clears.
func TestAuthRecoveryChooseResolvesAmbiguity(t *testing.T) {
	dataDir := t.TempDir()
	c, live := codexCoordinator(t, dataDir)
	ctx := context.Background()

	// An unrelated valid credential appears: not fresher, so it escalates.
	if err := os.WriteFile(live, []byte(`{"OPENAI_API_KEY":"sk-other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := c.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st != providerauth.StateRecoveryRequired {
		t.Fatalf("state = %s, want recovery_required", st)
	}

	out, code := runAuthRecovery(t, dataDir, "status", "codex")
	if code != 0 || !strings.Contains(out, "recovery_required") {
		t.Fatalf("status = %q exit = %d", out, code)
	}

	out, code = runAuthRecovery(t, dataDir, "choose", "codex", "current")
	if code != 0 {
		t.Fatalf("choose exit = %d: %s", code, out)
	}
	if !strings.Contains(out, "codex resolved") {
		t.Fatalf("choose output = %q", out)
	}

	// The retained CURRENT is back on disk and the state is healthy again.
	got, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"refresh_token":"r"`) {
		t.Fatalf("live = %s, want CURRENT republished", got)
	}
	status, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.BackupState != providerauth.BackupCurrent {
		t.Fatalf("backup state = %s, want current", status.BackupState)
	}
}

// TestAuthRecoveryIsRegistered proves P20 activated the operator surface. It
// was deliberately absent through P19, because a recovery command is only
// useful once the daemon actually owns coordinators and watchers (P20 step 13).
func TestAuthRecoveryIsRegistered(t *testing.T) {
	root := newRootCmd()
	for _, c := range root.Commands() {
		if c.Name() == "auth-recovery" {
			return
		}
	}
	t.Fatal("auth-recovery is not registered on the root command")
}
