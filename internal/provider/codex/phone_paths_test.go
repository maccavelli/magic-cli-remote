package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// The phone's codex paths and the credential coordinator (MADR 0074 §16.2,
// D38, Q17 re-asked, Q-D; PLAN P23 steps 1 and 7). The phone's clear reaches
// codex through ClearCredentialMethod and its key write through SetCredential;
// both must leave the coordinator in a state the next daemon start accepts.

const revokeToken = "r-SENTINEL-refresh-0123456789"

// chatGPTCred is a ChatGPT credential whose refresh token is revokeToken.
const chatGPTCred = `{"tokens":{"access_token":"a","refresh_token":"` + revokeToken + `"}}`

// fakeCodexPhone is a stand-in codex for the phone's two paths: `logout`
// removes auth.json from its home, `login --with-api-key` writes the key read
// from stdin into it, and `login status` succeeds when a credential is
// present. Every call is journalled with the home it ran in. It never prints
// the key.
func fakeCodexPhone(t *testing.T, journal string) string {
	t.Helper()
	testexec.SkipIfNoPOSIXShell(t)
	path := filepath.Join(t.TempDir(), "codex")
	script := `echo "argv:$* home:$CODEX_HOME" >> "` + journal + `"
if [ "$1" = "logout" ]; then
  rm -f "$CODEX_HOME/auth.json"
  exit 0
fi
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  [ -f "$CODEX_HOME/auth.json" ] || exit 1
  exit 0
fi
if [ "$1" = "login" ] && [ "$2" = "--with-api-key" ]; then
  read -r key
  printf '{"OPENAI_API_KEY":"%s","tokens":null}' "$key" > "$CODEX_HOME/auth.json"
  chmod 600 "$CODEX_HOME/auth.json"
  exit 0
fi
exit 0
`
	testexec.WriteShellStub(t, path, script)
	return path
}

// fakeCodexRevoke is a stand-in codex whose `logout` behaves as the real one
// does: it can revoke only if the home it runs in holds the refresh token. It
// exits 1 otherwise, and journals the home and whether the daemon's own LIVE
// credential was already gone, which is what proves the tombstone came first.
func fakeCodexRevoke(t *testing.T, journal, live string) string {
	t.Helper()
	testexec.SkipIfNoPOSIXShell(t)
	path := filepath.Join(t.TempDir(), "codex")
	script := `if [ "$1" = "logout" ]; then
  if [ -e "` + live + `" ]; then L=live_present; else L=live_gone; fi
  echo "logout:$CODEX_HOME:$L" >> "` + journal + `"
  grep -q '` + revokeToken + `' "$CODEX_HOME/auth.json" 2>/dev/null || exit 1
  rm -f "$CODEX_HOME/auth.json"
  exit 0
fi
exit 0
`
	testexec.WriteShellStub(t, path, script)
	return path
}

