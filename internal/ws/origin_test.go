package ws_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// originTestServer stands up a minimal WS server (no auth: the Origin check runs
// at the upgrade, before any authentication) with the given allowlist.
func originTestServer(t *testing.T, allowed []string) string {
	t.Helper()
	reg := provider.NewRegistry()
	mgr := session.NewManager(reg, nil, nil, nil)
	srv := ws.New(ws.Options{
		Sessions:       mgr,
		Registry:       reg,
		AllowedOrigins: allowed,
		Version:        "test",
		ListenAddr:     "127.0.0.1:0",
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return "ws" + ts.URL[len("http"):] + "/v1/ws"
}

func dialWithOrigin(t *testing.T, wsURL, origin string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var opts *websocket.DialOptions
	if origin != "" {
		opts = &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}}
	}
	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "")
	}
	return err
}

// A cross-origin browser request must be rejected at the upgrade — the core of
// the fix: otherwise a malicious page on the operator's host/tailnet could open
// the socket and (with auth off) drive agent sessions.
func TestWSRejectsCrossOrigin(t *testing.T) {
	wsURL := originTestServer(t, nil)
	if err := dialWithOrigin(t, wsURL, "http://evil.example"); err == nil {
		t.Fatal("cross-origin browser request should be rejected")
	}
}

// A native client (Flutter, CLI) sends no Origin header and must be accepted.
func TestWSAcceptsNoOrigin(t *testing.T) {
	wsURL := originTestServer(t, nil)
	if err := dialWithOrigin(t, wsURL, ""); err != nil {
		t.Fatalf("no-Origin (native) request should be accepted, got %v", err)
	}
}

// An operator can opt a browser origin into the allowlist for Flutter-web.
func TestWSAllowlistedOriginAccepted(t *testing.T) {
	wsURL := originTestServer(t, []string{"app.example.com"})
	if err := dialWithOrigin(t, wsURL, "https://app.example.com"); err != nil {
		t.Fatalf("allowlisted origin should be accepted, got %v", err)
	}
	// A different cross-origin is still rejected even with an allowlist present.
	if err := dialWithOrigin(t, wsURL, "https://other.example.com"); err == nil {
		t.Fatal("non-allowlisted origin should still be rejected")
	}
}
