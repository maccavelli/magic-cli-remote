package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/daemon"
	"github.com/maccavelli/magic-cli-remote/internal/pairuri"

	"github.com/mdp/qrterminal/v3"
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
	// Even with an operator-pinned advertise host, LE must win: the CA issues
	// for the domain, so advertising anything else fails hostname verification.
	cfg.Pair.AdvertiseHost = "100.64.0.1:7531"

	got := resolvePairHost("", cfg)
	if got != "devbox.ts.lallygag.net:7531" {
		t.Fatalf("host=%q want devbox.ts.lallygag.net:7531", got)
	}
}

func TestResolvePairHostSelfSignedUsesAdvertiseHost(t *testing.T) {
	cfg := selfSignedConfig(t)
	cfg.Pair.AdvertiseHost = "100.64.0.1:7531"

	got := resolvePairHost("", cfg)
	if got != "100.64.0.1:7531" {
		t.Fatalf("host=%q want 100.64.0.1:7531", got)
	}

	// A bare host inherits listen.port.
	cfg.Pair.AdvertiseHost = "devbox.local"
	if got := resolvePairHost("", cfg); got != "devbox.local:7531" {
		t.Fatalf("host=%q want devbox.local:7531", got)
	}
}

// The per-run --host flag overrides even a configured advertise host.
func TestResolvePairHostFlagBeatsAdvertiseHost(t *testing.T) {
	cfg := selfSignedConfig(t)
	cfg.Pair.AdvertiseHost = "100.64.0.1:7531"
	if got := resolvePairHost("other.example.com:9443", cfg); got != "other.example.com:9443" {
		t.Fatalf("host=%q want other.example.com:9443", got)
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

// letsencrypt mode must still advertise a pin: it is the recovery path when
// ACME issuance fails and the daemon falls back to the self-signed identity.
// Without it, every LE failure locks out every paired phone permanently.
func TestPairFingerprintEmittedForLetsEncrypt(t *testing.T) {
	cfg := leConfig(t)
	fp, err := pairFingerprint(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(fp) != 43 {
		t.Fatalf("fingerprint=%q (len %d) want 43-char base64url in letsencrypt mode", fp, len(fp))
	}
}

// The pin advertised in letsencrypt mode must be the *fallback* leaf — the very
// certificate daemon.EnsureTLS serves when ACME fails — or it protects nothing.
func TestPairFingerprintLetsEncryptMatchesFallbackLeaf(t *testing.T) {
	cfg := leConfig(t)
	fp, err := pairFingerprint(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := daemon.EnsureCerts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if b.Generated {
		t.Fatal("second EnsureCerts regenerated the identity")
	}
	if got := b.FingerprintBase64(); got != fp {
		t.Fatalf("advertised %q but fallback leaf is %q", fp, got)
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
		{"selfsigned", "abc", config.TLSModeSelfSigned, "selfsigned — pinned by the app"},
		{"letsencrypt", "abc", config.TLSModeLetsEncrypt, "fallback if ACME issuance fails"},
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

// runPairCode drives the real `pair code` command against an isolated config
// file and data dir, and returns the pair URI it printed. Going through cobra
// rather than calling the helpers directly is the point: it is the wiring
// between pairFingerprint, the resolved mode and pairuri.Encode that regressed.
func runPairCode(t *testing.T, yaml string) pairuri.Payload {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "data_dir: " + filepath.Join(dir, "data") + "\n" + yaml
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCREMOTE_CONFIG", cfgPath)
	t.Setenv("MCREMOTE_PAIR_HOST", "100.64.0.1:7531")

	cmd := newPairCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"code", "--qr=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("pair code: %v\n%s", err, buf.String())
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		if i := strings.Index(line, "mcremote://"); i >= 0 {
			p, err := pairuri.Parse(strings.TrimSpace(line[i:]))
			if err != nil {
				t.Fatalf("parse pair URI: %v", err)
			}
			return p
		}
	}
	t.Fatalf("no pair URI in output:\n%s", buf.String())
	return pairuri.Payload{}
}

func TestPairURICarriesModeAndFingerprintSelfSigned(t *testing.T) {
	p := runPairCode(t, "tls:\n  mode: selfsigned\n")
	if p.Mode != config.TLSModeSelfSigned {
		t.Fatalf("mode=%q want selfsigned", p.Mode)
	}
	if len(p.Fingerprint) != 43 {
		t.Fatalf("fp=%q want 43-char base64url", p.Fingerprint)
	}
}

// The regression this whole change exists for: letsencrypt mode used to emit a
// URI with no fp=, so an ACME failure left every paired phone with nothing to
// fall back to.
func TestPairURICarriesModeAndFingerprintLetsEncrypt(t *testing.T) {
	p := runPairCode(t, "tls:\n  mode: letsencrypt\n  letsencrypt:\n"+
		"    domains: [devbox.example.com]\n    email: ops@example.com\n")
	if p.Mode != config.TLSModeLetsEncrypt {
		t.Fatalf("mode=%q want letsencrypt", p.Mode)
	}
	if len(p.Fingerprint) != 43 {
		t.Fatalf("fp=%q want 43-char base64url in letsencrypt mode", p.Fingerprint)
	}
	if !strings.Contains(p.Host, "devbox.example.com") {
		t.Fatalf("host=%q want the certificate's DNS name", p.Host)
	}
}

func TestPairURIOmitsFingerprintWhenTLSOff(t *testing.T) {
	p := runPairCode(t, "tls:\n  mode: off\n")
	if p.Mode != config.TLSModeOff {
		t.Fatalf("mode=%q want off", p.Mode)
	}
	if p.Fingerprint != "" {
		t.Fatalf("fp=%q want empty with TLS off", p.Fingerprint)
	}
	if !strings.HasPrefix(p.Host, "ws://") {
		t.Fatalf("host=%q want an explicit ws:// prefix", p.Host)
	}
}

// `pair code --ttl 0` (or a negative TTL) must be rejected, not silently fall
// back to the ~5-minute default: a "disable" or typo must never mint a live
// bearer code. mint coerces ttl<=0 to the default, so the guard lives here.
func TestPairCodeRejectsNonPositiveTTL(t *testing.T) {
	for _, ttl := range []string{"0", "0s", "-1m"} {
		t.Run(ttl, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			body := "data_dir: " + filepath.Join(dir, "data") + "\ntls:\n  mode: off\n"
			if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MCREMOTE_CONFIG", cfgPath)

			cmd := newPairCmd()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs([]string{"code", "--qr=false", "--ttl=" + ttl})
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("--ttl=%s should be rejected, got success:\n%s", ttl, buf.String())
			}
			if !strings.Contains(err.Error(), "--ttl must be positive") {
				t.Fatalf("error = %v, want a positive-TTL rejection", err)
			}
			// The default fallback would have printed a live code; it must not.
			if strings.Contains(buf.String(), "Pair code:") {
				t.Fatalf("a pair code was minted despite --ttl=%s:\n%s", ttl, buf.String())
			}
		})
	}
}

// `pair lisst` (a typo for a subcommand) must be rejected outright, not fall
// through to the default RunE and silently mint a live 5-minute bearer code.
// Regression for the cobra.NoArgs gap on the pair command.
func TestPairRejectsTypoSubcommandWithoutMinting(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "data_dir: " + dataDir + "\ntls:\n  mode: off\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCREMOTE_CONFIG", cfgPath)

	// Use the full root tree: pair must be a *child* command here. As a child it
	// has a parent, so cobra's default legacyArgs would let an unknown arg fall
	// through to RunE — exactly the case cobra.NoArgs must now reject. (A bare
	// newPairCmd() has no parent and legacyArgs already rejects it, masking the
	// bug.)
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"pair", "lisst", "--qr=false"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("pair lisst should be rejected, got success:\n%s", buf.String())
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error = %v, want an unknown-command rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, "pair_codes.json")); statErr == nil {
		t.Fatal("a pair code was minted despite the typo'd subcommand")
	}
}