// manifestOf reads the coordinator's manifest for codex.
func manifestOf(t *testing.T, coord *providerauth.Coordinator) providerauth.Manifest {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(coord.DataRoot(), "codex", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m providerauth.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// currentPayload returns the bytes of the generation labelled current, or "".
func currentPayload(t *testing.T, coord *providerauth.Coordinator) string {
	t.Helper()
	for _, g := range manifestOf(t, coord).Generations {
		if g.Label != providerauth.LabelCurrent {
			continue
		}
		b, err := os.ReadFile(filepath.Join(coord.DataRoot(), "codex", "generations", g.ID+".auth"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	return ""
}

func journalLines(t *testing.T, journal string) []string {
	t.Helper()
	b, err := os.ReadFile(journal)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// logoutRuns is the journal lines that are a `logout` invocation.
func logoutRuns(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, "logout:") || strings.HasPrefix(l, "argv:logout ") {
			out = append(out, l)
		}
	}
	return out
}

// TestPhoneCodexLogoutMarksRevoked: a phone logout of a ChatGPT session must
// leave no generation retained, BackupState logged_out and RecoveryAvailable
// false, and the next start must not ask for recovery (MADR 0074 §16.5 item
// 9). The direct path leaves CURRENT holding the revoked generation, unmarked.
func TestPhoneCodexLogoutMarksRevoked(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "calls.log")
	bin := fakeCodexPhone(t, journal)
	p, coord, live := logoutFixture(t, bin, `{"tokens":{"access_token":"a","refresh_token":"r"}}`)
	ctx := context.Background()

	if err := p.ClearCredentialMethod(ctx, "", "openai:device"); err != nil {
		t.Fatalf("the phone's clear failed: %v", err)
	}
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Fatal("logout left the live credential in place")
	}

	// The next daemon start.
	state, err := coord.Recover(ctx)
	if err != nil {
		t.Fatalf("recover after a phone logout: %v", err)
	}
	st, err := coord.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := manifestOf(t, coord)
	if state != providerauth.StateLoggedOut || st.BackupState != providerauth.BackupLoggedOut ||
		st.RecoveryAvailable || len(m.Generations) != 0 {
		t.Fatalf("after a phone logout and a restart: state=%s backup=%s recovery_available=%v generations=%d; "+
			"want logged_out, logged_out, false, 0 (the revoked generation must not be retained or offered)",
			state, st.BackupState, st.RecoveryAvailable, len(m.Generations))
	}
}

// TestPhoneCodexKeyWriteCommitsCurrent: a phone key write must leave CURRENT
// holding the new key and a start that sets no recovery_required (MADR 0074
// §16.5 item 9). The direct path leaves a new LIVE with no ordering signal,
// which the next start cannot adopt over the old CURRENT.
func TestPhoneCodexKeyWriteCommitsCurrent(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "calls.log")
	bin := fakeCodexPhone(t, journal)
	p, coord, live := logoutFixture(t, bin, `{"tokens":{"access_token":"a","refresh_token":"r"}}`)
	ctx := context.Background()

	const key = "sk-SENTINELkey0123456789"
	if err := p.SetCredential(ctx, "", "openai:api", key, nil); err != nil {
		t.Fatalf("the phone's key write failed: %v", err)
	}
	b, err := os.ReadFile(live)
	if err != nil || !strings.Contains(string(b), key) {
		t.Fatalf("the live credential does not hold the new key (err=%v)", err)
	}

	// The next daemon start.
	state, err := coord.Recover(ctx)
	if err != nil {
		t.Fatalf("recover after a phone key write: %v", err)
	}
	cur := currentPayload(t, coord)
	if state == providerauth.StateRecoveryRequired || !strings.Contains(cur, key) {
		t.Fatalf("after a phone key write and a restart: state=%s, CURRENT holds the new key=%v; "+
			"want a state other than recovery_required and CURRENT holding the key",
			state, strings.Contains(cur, key))
	}
}

// TestCoordinatedLogoutRevokesWithTheToken (MADR 0074 D38, 16.2 "the revoke runs
// with no token"): RecordLogout deletes LIVE, so `codex logout` run afterwards
// in the daemon's home finds no token and revokes nothing. The revoke runs from
// an owner-only copy taken first. The stub's logout fails unless its home holds
// the refresh token, so today's order cannot pass.
func TestCoordinatedLogoutRevokesWithTheToken(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "order.log")
	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)
	t.Setenv("TMP", tmpRoot)
	t.Setenv("TEMP", tmpRoot)
	// The fixture makes the daemon's home, and the revoking stub must watch
	// that home's LIVE file, so the stub is built after it and swapped in.
	p, coord, live := logoutFixture(t, fakeCodexPhone(t, filepath.Join(t.TempDir(), "unused.log")), chatGPTCred)
	p.cfg.Bin = fakeCodexRevoke(t, journal, live)

	if err := p.CoordinatedLogout(context.Background()); err != nil {
		t.Fatalf("a coordinated ChatGPT logout reported %v; the stub fails unless the revoke saw the token", err)
	}

	runs := logoutRuns(journalLines(t, journal))
	if len(runs) != 1 {
		t.Fatalf("codex logout ran %d times: %q", len(runs), runs)
	}
	parts := strings.SplitN(strings.TrimPrefix(runs[0], "logout:"), ":", 2)
	revokeHome, liveState := parts[0], parts[1]
	if liveState != "live_gone" {
		t.Errorf("the revoke ran with the daemon's LIVE credential %s: the tombstone must come first", liveState)
	}
	if revokeHome == os.Getenv("CODEX_HOME") || !strings.Contains(revokeHome, "mcremote-codex-revoke-") {
		t.Errorf("the revoke ran in %q, want an owner-only copy, not the daemon's home %q", revokeHome, os.Getenv("CODEX_HOME"))
	}
	if _, err := os.Stat(revokeHome); !os.IsNotExist(err) {
		t.Errorf("the temporary home %q outlived the logout", revokeHome)
	}
	st, err := coord.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.BackupState != providerauth.BackupLoggedOut || st.RecoveryAvailable {
		t.Errorf("backup=%s recovery_available=%v, want logged_out, false", st.BackupState, st.RecoveryAvailable)
	}
}

