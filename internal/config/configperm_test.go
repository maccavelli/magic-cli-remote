package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
)

// MADR 0155 P2. These tables decide whether a daemon starts, so each one names
// the state it is asserting rather than only the outcome.

const (
	listenOnlyConfig = "listen:\n  port: 7531\n"
	secretConfig     = "relay:\n  secret: 0123456789abcdef\n"
)

// privateConfig writes body to a directory made private with the product's own
// primitive, and returns the path.
//
// The directory matters: a t.TempDir() file on Windows inherits whatever the
// host's %TEMP% grants, which on the machine this was written on includes a
// third-party group. PLAN 0154 P2 learned this the hard way — a fixture that
// depends on the host's ACLs tests the host, not the code.
func privateConfig(t *testing.T, body string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "cfg")
	if err := appdirs.EnsurePrivateDir(dir); err != nil {
		t.Fatalf("EnsurePrivateDir: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestGuardConfigFileAcceptsAPrivateConfig(t *testing.T) {
	path := privateConfig(t, "listen:\n  port: 7531\n")
	cfg := Config{}
	if err := guardConfigFile(&cfg, path, func(string) bool { return false }); err != nil {
		t.Fatalf("a private config was refused: %v", err)
	}
	if len(cfg.Diagnostics) != 0 {
		t.Errorf("a private config produced diagnostics: %v", cfg.Diagnostics)
	}
}

// A config readable by another principal, with no credential in it, must not
// stop the daemon. This is the case that made MADR 0155 reject mirroring
// mcrelay's hard fail: stopping here would be a self-update that breaks
// working installations.
//
// On Unix the guard repairs the file (D8) and the assertion is that it did. On
// Windows repair is deliberately a no-op, so the assertion is that a warning
// and a Diagnostic were produced and startup continued.
func TestGuardConfigFileToleratesAnExposedConfigWithoutASecret(t *testing.T) {
	path := privateConfig(t, listenOnlyConfig)
	makeNonPrivate(t, path)

	cfg := Config{}
	if err := guardConfigFile(&cfg, path, func(string) bool { return false }); err != nil {
		t.Fatalf("a secret-free config must not be fatal: %v", err)
	}

	ok, err := appdirs.FileIsOwnerOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if ok {
			t.Fatal("fixture did not make the file non-private; the test would assert nothing")
		}
		if len(cfg.Diagnostics) != 1 || cfg.Diagnostics[0].Code != configPermCode {
			t.Fatalf("want one %s diagnostic, got %v", configPermCode, cfg.Diagnostics)
		}
		return
	}
	if !ok {
		t.Error("D8 repair did not make the file owner-only")
	}
	if len(cfg.Diagnostics) != 0 {
		t.Errorf("a repaired config still produced diagnostics: %v", cfg.Diagnostics)
	}
}

// The fatal case: exposed, and the exposure includes a credential.
//
// It is fatal on every platform, and on Unix it is fatal even though the guard
// repairs the file first (MADR 0155 amendment, 2026-09-12). The first version
// of this test tried to make the file "unrepairable" by dropping write on its
// directory. That does not stop chmod(2) on a file the process owns, so on
// Linux the repair succeeded, the guard returned nil, and the daemon would
// have started with an exposed credential. CI run 34717428526 found it; every
// earlier run of this test was on Windows, where no repair happens.
//
// The two platforms reach the two fatal messages naturally, so there is no
// production seam here: Unix takes the repaired branch, Windows the
// not-repaired one, which is also the path a failed Unix repair takes.
func TestGuardConfigFileIsFatalWithAnInlineSecret(t *testing.T) {
	path := privateConfig(t, secretConfig)
	makeNonPrivate(t, path)
	if ok, err := appdirs.FileIsOwnerOnly(path); err != nil || ok {
		t.Fatalf("fixture did not make the file non-private (ok=%v err=%v); the test would assert nothing", ok, err)
	}

	cfg := Config{}
	err := guardConfigFile(&cfg, path, func(key string) bool { return key == "relay.secret" })
	if err == nil {
		t.Fatal("a config readable by another principal and carrying a credential must be fatal")
	}
	msg := err.Error()
	if !strings.Contains(msg, "rotate") {
		t.Errorf("err=%v; D9 requires the message to say the credential is exposed", err)
	}
	if !strings.Contains(msg, path) {
		t.Errorf("err=%v; the message must name the file", err)
	}
	if strings.Contains(msg, "relay.secret") || strings.Contains(msg, "0123456789abcdef") {
		t.Errorf("err=%v; PLAN 0155 C1 forbids naming the field or the value", err)
	}
	if len(cfg.Diagnostics) != 0 {
		t.Errorf("a fatal refusal also produced diagnostics: %v", cfg.Diagnostics)
	}

	ok, ferr := appdirs.FileIsOwnerOnly(path)
	if ferr != nil {
		t.Fatal(ferr)
	}
	if runtime.GOOS == "windows" {
		if ok {
			t.Error("the file became private on Windows, where repair is deliberately a no-op")
		}
		if !strings.Contains(msg, "is readable by another principal") {
			t.Errorf("err=%v; an unrepaired file must be described as still readable", err)
		}
		return
	}
	// Unix: D8 ran before the refusal, so the refusal is not vacuous about A3.
	if !ok {
		t.Error("D8 repair did not make the file owner-only before refusing")
	}
	if !strings.Contains(msg, "tightened to 0600") {
		t.Errorf("err=%v; a repaired file's refusal must say the permissions were tightened", err)
	}
	if strings.Contains(msg, "chmod") {
		t.Errorf("err=%v; the chmod has already been done, so the message must not ask for it", err)
	}
}

// C1: no secret value, and no field name, reaches the message.
func TestGuardConfigFileMessageNamesNoSecret(t *testing.T) {
	const secret = "0123456789abcdef"
	path := privateConfig(t, "relay:\n  secret: "+secret+"\n")
	cfg := Config{}
	err := guardConfigFile(&cfg, path, func(key string) bool { return key == "relay.secret" })
	text := ""
	if err != nil {
		text = err.Error()
	}
	for _, d := range cfg.Diagnostics {
		text += " " + d.Message
	}
	if strings.Contains(text, secret) {
		t.Errorf("the secret value appears in output: %q", text)
	}
	if strings.Contains(text, "relay.secret") {
		t.Errorf("the message names the exposed setting, which PLAN 0155 C1 forbids: %q", text)
	}
}

// The remedy must be the one the operator's platform actually has.
func TestOwnerOnlyRemedyIsPlatformCorrect(t *testing.T) {
	got := ownerOnlyRemedy("/tmp/config.yaml")
	if runtime.GOOS == "windows" {
		if strings.Contains(strings.ToLower(got), "chmod") {
			t.Errorf("Windows remedy says chmod, which does not exist there: %q", got)
		}
		return
	}
	if !strings.Contains(got, "chmod 0600") {
		t.Errorf("Unix remedy = %q, want it to name chmod 0600", got)
	}
}
