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
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// healthzServer serves body with status on 127.0.0.1, over TLS when cert is
// set, until the test ends, and returns its port.
func healthzServer(t *testing.T, status int, body string, cert *tls.Certificate) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if cert != nil {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{*cert}, MinVersion: tls.VersionTLS12})
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// writeRelayConfig writes a private relay config. The folder is owner-only,
// so on Windows the file's DACL passes the relay's config guard, which a
// mode alone does not.
func writeRelayConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(privateFixtureDir(t), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// namedCert is a self-signed serving certificate for relay.test, written to
// the files a relay's tls.cert_file and tls.key_file name.
func namedCert(t *testing.T) (tls.Certificate, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "relay.test"},
		DNSNames:     []string{"relay.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	dir := privateFixtureDir(t)
	certPath, keyPath := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return pair, certPath, keyPath
}

func plainConfig(host string, port int) string {
	return fmt.Sprintf("listen:\n  host: %q\n  port: %d\ntls:\n  mode: off\n", host, port)
}

// TestHealthzProbeReady: the relay's /healthz answering 200 {"ok":true} is
// ready, over plaintext and over TLS verified against the relay's own
// certificate file (MADR 0054 A4; PLAN S8 step 1d).
func TestHealthzProbeReady(t *testing.T) {
	t.Run("plaintext", func(t *testing.T) {
		isolateRelay(t)
		port := healthzServer(t, 200, `{"ok":true}`, nil)
		args := service.ServeArgs{ConfigPath: writeRelayConfig(t, plainConfig("127.0.0.1", port))}
		if h, err := healthzProbe(context.Background(), args); err != nil || !h.Ready {
			t.Fatalf("probe = %+v, %v; want ready", h, err)
		}
	})
	t.Run("tls files", func(t *testing.T) {
		isolateRelay(t)
		pair, certPath, keyPath := namedCert(t)
		port := healthzServer(t, 200, `{"ok":true}`, &pair)
		cfg := fmt.Sprintf("listen:\n  host: 127.0.0.1\n  port: %d\ntls:\n  mode: files\n  cert_file: %s\n  key_file: %s\n", port, certPath, keyPath)
		args := service.ServeArgs{ConfigPath: writeRelayConfig(t, cfg)}
		if h, err := healthzProbe(context.Background(), args); err != nil || !h.Ready {
			t.Fatalf("probe = %+v, %v; want ready over TLS", h, err)
		}
	})
}

// TestHealthzProbeFollowsTheBakedListenPort: a relay set up with its own
// --listen-port is probed there, not at its configuration's port (MADR 0054
// amendment S8, S1).
func TestHealthzProbeFollowsTheBakedListenPort(t *testing.T) {
	isolateRelay(t)
	port := healthzServer(t, 200, `{"ok":true}`, nil)
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	other := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()
	args := service.ServeArgs{ConfigPath: writeRelayConfig(t, plainConfig("127.0.0.1", other)), ListenPort: port}
	if h, err := healthzProbe(context.Background(), args); err != nil || !h.Ready {
		t.Fatalf("probe = %+v, %v; want ready on the baked port %d", h, err, port)
	}
}

// TestHealthzProbeDialsLoopbackForAWildcard: a relay listening on every
// address is reached on loopback.
func TestHealthzProbeDialsLoopbackForAWildcard(t *testing.T) {
	isolateRelay(t)
	port := healthzServer(t, 200, `{"ok":true}`, nil)
	args := service.ServeArgs{ConfigPath: writeRelayConfig(t, plainConfig("0.0.0.0", port))}
	if h, err := healthzProbe(context.Background(), args); err != nil || !h.Ready {
		t.Fatalf("probe = %+v, %v; want ready through loopback", h, err)
	}
}

// TestHealthzProbeNotReady: only status 200 with the body {"ok":true} is
// ready.
func TestHealthzProbeNotReady(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"a 500", 500, `{"ok":true}`},
		{"ok false", 200, `{"ok":false}`},
		{"another body", 200, `{"ok":true,"hosts":3}`},
		{"not JSON", 200, `ok`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateRelay(t)
			port := healthzServer(t, tc.status, tc.body, nil)
			args := service.ServeArgs{ConfigPath: writeRelayConfig(t, plainConfig("127.0.0.1", port))}
			h, err := healthzProbe(context.Background(), args)
			if err != nil {
				t.Fatalf("probe error = %v, want an observation", err)
			}
			if h.Ready {
				t.Fatalf("probe = %+v, want not ready on %d %s", h, tc.status, tc.body)
			}
		})
	}
}

// TestHealthzProbeErrorsWithNothingListening: a refused connection is an
// error, which the backend keeps until the wait times out.
func TestHealthzProbeErrorsWithNothingListening(t *testing.T) {
	isolateRelay(t)
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()
	args := service.ServeArgs{ConfigPath: writeRelayConfig(t, plainConfig("127.0.0.1", port))}
	if h, err := healthzProbe(context.Background(), args); err == nil || h.Ready {
		t.Fatalf("probe = %+v, %v; want an error", h, err)
	}
}