// lateAdapter is codex's adapter with a native-lock path that fails once armed.
// RecordLogout reads the path after its own StateExternal check, so arming it
// after Seed makes RecordLogout fail after MarkRevoked has run.
type lateAdapter struct {
	*CredentialAdapter
	armed *bool
	err   error
}

func (a lateAdapter) NativeLockPath() (string, error) {
	if *a.armed {
		return "", a.err
	}
	return a.CredentialAdapter.NativeLockPath()
}

// lateFixture is logoutFixture with a lateAdapter. Arm it with *armed = true.
func lateFixture(t *testing.T, bin, cred string, failWith error) (*Provider, *providerauth.Coordinator, *bool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(cred), 0o600); err != nil {
		t.Fatal(err)
	}
	armed := new(bool)
	coord, err := providerauth.NewCoordinator(t.TempDir(),
		lateAdapter{CredentialAdapter: NewCredentialAdapter("codex", bin), armed: armed, err: failWith},
		providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewCoordinated(Config{Bin: bin}, nil, coord, nil), coord, armed
}

// TestCoordinatedLogoutRemovesTheCopy: the temporary home is removed on every
// path, including when the revoke fails and when RecordLogout fails after the
// copy was taken.
func TestCoordinatedLogoutRemovesTheCopy(t *testing.T) {
	leftovers := func(root string) []string {
		m, _ := filepath.Glob(filepath.Join(root, "mcremote-codex-revoke-*"))
		return m
	}
	setTmp := func(t *testing.T) string {
		root := t.TempDir()
		t.Setenv("TMPDIR", root)
		t.Setenv("TMP", root)
		t.Setenv("TEMP", root)
		return root
	}

	t.Run("the revoke fails", func(t *testing.T) {
		root := setTmp(t)
		journal := filepath.Join(t.TempDir(), "calls.log")
		bin := fakeCodexLogout(t, journal, 9)
		p, _, _ := logoutFixture(t, bin, chatGPTCred)
		if err := p.CoordinatedLogout(context.Background()); err == nil {
			t.Fatal("a failed revoke was reported as a clean logout")
		}
		if got := leftovers(root); len(got) != 0 {
			t.Fatalf("a failed revoke left %q behind", got)
		}
	})

	t.Run("RecordLogout fails after the copy", func(t *testing.T) {
		root := setTmp(t)
		journal := filepath.Join(t.TempDir(), "calls.log")
		bin := fakeCodexLogout(t, journal, 0)
		p, _, armed := lateFixture(t, bin, chatGPTCred, errors.New("lock path unavailable"))
		*armed = true
		if err := p.CoordinatedLogout(context.Background()); err == nil {
			t.Fatal("a failed RecordLogout was reported as a clean logout")
		}
		if got := leftovers(root); len(got) != 0 {
			t.Fatalf("a failed RecordLogout left %q behind", got)
		}
	})
}

