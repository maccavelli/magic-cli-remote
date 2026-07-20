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
