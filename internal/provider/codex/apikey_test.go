package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// fakeCodexAPIKey writes a stand-in codex whose `login --with-api-key` reads
// the key from stdin and writes it into CODEX_HOME, and whose `login status`
// succeeds when a credential is present. It also records its full argv and
// environment so a test can prove the key never appeared in either.
func fakeCodexAPIKey(t *testing.T, journal string, exitCode int) string {
	t.Helper()
	testexec.SkipIfNoPOSIXShell(t)
	path := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
{ echo "argv:$*"; env; } >> "` + journal + `"
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  [ -f "$CODEX_HOME/auth.json" ] || exit 1
  exit 0
fi
if [ "$1" = "login" ] && [ "$2" = "--with-api-key" ]; then
  read -r key
  [ ` + "`" + `printf %s "$exit_code"` + "`" + ` ] 2>/dev/null
  if [ "` + itoaTest(exitCode) + `" != "0" ]; then exit ` + itoaTest(exitCode) + `; fi
  printf '{"OPENAI_API_KEY":"%s","tokens":null}' "$key" > "$CODEX_HOME/auth.json"
  chmod 600 "$CODEX_HOME/auth.json"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}

// TestCoordinatedAPIKeyPublishesAndKeepsTheChain proves an API-key login is
// published through the same transaction and shares the one CURRENT/PREVIOUS
// chain, because Codex has one native credential (MADR 0074 P18 step 9).
func TestCoordinatedAPIKeyPublishesAndKeepsTheChain(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "argv.log")
	bin := fakeCodexAPIKey(t, journal, 0)
	p, coord, live := logoutFixture(t, bin, `{"tokens":{"access_token":"a","refresh_token":"r"}}`)

	const key = "sk-SENTINELkey0123456789"
	if err := p.SetCredentialCoordinated(context.Background(), "", "openai:api", key); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), key) {
		t.Fatalf("live credential does not hold the new key")
	}
	st, err := coord.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The prior ChatGPT generation is retained as PREVIOUS in the same chain.
	if st.BackupState != providerauth.BackupCurrent || !st.RecoveryAvailable {
		t.Fatalf("backup state = %s recoverable = %v, want a healthy chain", st.BackupState, st.RecoveryAvailable)
	}
}

// TestCoordinatedAPIKeyNeverLeaksTheKey proves the key crosses only stdin.
func TestCoordinatedAPIKeyNeverLeaksTheKey(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "argv.log")
	bin := fakeCodexAPIKey(t, journal, 0)
	p, coord, _ := logoutFixture(t, bin, `{"tokens":{"access_token":"a","refresh_token":"r"}}`)

	const key = "sk-SENTINELkey0123456789"
	if err := p.SetCredentialCoordinated(context.Background(), "", "openai:api", key); err != nil {
		t.Fatal(err)
	}

	// argv and the child environment must be clean.
	b, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, key) {
			t.Fatalf("the key reached argv or the environment: %q", line)
		}
	}

	// The manifest must be clean; only the immutable generation payload holds
	// the credential.
	root := filepath.Join(coord.DataRoot(), "codex")
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if filepath.Base(filepath.Dir(path)) == "generations" {
			return nil
		}
		body, readErr := os.ReadFile(path) //nolint:gosec // test walk
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(body), key) {
			t.Errorf("%s contains the key", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestCoordinatedAPIKeyFailureLeavesLiveUntouched proves a rejected key never
// disturbs the working credential.
func TestCoordinatedAPIKeyFailureLeavesLiveUntouched(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "argv.log")
	bin := fakeCodexAPIKey(t, journal, 3)
	p, _, live := logoutFixture(t, bin, `{"tokens":{"access_token":"a","refresh_token":"r"}}`)
	before, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}

	err = p.SetCredentialCoordinated(context.Background(), "", "openai:api", "sk-SENTINELkey0123456789")
	if err == nil {
		t.Fatal("a failed login reported success")
	}
	if strings.Contains(err.Error(), "sk-SENTINEL") {
		t.Fatalf("error text leaked the key: %v", err)
	}
	after, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("a failed login changed the live credential")
	}
}

// TestCoordinatedAPIKeyRejectsBadInput covers the guard rails.
func TestCoordinatedAPIKeyRejectsBadInput(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "argv.log")
	bin := fakeCodexAPIKey(t, journal, 0)
	p, _, _ := logoutFixture(t, bin, `{"OPENAI_API_KEY":"sk-old"}`)
	ctx := context.Background()

	if err := p.SetCredentialCoordinated(ctx, "", "openai:api", "   "); err == nil {
		t.Error("a blank key was accepted")
	}
	if err := p.SetCredentialCoordinated(ctx, "nope", "openai:api", "k"); err == nil {
		t.Error("an unknown upstream was accepted")
	}
	if err := p.SetCredentialCoordinated(ctx, "", "openai:device", "k"); err == nil {
		t.Error("the device method was accepted for a key write")
	}
	if err := New(Config{Bin: bin}).SetCredentialCoordinated(ctx, "", "openai:api", "k"); err == nil {
		t.Error("an uncoordinated provider wrote a credential transactionally")
	}
}
