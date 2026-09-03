package codex

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// fakeCodex writes a shell script that records its argv and stdin, so the test
// can assert on how the real binary would have been invoked.
//
// The helper isolates the Codex home ITSELF rather than trusting each caller to
// do it. It used to write its stub credential to "$HOME/.codex/auth.json" and
// leave the environment to the test: TestClearCredentialRunsLogout did not set
// HOME, so running this package overwrote the developer's real
// ~/.codex/auth.json with the three-byte stub `{}` on every run — destroying a
// live ChatGPT credential and forcing a re-login. That artefact was then
// mistaken for Codex behaviour and reasoned about at length in MADR 0074
// §15.13 and MADR 0134.
//
// CODEX_HOME is what credstore.CodexAuthPath consults first, so setting it is
// what actually redirects the code under test; HOME is set as well so nothing
// that falls back to ~/.codex can escape either. Both are t.Setenv, so they are
// restored when the test ends.
func fakeCodex(t *testing.T, exitCode int) (bin, argvFile, stdinFile string) {
	t.Helper()
	testexec.SkipIfNoPOSIXShell(t)
	dir := t.TempDir()
	codexHome := filepath.Join(dir, "codex-home")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("HOME", dir)

	argvFile = filepath.Join(dir, "argv")
	stdinFile = filepath.Join(dir, "stdin")
	bin = filepath.Join(dir, "codex-stub")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + argvFile + "\n" +
		"cat > " + stdinFile + "\n" +
		"if [ " + strconv.Itoa(exitCode) + " -eq 0 ]; then\n" +
		"  mkdir -p \"" + codexHome + "\"\n" +
		"  printf '{}\\n' > \"" + codexHome + "/auth.json\"\n" +
		"fi\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, argvFile, stdinFile
}

// TestFakeCodexCannotEscapeItsSandbox is the regression guard for the defect
// above. It asserts the stub writes only inside the directories the helper
// created, so a future edit that reintroduces "$HOME/.codex" — or a caller that
// forgets to isolate — fails here instead of on someone's real credential.
func TestFakeCodexCannotEscapeItsSandbox(t *testing.T) {
	realHome := t.TempDir() // stands in for the developer's home
	t.Setenv("HOME", realHome)
	canary := filepath.Join(realHome, ".codex", "auth.json")

	bin, _, _ := fakeCodex(t, 0)
	p := New(Config{Bin: bin})
	if err := p.ClearCredential(context.Background(), "openai"); err != nil {
		t.Fatalf("ClearCredential: %v", err)
	}

	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Fatalf("the codex stub wrote to %s: a test must never touch the real "+
			"credential path (err=%v)", canary, err)
	}
	// And it must still have written where the code under test looks, or the
	// assertion above would pass for the wrong reason.
	isolated, err := credstore.CodexAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(isolated); err != nil {
		t.Fatalf("stub did not write the isolated credential at %s: %v", isolated, err)
	}
}

// The security property that matters here: argv is world-readable through ps
// for the lifetime of the process, so the key must arrive on stdin and appear
// nowhere in the command line (MADR 0074 D1).
func TestSetCredentialSendsKeyOnStdinNotArgv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const secret = "sk-live-DO-NOT-LEAK-argv"
	bin, argvFile, stdinFile := fakeCodex(t, 0)

	p := New(Config{Bin: bin})
	if err := p.SetCredential(context.Background(), "openai", "", secret, nil); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}

	argv, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(argv), secret) {
		t.Fatalf("secret appeared in argv (visible via ps): %s", argv)
	}
	want := "login\n--with-api-key\n"
	if string(argv) != want {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != secret {
		t.Fatalf("stdin = %q, want the secret", stdin)
	}
}

// A failing CLI must surface as an error, and its output must not smuggle the
// key back out through the error string.
func TestSetCredentialFailureDoesNotEchoSecret(t *testing.T) {
	const secret = "sk-live-DO-NOT-LEAK-err"
	bin, _, _ := fakeCodex(t, 1)

	p := New(Config{Bin: bin})
	err := p.SetCredential(context.Background(), "openai", "", secret, nil)
	if err == nil {
		t.Fatal("a failing codex login reported success")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error text carried the secret: %v", err)
	}
}

func TestSetCredentialValidatesInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bin, _, _ := fakeCodex(t, 0)
	p := New(Config{Bin: bin})

	if err := p.SetCredential(context.Background(), "openai", "", "  ", nil); err == nil {
		t.Error("accepted a blank credential")
	}
	if err := p.SetCredential(context.Background(), "not-openai", "", "sk-x", nil); err == nil {
		t.Error("accepted an unknown upstream")
	}
}

func TestClearCredentialRunsLogout(t *testing.T) {
	bin, argvFile, _ := fakeCodex(t, 0)
	p := New(Config{Bin: bin})
	if err := p.ClearCredential(context.Background(), "openai"); err != nil {
		t.Fatalf("ClearCredential: %v", err)
	}
	argv, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(argv)) != "logout" {
		t.Fatalf("argv = %q, want logout", argv)
	}
}

// Status is presence-only and must never read the file's contents.
func TestAuthStatusReportsPresence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p := New(Config{Bin: "codex"})
	st, err := p.AuthStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "missing" {
		t.Fatalf("cold host reported %q, want missing", st.Status)
	}

	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The real auth.json shape: codex's TokenData requires id_token,
	// access_token, and refresh_token. The field names matter now that
	// "configured" means a usable credential rather than a file that exists
	// (MADR 0074 §15.13) — an invented shape would be reported missing, which
	// is the correct answer for a file holding nothing codex could use.
	if err := os.WriteFile(filepath.Join(dir, "auth.json"),
		[]byte(`{"tokens":{"id_token":"jwt","access_token":"a","refresh_token":"r"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err = p.AuthStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "configured" {
		t.Fatalf("with a store present, status = %q", st.Status)
	}
	// Both methods are advertised, and the destructive one is distinguishable
	// so the phone can gate it (MADR 0074 D8).
	var device bool
	for _, m := range st.Upstreams[0].Methods {
		if m.Type == "oauth_device" {
			device = true
		}
	}
	if !device {
		t.Errorf("device method not advertised: %+v", st.Upstreams[0].Methods)
	}
}
