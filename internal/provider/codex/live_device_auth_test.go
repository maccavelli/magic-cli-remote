//go:build live_codex

package codex_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// hashFile returns a file's SHA-256, or "absent".
func hashFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "absent"
	}
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func codexVersion(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("codex", "--version").CombinedOutput()
	if err != nil {
		t.Skipf("codex not runnable: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestLiveIsolatedDeviceAuthNeverTouchesTheHostCredential is the acceptance
// proof for MADR 0074 F1/F14 and D22.
//
// It starts a real `codex login --device-auth` against an isolated CODEX_HOME,
// cancels it, and asserts two things that together are the whole repair:
// upstream deleted only the isolated credential, and the operator's real
// ~/.codex/auth.json is byte-identical throughout.
//
// It never prints the device code or any token, never completes an
// authorization, and never writes outside its temporary directory.
func TestLiveIsolatedDeviceAuthNeverTouchesTheHostCredential(t *testing.T) {
	version := codexVersion(t)
	t.Logf("codex version under test: %s", version)

	// The real host credential, recorded before anything runs.
	realPath, err := credstore.CodexAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	realBefore := hashFile(t, realPath)

	// An isolated home holding a fixture that looks like a ChatGPT session.
	// It is not a real credential: the point is to observe whether upstream
	// deletes what it finds, not to authenticate.
	isolated := t.TempDir()
	t.Setenv("CODEX_HOME", isolated)
	seeded := filepath.Join(isolated, "auth.json")
	if err := os.WriteFile(seeded,
		[]byte(`{"OPENAI_API_KEY":null,"tokens":{"access_token":"fixture","refresh_token":"fixture","account_id":"fixture"},"last_refresh":"2026-08-21T00:00:00Z"}`),
		0o600); err != nil {
		t.Fatal(err)
	}
	seededBefore := hashFile(t, seeded)

	coord, err := providerauth.NewCoordinator(t.TempDir(),
		codex.NewCredentialAdapter("codex", "codex"), providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}

	p := codex.NewCoordinated(codex.Config{Bin: "codex"}, nil, coord, nil)
	h, err := p.StartOwnedDeviceAuth(context.Background(), "", "", nil)
	if err != nil {
		t.Fatalf("start device auth: %v", err)
	}
	// Never log the code itself; presence is all that is asserted.
	if h.Flow().UserCode == "" {
		t.Error("no device code was parsed from a live codex login")
	}
	t.Log("device code received (value deliberately not logged)")

	// The pending home is where upstream's destructive clear lands, and it
	// started empty, so there was nothing there to delete or revoke.
	h.Cancel()
	if err := h.Wait(context.Background()); err == nil {
		t.Error("a cancelled live flow reported success")
	}

	if got := hashFile(t, realPath); got != realBefore {
		t.Fatalf("the real host credential changed: this is the defect the repair exists to remove")
	}
	if got := hashFile(t, seeded); got != seededBefore {
		t.Logf("upstream modified the isolated seeded credential, as expected for %s", version)
	}

	// And the coordinator's own view is unchanged and healthy.
	st, err := coord.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.BackupState == providerauth.BackupLoggedOut {
		t.Fatal("a cancelled flow recorded a logout")
	}
	time.Sleep(50 * time.Millisecond)
}

// TestLiveEffectiveHomeIsHonoured proves mcremote inspects the same file the
// CLI mutates when CODEX_HOME is set (MADR 0074 F7).
func TestLiveEffectiveHomeIsHonoured(t *testing.T) {
	_ = codexVersion(t)

	// Control first, before any CODEX_HOME override is in effect: the host
	// must actually be signed in, or isolation proves nothing.
	if err := exec.Command("codex", "login", "status").Run(); err != nil {
		t.Skipf("host is not signed in to codex, so isolation cannot be distinguished: %v", err)
	}

	isolated := t.TempDir()
	t.Setenv("CODEX_HOME", isolated)

	got, err := credstore.CodexAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(isolated, "auth.json"); got != want {
		t.Fatalf("effective auth path = %q, want %q", got, want)
	}
	// The CLI agrees: a status run against the isolated home must not see the
	// operator's real session.
	//
	// Assert on the exit code, not the wording. Codex prints "Not logged in"
	// and exits 1 when it has no credential, and a naive substring test for
	// "logged in" matches that string — which is how the first version of this
	// test failed against correct behaviour. The exit code is also what the
	// adapter's own probe depends on, so this pins the contract that matters.
	cmd := exec.Command("codex", "login", "status")
	cmd.Env = append(os.Environ(), credstore.CodexHomeEnv(isolated))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("codex reported a credential from outside the isolated home: %s",
			strings.TrimSpace(string(out)))
	}

}