// TestClearCredentialDoesNotRecurse: the phone's clear is CoordinatedLogout,
// and CoordinatedLogout no longer calls ClearCredential, so one logout runs
// `codex logout` once and returns inside its bound.
func TestClearCredentialDoesNotRecurse(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "calls.log")
	bin := fakeCodexPhone(t, journal)
	p, _, _ := logoutFixture(t, bin, chatGPTCred)

	done := make(chan error, 1)
	go func() { done <- p.ClearCredential(context.Background(), "") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("ClearCredential did not return within its bound: it recursed")
	}
	if runs := logoutRuns(journalLines(t, journal)); len(runs) != 1 {
		t.Fatalf("codex logout ran %d times, want once: %q", len(runs), runs)
	}
}

// setExternal rewrites the coordinator's manifest to StateExternal, as a start
// does on a host signed in from outside auth.json (MADR 0134), and returns the
// bytes written.
func setExternal(t *testing.T, coord *providerauth.Coordinator) []byte {
	t.Helper()
	path := filepath.Join(coord.DataRoot(), "codex", "manifest.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["state"] = string(providerauth.StateExternal)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestUnprotectableCodexCredential (Q-D): on a host whose credential the
// coordinator cannot protect, the phone's clear and key write take the direct
// path, in the daemon's home, once each, and change nothing in the coordinator.
func TestUnprotectableCodexCredential(t *testing.T) {
	t.Run("an external manifest", func(t *testing.T) {
		journal := filepath.Join(t.TempDir(), "calls.log")
		bin := fakeCodexPhone(t, journal)
		p, coord, _ := logoutFixture(t, bin, chatGPTCred)
		before := setExternal(t, coord)
		ctx := context.Background()

		if err := p.ClearCredentialMethod(ctx, "", "openai:device"); err != nil {
			t.Fatalf("the phone's clear on an external host: %v", err)
		}

		// MarkRevoked refuses a StateExternal manifest before changing anything, so
		// the clear is the direct `codex logout`, once, in the daemon's home.
		lines := journalLines(t, journal)
		home := os.Getenv("CODEX_HOME")
		var logouts int
		for _, l := range lines {
			if !strings.HasSuffix(l, "home:"+home) {
				t.Errorf("%q ran outside the daemon's home %q", l, home)
			}
			if strings.HasPrefix(l, "argv:logout ") {
				logouts++
			}
		}
		if logouts != 1 {
			t.Errorf("codex logout ran %d times, want once: %q", logouts, lines)
		}
		after, err := os.ReadFile(filepath.Join(coord.DataRoot(), "codex", "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Error("the coordinator's manifest changed on a host it cannot protect")
		}
	})

	// The key write is different, and the PLAN's test text lumped the two: an
	// external manifest is deliberately not a refusal for a sign-in, because the
	// write moves the credential into the file the coordinator protects (MADR
	// 0074 §15.13). Only CheckBackend refuses a key write (deviation 4).
	t.Run("a key write on an external manifest commits through the coordinator", func(t *testing.T) {
		journal := filepath.Join(t.TempDir(), "calls.log")
		bin := fakeCodexPhone(t, journal)
		p, coord, live := logoutFixture(t, bin, chatGPTCred)
		setExternal(t, coord)

		const key = "sk-SENTINELkey0123456789"
		if err := p.SetCredential(context.Background(), "", "openai:api", key, nil); err != nil {
			t.Fatalf("the phone's key write on an external host: %v", err)
		}
		home := os.Getenv("CODEX_HOME")
		for _, l := range journalLines(t, journal) {
			if strings.HasPrefix(l, "argv:login --with-api-key ") && strings.HasSuffix(l, "home:"+home) {
				t.Errorf("%q ran in the daemon's home: the write was not staged in a pending home", l)
			}
		}
		b, err := os.ReadFile(live)
		if err != nil || !strings.Contains(string(b), key) {
			t.Fatalf("the live credential does not hold the new key (err=%v)", err)
		}
	})

	// A keyring credential has no auth.json, so with LIVE absent and a manifest
	// not yet external the coordinator would tombstone a logout that revoked and
	// cleared nothing. CheckBackend refuses first and the direct logout runs
	// (deviation 5).
	t.Run("a logout where CheckBackend refuses and LIVE is absent", func(t *testing.T) {
		journal := filepath.Join(t.TempDir(), "calls.log")
		bin := fakeCodexPhone(t, journal)
		p, coord, live := logoutFixture(t, bin, chatGPTCred)
		home := os.Getenv("CODEX_HOME")
		if err := os.Remove(live); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "config.toml"),
			[]byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(filepath.Join(coord.DataRoot(), "codex", "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}

		if err := p.ClearCredential(context.Background(), ""); err != nil {
			t.Fatalf("the phone's clear on a keyring host: %v", err)
		}
		runs := logoutRuns(journalLines(t, journal))
		if len(runs) != 1 || !strings.HasSuffix(runs[0], "home:"+home) {
			t.Fatalf("codex logout runs = %q, want one in the daemon's home %q", runs, home)
		}
		after, _ := os.ReadFile(filepath.Join(coord.DataRoot(), "codex", "manifest.json"))
		if string(after) != string(before) {
			t.Error("a tombstone was recorded for a logout the coordinator could not perform")
		}
	})

	t.Run("CheckBackend refuses", func(t *testing.T) {
		journal := filepath.Join(t.TempDir(), "calls.log")
		bin := fakeCodexPhone(t, journal)
		p, coord, _ := logoutFixture(t, bin, chatGPTCred)
		home := os.Getenv("CODEX_HOME")
		if err := os.WriteFile(filepath.Join(home, "config.toml"),
			[]byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(filepath.Join(coord.DataRoot(), "codex", "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}

		if err := p.SetCredential(context.Background(), "", "openai:api", "sk-SENTINELkey0123456789", nil); err != nil {
			t.Fatalf("the phone's key write where CheckBackend refuses: %v", err)
		}
		var logins int
		for _, l := range journalLines(t, journal) {
			if strings.HasPrefix(l, "argv:login --with-api-key ") {
				logins++
				if !strings.HasSuffix(l, "home:"+home) {
					t.Errorf("%q ran outside the daemon's home", l)
				}
			}
		}
		if logins != 1 {
			t.Errorf("login --with-api-key ran %d times, want once", logins)
		}
		after, _ := os.ReadFile(filepath.Join(coord.DataRoot(), "codex", "manifest.json"))
		if string(after) != string(before) {
			t.Error("the coordinator's manifest changed on a host it cannot protect")
		}
	})
}

// TestLateUnsupportedBackendIsReturned (Q-D): an ErrUnsupportedBackend that
// RecordLogout returns after MarkRevoked has already run is not an early
// refusal. It is returned as it is, and nothing falls back to a direct logout
// that would run `codex logout` against a credential the coordinator just
// recorded as revoked.
func TestLateUnsupportedBackendIsReturned(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "calls.log")
	bin := fakeCodexPhone(t, journal)
	late := fmt.Errorf("%w: raised after MarkRevoked ran", providerauth.ErrUnsupportedBackend)
	p, _, armed := lateFixture(t, bin, chatGPTCred, late)
	*armed = true

	err := p.ClearCredential(context.Background(), "")
	if !errors.Is(err, providerauth.ErrUnsupportedBackend) {
		t.Fatalf("ClearCredential = %v, want the unsupported-backend error returned", err)
	}
	if errors.Is(err, errEarlyUnsupportedBackend) {
		t.Fatal("a refusal after MarkRevoked was marked as an early one")
	}
	if runs := logoutRuns(journalLines(t, journal)); len(runs) != 0 {
		t.Fatalf("a late refusal fell back to a direct logout: %q", runs)
	}
}
