package daemon

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// TestDaemonNegotiatesHTTP1Only is MADR 0201 D8 (0073 F-T8, daemon half). The
// daemon serves WebSocket upgrades and small GETs; HTTP/2 is unused surface.
// Measured before this fix, a self-signed daemon answered ALPN `h2` — the
// triage's "self-signed is unaffected" was wrong. The server is built exactly as
// the daemon builds it and offered h2 first; it must pick http/1.1.
func TestDaemonNegotiatesHTTP1Only(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.TLS = cfg.TLS.Normalized()
	id, err := EnsureTLS(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer id.Close()
	if id.Mode != config.TLSModeSelfSigned {
		t.Fatalf("mode=%s, want selfsigned", id.Mode)
	}

	srv := newHTTPServer("127.0.0.1:0", http.NotFoundHandler(), quietLogger())
	srv.TLSConfig = id.Config
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ServeTLS(ln, "", "") }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("serve: %v", err)
		}
	}()

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", ln.Addr().String(), &tls.Config{
		NextProtos:         []string{"h2", "http/1.1"},
		InsecureSkipVerify: true, //nolint:gosec // a self-signed test listener; only ALPN is checked
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := conn.ConnectionState().NegotiatedProtocol; got != "http/1.1" {
		t.Fatalf("negotiated %q with a client offering h2 first, want http/1.1", got)
	}
}
