package daemon

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEnsureTLSSelfSigned(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.TLS = cfg.TLS.Normalized()

	id, err := EnsureTLS(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer id.Close()

	if id.Mode != config.TLSModeSelfSigned {
		t.Fatalf("mode=%s", id.Mode)
	}
	if id.Config == nil || id.SelfSigned == nil || id.FellBack {
		t.Fatalf("identity=%+v", id)
	}
}

func TestEnsureTLSOff(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.TLS = cfg.TLS.WithEnabled(false)

	id, err := EnsureTLS(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer id.Close()

	if id.Mode != config.TLSModeOff || id.Config != nil {
		t.Fatalf("identity=%+v", id)
	}
}

// A misconfigured or unreachable ACME setup must never stop the daemon from
// coming up: it downgrades to the self-signed identity. The config here is
// rejected by certs.EnsureACME before any network call is made (no email), so
// this exercises the fallback without touching a live CA.
func TestEnsureTLSFallsBackWhenACMEFails(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.TLS.Mode = config.TLSModeLetsEncrypt
	cfg.TLS.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net"}
	// Email deliberately omitted — EnsureACME rejects it locally.

	id, err := EnsureTLS(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("ACME failure must not be fatal: %v", err)
	}
	defer id.Close()

	if id.Mode != config.TLSModeSelfSigned {
		t.Fatalf("mode=%s want selfsigned fallback", id.Mode)
	}
	if !id.FellBack {
		t.Fatal("FellBack should record the downgrade")
	}
	if id.Config == nil || id.SelfSigned == nil {
		t.Fatalf("identity=%+v", id)
	}
	// The self-signed fallback must still cover the ACME name so a phone that
	// pins the fingerprint out-of-band can at least match the hostname.
	if err := id.SelfSigned.Leaf.VerifyHostname("devbox.ts.lallygag.net"); err != nil {
		t.Fatalf("fallback leaf missing ACME SAN: %v", err)
	}
}
