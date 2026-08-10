package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// fakeDeviceCodex imitates codex-cli 0.146.0's device flow, including the part
// that makes MADR 0074 D8 necessary: it DELETES the existing credential the
// moment it starts, before the user has done anything.
//
// writeBack controls whether the flow "succeeds" and stores a new credential.
func fakeDeviceCodex(t *testing.T, authPath string, writeBack bool, sleepSeconds string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex-device-stub")
	after := ""
	if writeBack {
		after = "printf '{\"tokens\":{\"access\":\"fresh\"}}' > " + authPath + "\n"
	}
	script := "#!/bin/sh\n" +
		"rm -f " + authPath + "\n" +
		"printf '1. Open this link\\n   https://auth.openai.com/codex/device\\n'\n" +
		"printf '2. Enter this one-time code\\n   K5GK-PUGKG\\n'\n" +
		"sleep " + sleepSeconds + "\n" +
		after
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

// seedCredential puts a credential in a temp HOME and returns its path.
func seedCredential(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"tokens":{"access":"ORIGINAL"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// D8, first clause: with a credential present, the flow is refused outright
// unless the caller confirmed. A mis-tap must not sign the host out.
func TestDeviceAuthRefusedWithoutConfirmation(t *testing.T) {
	path := seedCredential(t)
	p := New(Config{Bin: fakeDeviceCodex(t, path, false, "1")})

	_, _, err := p.StartDeviceAuth(context.Background(), "openai", "", nil, false)
	if err == nil {
		t.Fatal("destructive flow started without confirmation")
	}
	if !errors.Is(err, provider.ErrAuthConfirmRequired) {
		t.Fatalf("want ErrAuthConfirmRequired, got %v", err)
	}
	// And crucially, nothing was destroyed by the refusal itself.
	b, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("refusal deleted the credential: %v", readErr)
	}
	if string(b) != `{"tokens":{"access":"ORIGINAL"}}` {
		t.Fatalf("refusal modified the credential: %s", b)
	}
}

// D8, restore clause: the CLI deletes the credential at start, so abandoning
// the flow must put it back byte-for-byte. This is the exact scenario that
// signed this host out during the MADR 0074 research.
func TestDeviceAuthRestoresCredentialOnCancel(t *testing.T) {
	path := seedCredential(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p := New(Config{Bin: fakeDeviceCodex(t, path, false, "120")})

	_, wait, err := p.StartDeviceAuth(context.Background(), "openai", "", nil, true)
	if err != nil {
		t.Fatalf("StartDeviceAuth: %v", err)
	}
	// The stub has already deleted it, mirroring the real CLI.
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("test stub did not reproduce the deletion; the guard is untested")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- wait(ctx) }()
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled flow reported success")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("wait never returned after cancel")
	}

	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("credential was not restored: %v", err)
	}
	if string(restored) != string(original) {
		t.Fatalf("restored credential differs:\n got %s\nwant %s", restored, original)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("restored credential mode = %o, want 600", st.Mode().Perm())
	}
}

// A clean exit that stored nothing is still a failure, and still restores.
func TestDeviceAuthRestoresWhenFlowStoresNothing(t *testing.T) {
	path := seedCredential(t)
	p := New(Config{Bin: fakeDeviceCodex(t, path, false, "0")})

	_, wait, err := p.StartDeviceAuth(context.Background(), "openai", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := wait(context.Background()); err == nil {
		t.Fatal("a flow that stored no credential reported success")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("credential not restored after an empty success: %v", err)
	}
}

// On success the new credential must survive — the restore must not stomp it.
func TestDeviceAuthKeepsNewCredentialOnSuccess(t *testing.T) {
	path := seedCredential(t)
	p := New(Config{Bin: fakeDeviceCodex(t, path, true, "0")})

	_, wait, err := p.StartDeviceAuth(context.Background(), "openai", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := wait(context.Background()); err != nil {
		t.Fatalf("successful flow reported %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"tokens":{"access":"fresh"}}` {
		t.Fatalf("restore clobbered the new credential: %s", b)
	}
}

// A cold host has nothing to lose, so no confirmation is demanded.
func TestDeviceAuthNeedsNoConfirmationOnColdHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	p := New(Config{Bin: fakeDeviceCodex(t, path, true, "0")})

	flow, wait, err := p.StartDeviceAuth(context.Background(), "openai", "", nil, false)
	if err != nil {
		t.Fatalf("cold host demanded confirmation: %v", err)
	}
	if flow.UserCode != "K5GK-PUGKG" {
		t.Errorf("user code = %q", flow.UserCode)
	}
	if err := wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestDeviceAuthRejectsUnknownUpstream(t *testing.T) {
	seedCredential(t)
	p := New(Config{Bin: "codex"})
	if _, _, err := p.StartDeviceAuth(context.Background(), "anthropic", "", nil, true); err == nil {
		t.Fatal("accepted an upstream codex does not have")
	}
}
