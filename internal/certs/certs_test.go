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