// TestPairQRStaysScannable pins the printed QR's module count.
//
// Not a scannability *claim* — 61 modules has not been shown to fail on a real
// device, and the earlier suspicion that it had was mistaken. This is a change
// detector: the pair URI grows silently whenever a field is added to it (relay
// advertising, MADR 0061, added `&hid=…&relay=…` and took the code from 53
// modules to 61), and nothing errors when it does, because the QR stays
// perfectly valid — it just gets denser.
//
// If this fails, re-check on a phone before assuming either direction. Lowering
// the error-correction level to L buys back 8 modules if it is ever needed.
func TestPairQRStaysScannable(t *testing.T) {
	const maxWidth = 61

	// The widest realistic payload: pair code + fingerprint + TLS mode + a
	// relay tuple, all URL-encoded.
	uri := "mcremote://pair?code=Z2S2E3S8" +
		"&fp=Tm-_s2l3OJIRLSuofiq21VHE-zTDQqhLaEZFtLslI0c" +
		"&hid=macos-laptop" +
		"&host=wss%3A%2F%2F100.64.0.3%3A7531" +
		"&mode=selfsigned" +
		"&relay=wss%3A%2F%2Fheadscale.lallygag.net%3A8443"

	var buf bytes.Buffer
	qrterminal.GenerateWithConfig(uri, qrterminal.Config{
		Level:      qrterminal.L,
		Writer:     &buf,
		HalfBlocks: true,
		QuietZone:  2,
	})

	width := 0
	for _, line := range strings.Split(buf.String(), "\n") {
		if n := len([]rune(line)); n > width {
			width = n
		}
	}
	if width == 0 {
		t.Fatal("no QR was rendered")
	}
	if width > maxWidth {
		t.Errorf("pair QR is %d modules wide, want <= %d.\n"+
			"The pair URI (%d chars) grew. Verify on a device, then either "+
			"shorten the URI, lower the error-correction level, or raise this "+
			"bound deliberately.", width, maxWidth, len(uri))
	}
}
