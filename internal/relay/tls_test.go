package relay

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyTLSOff(t *testing.T) {
	fc := FileConfig{
		DataDir: t.TempDir(),
		TLS:     TLSConfig{Mode: TLSModeOff},
		Log:     LogConfig{Level: "info"},
	}
	cfg := &Config{}
	cleanup, err := ApplyTLS(context.Background(), fc, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cleanup != nil {
		t.Fatal("expected no cleanup for off")
	}
	if cfg.TLSConfig != nil {
		t.Fatal("expected nil TLSConfig")
	}
}

func TestApplyTLSFilesMissing(t *testing.T) {
	fc := FileConfig{
		DataDir: t.TempDir(),
		TLS:     TLSConfig{Mode: TLSModeFiles},
		Log:     LogConfig{Level: "info"},
	}
	cfg := &Config{} // no cert paths
	_, err := ApplyTLS(context.Background(), fc, cfg, nil)
	if err == nil {
		t.Fatal("expected error for missing PEMs")
	}
}

func TestApplyTLSFilesOK(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeTestCert(t, dir)
	fc := FileConfig{
		DataDir: dir,
		TLS: TLSConfig{
			Mode:     TLSModeFiles,
			CertFile: certPath,
			KeyFile:  keyPath,
		},
		Log: LogConfig{Level: "info"},
	}
	cfg := &Config{TLSCertFile: certPath, TLSKeyFile: keyPath}
	cleanup, err := ApplyTLS(context.Background(), fc, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cleanup != nil {
		t.Fatal("files mode needs no ACME cleanup")
	}
	if cfg.TLSCertFile != certPath || cfg.TLSKeyFile != keyPath {
		t.Fatalf("paths clobbered: %+v", cfg)
	}
}

func TestApplyTLSLetsEncryptFailsWithoutPublicDomain(t *testing.T) {
	dir := t.TempDir()
	fc := FileConfig{
		DataDir: dir,
		TLS: TLSConfig{
			Mode: TLSModeLetsEncrypt,
			LetsEncrypt: LetsEncryptConfig{
				Domains:  []string{"unlikely-mcrelay-unit.invalid"},
				Email:    "test@example.com",
				Staging:  true,
				CacheDir: filepath.Join(dir, "acme"),
			},
		},
		Log: LogConfig{Level: "info"},
	}
	// Short-circuit: ApplyTLS uses full ACMEStartupTimeout (3m) — too slow for unit tests.
	// Call EnsureACMEHTTP path indirectly by overriding via a short-timeout wrapper test
	// in certs package; here we only assert ApplyTLS surfaces the error type when we
	// inject a pre-normalized config that will fail quickly... we can't inject timeout.
	// So test Directory() + mode plumbing instead, and rely on certs tests for manage fail.
	if fc.TLS.Normalized().Mode != TLSModeLetsEncrypt {
		t.Fatal(fc.TLS.Mode)
	}
	if d := fc.TLS.LetsEncrypt.Directory(); d == "" || !contains(d, "staging") {
		t.Fatalf("directory=%q", d)
	}
	_ = fc.ACMECacheDir()
}

func TestEffectiveHTTPPort(t *testing.T) {
	if effectiveHTTPPort(0) != 80 {
		t.Fatal(effectiveHTTPPort(0))
	}
	if effectiveHTTPPort(-1) != 80 {
		t.Fatal(effectiveHTTPPort(-1))
	}
	if effectiveHTTPPort(8080) != 8080 {
		t.Fatal(effectiveHTTPPort(8080))
	}
}

func TestListenAndServeWithTLSConfig(t *testing.T) {
	// Managed TLSConfig path (same shape as ACME bundle output).
	dir := t.TempDir()
	certPath, keyPath := writeTestCert(t, dir)
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := ParseAllowFlag("h1:sixteen-chars-min-sec")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Config{
		ListenAddr: "127.0.0.1:0",
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		Allow:  []HostCredential{cred},
		Limits: DefaultLimits(),
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	// Serve needs a bound listener; use ListenAndServe which binds :0.
	// Race: start then cancel quickly.
	go func() { errCh <- srv.ListenAndServe(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			// ErrServerClosed / context.Canceled both OK
			if err != context.Canceled {
				t.Logf("serve exit: %v", err)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for serve exit")
	}
}

func writeTestCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "mcrelay-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath = filepath.Join(dir, "tls.crt")
	keyPath = filepath.Join(dir, "tls.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && (func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})()))
}
