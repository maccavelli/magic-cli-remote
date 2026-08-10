package codex

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeCodex writes a shell script that records its argv and stdin, so the test
// can assert on how the real binary would have been invoked.
func fakeCodex(t *testing.T, exitCode int) (bin, argvFile, stdinFile string) {
	t.Helper()
	dir := t.TempDir()
	argvFile = filepath.Join(dir, "argv")
	stdinFile = filepath.Join(dir, "stdin")
	bin = filepath.Join(dir, "codex-stub")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + argvFile + "\n" +
		"cat > " + stdinFile + "\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, argvFile, stdinFile
}

// The security property that matters here: argv is world-readable through ps
// for the lifetime of the process, so the key must arrive on stdin and appear
// nowhere in the command line (MADR 0074 D1).
func TestSetCredentialSendsKeyOnStdinNotArgv(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"tokens":{"access":"secret"}}`), 0o600); err != nil {
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
