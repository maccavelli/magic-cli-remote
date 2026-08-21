package codex

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// fakeCodexLogout writes a stand-in codex whose `logout` records the order of
// events, so a test can prove the tombstone was durable before the revoking
// invocation ran.
func fakeCodexLogout(t *testing.T, journal string, exitCode int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
if [ "$1" = "logout" ]; then
  echo "logout:$CODEX_HOME" >> "` + journal + `"
  rm -f "$CODEX_HOME/auth.json"
  exit ` + strconv.Itoa(exitCode) + `
fi
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func logoutFixture(t *testing.T, bin, cred string) (*Provider, *providerauth.Coordinator, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	live := filepath.Join(home, "auth.json")
	if err := os.WriteFile(live, []byte(cred), 0o600); err != nil {
		t.Fatal(err)
	}
	coord, err := providerauth.NewCoordinator(t.TempDir(), NewCredentialAdapter("codex", bin), providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewCoordinated(Config{Bin: bin}, nil, coord, nil), coord, live
}

// TestChatGPTLogoutTombstonesBeforeRevoking is the §17.5 gate. `codex logout`
// revokes the refresh token server-side, so the durable tombstone must be
// written first; otherwise a crash between the two leaves a revoked credential
// that every fingerprint check still calls valid.
func TestChatGPTLogoutTombstonesBeforeRevoking(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "order.log")
	bin := fakeCodexLogout(t, journal, 0)
	p, coord, live := logoutFixture(t, bin, `{"tokens":{"access_token":"a","refresh_token":"r"}}`)

	if err := p.CoordinatedLogout(context.Background()); err != nil {
		t.Fatal(err)
	}

	// The live credential is gone and the manifest says logged out.
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Error("logout left the live credential in place")
	}
	st, err := coord.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.BackupState != providerauth.BackupLoggedOut {
		t.Errorf("backup state = %s, want logged_out", st.BackupState)
	}
	if st.RecoveryAvailable {
		t.Error("a revoked credential must never be offered for recovery")
	}

	// The revoking invocation ran against the real home, not a clone: a clone
	// would have carried the same refresh token and revoked it anyway.
	b, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), os.Getenv("CODEX_HOME")) {
		t.Fatalf("logout journal = %q, want the effective codex home", b)
	}
	if strings.Contains(string(b), "mcremote-codex-logout-") {
		t.Fatal("a ChatGPT credential was rehearsed on a clone, which revokes it")
	}
}

// TestChatGPTLogoutReportsAFailedRevoke proves a revoke that does not confirm
// is surfaced rather than swallowed — upstream only warns on revoke failure,
// so exit zero is not proof and neither is silence.
func TestChatGPTLogoutReportsAFailedRevoke(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "order.log")
	bin := fakeCodexLogout(t, journal, 9)
	p, coord, _ := logoutFixture(t, bin, `{"tokens":{"access_token":"a","refresh_token":"r"}}`)

	err := p.CoordinatedLogout(context.Background())
	if err == nil {
		t.Fatal("a failed revoke was reported as a clean logout")
	}
	// The local removal still happened and is still recorded, because the
	// credential must not come back looking valid.
	st, statusErr := coord.Status(context.Background())
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if st.BackupState != providerauth.BackupLoggedOut {
		t.Errorf("backup state = %s, want logged_out even when revoke failed", st.BackupState)
	}
}

// TestAPIKeyLogoutUsesTheCloneProbe proves the non-revoking path is retained:
// an API key is never revoked by Codex, so rehearsing on a copy is safe and
// gives a real precondition before touching LIVE.
func TestAPIKeyLogoutUsesTheCloneProbe(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "order.log")
	bin := fakeCodexLogout(t, journal, 0)
	p, _, live := logoutFixture(t, bin, `{"OPENAI_API_KEY":"sk-test"}`)

	if err := p.CoordinatedLogout(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "mcremote-codex-logout-") {
		t.Fatalf("journal = %q, want the clone probe for a non-revocable credential", b)
	}
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Error("logout left the live credential in place")
	}
	if strings.Contains(string(b), "sk-test") {
		t.Fatal("the journal captured the key")
	}
}

// TestAPIKeyLogoutProbeFailureLeavesLiveUntouched proves the clone probe is a
// real precondition: a failed rehearsal changes neither LIVE nor the manifest.
func TestAPIKeyLogoutProbeFailureLeavesLiveUntouched(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "order.log")
	bin := fakeCodexLogout(t, journal, 4)
	p, coord, live := logoutFixture(t, bin, `{"OPENAI_API_KEY":"sk-test"}`)
	before, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.CoordinatedLogout(context.Background()); err == nil {
		t.Fatal("a failed probe reported success")
	}
	after, err := os.ReadFile(live)
	if err != nil {
		t.Fatalf("a failed probe removed the live credential: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("a failed probe changed the live credential")
	}
	st, err := coord.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.BackupState == providerauth.BackupLoggedOut {
		t.Error("a failed probe recorded a logout tombstone")
	}
}

// TestUnknownModeTakesTheRevokingPath proves the safe default: assuming a
// credential can be rehearsed is the mistake that destroys a grant.
func TestUnknownModeTakesTheRevokingPath(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "order.log")
	bin := fakeCodexLogout(t, journal, 0)
	// Seed a parseable credential, then replace LIVE with something the
	// adapter cannot classify.
	p, _, live := logoutFixture(t, bin, `{"tokens":{"access_token":"a","refresh_token":"r"}}`)
	if err := os.WriteFile(live, []byte(`{"unrecognized":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_ = p.CoordinatedLogout(context.Background())
	b, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "mcremote-codex-logout-") {
		t.Fatal("an unclassifiable credential was rehearsed on a clone")
	}
}

// TestCoordinatedLogoutNeedsACoordinator keeps the dark-by-default property.
func TestCoordinatedLogoutNeedsACoordinator(t *testing.T) {
	p := New(Config{Bin: "codex"})
	if err := p.CoordinatedLogout(context.Background()); err == nil {
		t.Fatal("an uncoordinated provider performed a transactional logout")
	}
}
