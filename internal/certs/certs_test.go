package certs_test

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/certs"
)

func TestEnsureGeneratesAndReuses(t *testing.T) {
	dir := t.TempDir()

	first, err := certs.Ensure(certs.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Generated {
		t.Fatal("expected first Ensure to generate")
	}

	second, err := certs.Ensure(certs.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if second.Generated {
		t.Fatal("expected second Ensure to reuse the persisted pair")
	}
	if first.FingerprintBase64() != second.FingerprintBase64() {
		t.Fatal("fingerprint changed across runs; every paired phone would break")
	}
}

// A complete *.new pair left after a crash mid-install is promoted on Ensure
// (Phase 3.1).
func TestPromoteStagedPair(t *testing.T) {
	dir := t.TempDir()
	// Install a live pair.
	live, err := certs.Ensure(certs.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	// Stage a second identity as *.new without installing it through Ensure.
	staged, err := certs.Ensure(certs.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	certNew := live.CertPath + ".new"
	keyNew := live.KeyPath + ".new"
	if err := copyFile(t, staged.CertPath, certNew); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(t, staged.KeyPath, keyNew); err != nil {
		t.Fatal(err)
	}

	// Corrupt the live cert so load fails — Ensure must promote *.new.
	if err := os.WriteFile(live.CertPath, []byte("not a cert\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := certs.Ensure(certs.Options{Dir: dir})
	if err != nil {
		t.Fatalf("Ensure after staged pair: %v", err)
	}
	if got.FingerprintBase64() != staged.FingerprintBase64() {
		t.Fatal("expected promoted staged fingerprint")
	}
}

func copyFile(t *testing.T, src, dst string) error {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

func TestKeyIsNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	b, err := certs.Ensure(certs.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{b.KeyPath, b.CertPath} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, want 0600", filepath.Base(p), st.Mode().Perm())
		}
	}
}

func TestExpiredCertIsRegenerated(t *testing.T) {
	dir := t.TempDir()

	old, err := certs.Ensure(certs.Options{
		Dir:      dir,
		Validity: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Same files, but "now" is past the renewal window.
	fresh, err := certs.Ensure(certs.Options{
		Dir: dir,
		Now: func() time.Time { return time.Now().Add(48 * time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.Generated {
		t.Fatal("expected an expired cert to be regenerated")
	}
	if fresh.FingerprintBase64() == old.FingerprintBase64() {
		t.Fatal("regenerated cert must have a new fingerprint")
	}
}

func TestSANsCoverLocalhostAndLoopback(t *testing.T) {
	b, err := certs.Ensure(certs.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Leaf.VerifyHostname("localhost"); err != nil {
		t.Fatalf("localhost not covered: %v", err)
	}
	if err := b.Leaf.VerifyHostname("127.0.0.1"); err != nil {
		t.Fatalf("127.0.0.1 not covered: %v", err)
	}
	if b.Leaf.PublicKeyAlgorithm != x509.ECDSA {
		t.Fatalf("key algorithm = %v, want ECDSA", b.Leaf.PublicKeyAlgorithm)
	}
}

func TestExtraHostSAN(t *testing.T) {
	b, err := certs.Ensure(certs.Options{
		Dir:        t.TempDir(),
		ExtraHosts: []string{"devbox.example", "100.64.0.9"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Leaf.VerifyHostname("devbox.example"); err != nil {
		t.Fatal(err)
	}
	if err := b.Leaf.VerifyHostname("100.64.0.9"); err != nil {
		t.Fatal(err)
	}
}

// The fingerprint the CLI prints must equal the digest a pinning client
// computes over the peer certificate it actually receives — otherwise every
// pin would fail closed.
func TestFingerprintMatchesServedCertificate(t *testing.T) {
	b, err := certs.Ensure(certs.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", b.TLSConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = conn.(*tls.Conn).Handshake()
		conn.Close()
	}()

	var peer *x509.Certificate
	client, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // pinning is what we're testing
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			c, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return err
			}
			peer = c
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if peer == nil {
		t.Fatal("no peer certificate observed")
	}
	sum := sha256.Sum256(peer.Raw)
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	if got != b.FingerprintBase64() {
		t.Fatalf("served fingerprint %q != advertised %q", got, b.FingerprintBase64())
	}
}

// SPKIFingerprint identifies the public key, not the certificate DER: it must
// equal base64url(sha256(RawSubjectPublicKeyInfo)) and stay stable when the
// certificate is regenerated around the same key (ADR 0005). This is the exact
// scheme the Dart client and future cross-checks assert against.
func TestSPKIFingerprintTracksKeyNotCert(t *testing.T) {
	b, err := certs.Ensure(certs.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	leaf := b.Leaf

	sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	got := certs.SPKIFingerprint(leaf)
	if got != want {
		t.Fatalf("SPKIFingerprint = %q, want %q", got, want)
	}
	// It is the key, not the cert DER: distinct from the DER-based fingerprint.
	if got == b.FingerprintBase64() {
		t.Fatal("SPKI fingerprint must differ from the certificate DER fingerprint")
	}
	// Length is a 32-byte digest in unpadded base64url (43 chars).
	if len(got) != 43 {
		t.Fatalf("fingerprint length = %d, want 43", len(got))
	}
}

func TestSANsHelperSkipsUnspecified(t *testing.T) {
	_, ips := certs.SANs([]string{"0.0.0.0", "192.0.2.7:7531"})
	for _, ip := range ips {
		if ip.IsUnspecified() {
			t.Fatal("0.0.0.0 must not become a SAN")
		}
	}
	found := false
	for _, ip := range ips {
		if ip.Equal(net.ParseIP("192.0.2.7")) {
			found = true
		}
	}
	if !found {
		t.Fatal("host:port extra host should contribute its IP as a SAN")
	}
}

// The serving leaf must not be a CA. It is inert while pinned, but the natural
// way to make curl or a browser work is to install it in a trust store — and a
// 10-year CA there could sign for any name on the internet.
func TestServingLeafIsNotACA(t *testing.T) {
	b, err := certs.Ensure(certs.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if b.Leaf.IsCA {
		t.Error("leaf has IsCA=true; installing it in a trust store yields a general-purpose CA")
	}
	if b.Leaf.KeyUsage&x509.KeyUsageCertSign != 0 {
		t.Error("leaf carries KeyUsageCertSign")
	}
	if b.Leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Error("leaf lost KeyUsageDigitalSignature")
	}
	if !b.Leaf.BasicConstraintsValid {
		t.Error("basic constraints must be marked valid so IsCA=false is actually asserted")
	}
	var serverAuth bool
	for _, eku := range b.Leaf.ExtKeyUsage {
		if eku == x509.ExtKeyUsageServerAuth {
			serverAuth = true
		}
	}
	if !serverAuth {
		t.Error("leaf lost ExtKeyUsageServerAuth")
	}
}

func TestEnsureReportsWhyItGenerated(t *testing.T) {
	dir := t.TempDir()
	first, err := certs.Ensure(certs.Options{Dir: dir, Validity: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if first.GeneratedReason != certs.ReasonFirstRun {
		t.Fatalf("reason=%q want %q", first.GeneratedReason, certs.ReasonFirstRun)
	}
	renewed, err := certs.Ensure(certs.Options{
		Dir: dir,
		Now: func() time.Time { return time.Now().Add(48 * time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if renewed.GeneratedReason != certs.ReasonExpiring {
		t.Fatalf("reason=%q want %q", renewed.GeneratedReason, certs.ReasonExpiring)
	}
}

// A pair that is present but unreadable must be an error, never a silent
// re-key: minting a new identity there invalidates every paired device, so a
// transient read failure would escalate into "re-pair every phone".
func TestCorruptCertFailsWithoutRegenerating(t *testing.T) {
	dir := t.TempDir()
	orig, err := certs.Ensure(certs.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	keyBefore, err := os.ReadFile(orig.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orig.CertPath, []byte("-----BEGIN CERTIFICATE-----\ntruncated"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := certs.Ensure(certs.Options{Dir: dir}); err == nil {
		t.Fatal("Ensure accepted a corrupt certificate instead of failing loudly")
	}

	certAfter, err := os.ReadFile(orig.CertPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(certAfter) != "-----BEGIN CERTIFICATE-----\ntruncated" {
		t.Fatal("Ensure overwrote the corrupt certificate")
	}
	keyAfter, err := os.ReadFile(orig.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(keyAfter) != string(keyBefore) {
		t.Fatal("Ensure replaced the private key — the identity was silently re-keyed")
	}
}

// A half-present pair is a damaged identity, not a first run: generating would
// overwrite whichever file survived.
func TestMissingKeyDoesNotRegenerate(t *testing.T) {
	dir := t.TempDir()
	orig, err := certs.Ensure(certs.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	certBefore, err := os.ReadFile(orig.CertPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(orig.KeyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := certs.Ensure(certs.Options{Dir: dir}); err == nil {
		t.Fatal("Ensure regenerated over a certificate whose key was merely missing")
	}
	certAfter, err := os.ReadFile(orig.CertPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(certAfter) != string(certBefore) {
		t.Fatal("Ensure overwrote the surviving certificate")
	}
}

// A host clock that jumps backward (a delayed NTP step, a suspend/resume, a
// VM whose time was not yet synced) must NOT re-key the identity. The leaf is
// trusted by its pinned fingerprint, so regenerating on a backward clock would
// silently lock out every paired phone — the exact failure this hardening
// prevents.
func TestBackwardClockDoesNotRegenerate(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

	first, err := certs.Ensure(certs.Options{
		Dir: dir,
		Now: func() time.Time { return base },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Generated {
		t.Fatal("expected first run to generate")
	}

	// Clock jumps a day into the past.
	second, err := certs.Ensure(certs.Options{
		Dir: dir,
		Now: func() time.Time { return base.Add(-24 * time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Generated {
		t.Fatal("a backward clock must not regenerate the identity (would break every paired phone)")
	}
	if first.FingerprintBase64() != second.FingerprintBase64() {
		t.Fatal("fingerprint changed after a backward clock jump")
	}
}

// Even a clock reading before the leaf's (backdated) NotBefore — a "not yet
// valid" window — must reuse the persisted identity rather than re-key. Trust
// is the pin, not the validity window.
func TestClockBeforeNotBeforeStillReuses(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

	first, err := certs.Ensure(certs.Options{
		Dir: dir,
		Now: func() time.Time { return base },
	})
	if err != nil {
		t.Fatal(err)
	}

	// A clock even earlier than the backdated NotBefore edge.
	before := base.Add(-(certs.BackdateSkew + 10*24*time.Hour))
	if !before.Before(first.Leaf.NotBefore) {
		t.Fatalf("test setup: %v is not before NotBefore %v", before, first.Leaf.NotBefore)
	}
	second, err := certs.Ensure(certs.Options{
		Dir: dir,
		Now: func() time.Time { return before },
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Generated {
		t.Fatal("a not-yet-valid leaf (clock before NotBefore) must be reused, not re-keyed")
	}
	if first.FingerprintBase64() != second.FingerprintBase64() {
		t.Fatal("fingerprint changed for a not-yet-valid leaf")
	}
}

// The minted leaf's NotBefore is backdated by a generous margin so ordinary
// clock wobble never lands before the window edge.
func TestLeafNotBeforeIsBackdatedForClockSkew(t *testing.T) {
	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	b, err := certs.Ensure(certs.Options{
		Dir: t.TempDir(),
		Now: func() time.Time { return base },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := b.Leaf.NotBefore, base.Add(-certs.BackdateSkew); !got.Equal(want) {
		t.Fatalf("NotBefore = %v, want %v (now - BackdateSkew)", got, want)
	}
	if certs.BackdateSkew < 24*time.Hour {
		t.Fatalf("BackdateSkew = %v, want a generous margin for clock skew", certs.BackdateSkew)
	}
}

// Genuine forward expiry still rotates — the hardening tolerates a backward
// clock without disabling real renewal.
func TestForwardExpiryStillRegenerates(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

	old, err := certs.Ensure(certs.Options{
		Dir:      dir,
		Validity: time.Hour,
		Now:      func() time.Time { return base },
	})
	if err != nil {
		t.Fatal(err)
	}

	// Well past NotAfter (base + 1h) and its RenewBefore window.
	fresh, err := certs.Ensure(certs.Options{
		Dir: dir,
		Now: func() time.Time { return base.Add(48 * time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.Generated || fresh.GeneratedReason != certs.ReasonExpiring {
		t.Fatalf("expected a genuinely expired leaf to rotate as %q; got generated=%v reason=%q",
			certs.ReasonExpiring, fresh.Generated, fresh.GeneratedReason)
	}
	if fresh.FingerprintBase64() == old.FingerprintBase64() {
		t.Fatal("rotated cert must have a new fingerprint")
	}
}
