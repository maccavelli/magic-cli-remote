package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

func leConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.TLS.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net"}
	cfg.TLS.LetsEncrypt.Email = "ops@lallygag.net"
	cfg.TLS = cfg.TLS.Normalized()
	if cfg.TLS.Mode != config.TLSModeLetsEncrypt {
		t.Fatalf("mode=%s want letsencrypt", cfg.TLS.Mode)
	}
	return cfg
}

func selfSignedConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.TLS = cfg.TLS.Normalized()
	if cfg.TLS.Mode != config.TLSModeSelfSigned {
		t.Fatalf("mode=%s want selfsigned", cfg.TLS.Mode)
	}
	return cfg
}

// In letsencrypt mode the QR must carry the certificate's DNS name: the CA
// never issues for the 100.64/10 mesh IP the daemon actually binds.
func TestResolvePairHostLetsEncryptUsesDomain(t *testing.T) {
	cfg := leConfig(t)
	// Even with a Tailscale IP available via the env override, LE wins.
	t.Setenv("MCREMOTE_PAIR_HOST", "100.64.0.1:7531")

	got := resolvePairHost("", cfg)
	if got != "devbox.ts.lallygag.net:7531" {
		t.Fatalf("host=%q want devbox.ts.lallygag.net:7531", got)
	}
}

func TestResolvePairHostSelfSignedUsesAddress(t *testing.T) {
	cfg := selfSignedConfig(t)
	t.Setenv("MCREMOTE_PAIR_HOST", "100.64.0.1:7531")

	got := resolvePairHost("", cfg)
	if got != "100.64.0.1:7531" {
		t.Fatalf("host=%q want 100.64.0.1:7531", got)
	}
}

func TestResolvePairHostExplicitFlagWins(t *testing.T) {
	cfg := leConfig(t)
	if got := resolvePairHost("other.example.com", cfg); got != "other.example.com:7531" {
		t.Fatalf("host=%q", got)
	}
	if got := resolvePairHost("other.example.com:9443", cfg); got != "other.example.com:9443" {
		t.Fatalf("host=%q", got)
	}
}

// Pinning a publicly trusted cert would break at the first ~60-day renewal,
// so fp= must be absent from the pair URI in letsencrypt mode.
func TestPairFingerprintOmittedForLetsEncrypt(t *testing.T) {
	cfg := leConfig(t)
	fp, err := pairFingerprint(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if fp != "" {
		t.Fatalf("fingerprint=%q want empty in letsencrypt mode", fp)
	}
}

func TestPairFingerprintEmittedForSelfSigned(t *testing.T) {
	cfg := selfSignedConfig(t)
	fp, err := pairFingerprint(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(fp) != 43 { // unpadded base64url SHA-256
		t.Fatalf("fingerprint=%q (len %d) want 43-char base64url", fp, len(fp))
	}
}

func TestPairFingerprintOmittedWhenTLSOff(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.TLS = cfg.TLS.WithEnabled(false)
	fp, err := pairFingerprint(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if fp != "" {
		t.Fatalf("fingerprint=%q want empty", fp)
	}
}

func TestPrintFingerprintLines(t *testing.T) {
	cases := []struct {
		name string
		fp   string
		mode string
		want string
	}{
		{"selfsigned", "abc", config.TLSModeSelfSigned, "pinned by the app"},
		{"letsencrypt", "", config.TLSModeLetsEncrypt, "publicly trusted"},
		{"off", "", config.TLSModeOff, "cleartext"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			printFingerprint(&buf, "Cert:   ", tc.fp, tc.mode)
			if !strings.Contains(buf.String(), tc.want) {
				t.Fatalf("output %q missing %q", buf.String(), tc.want)
			}
		})
	}
}
