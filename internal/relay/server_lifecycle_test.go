package relay

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/certs"
)

// 0115 P8: deterministic unit tests for the failure paths the audit found
// cold — exactly where F1–F4 had been hiding.

func lifecycleServer(t *testing.T, limits Limits) *Server {
	t.Helper()
	cred, err := ParseAllowFlag("h1:0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{
		ListenAddr: "127.0.0.1:0",
		Allow:      []HostCredential{cred},
		Limits:     limits,
	}, nil)
}

func wsClient(t *testing.T, ts *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

func TestTLSHandshakeLogFilter(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f := &tlsHandshakeLogFilter{lg: lg}

	cases := []struct {
		line, wantLevel string
	}{
		{"http: TLS handshake error from 1.2.3.4: EOF", "level=DEBUG"},
		{"client sent an HTTP request to an HTTPS server", "level=DEBUG"},
		{"unsupported SSLv2 handshake received", "level=DEBUG"},
		{"http: superfluous response.WriteHeader", "level=WARN"},
	}
	for _, c := range cases {
		buf.Reset()
		if _, err := f.Write([]byte(c.line + "\n")); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), c.wantLevel) {
			t.Errorf("%q logged as %q, want %s", c.line, buf.String(), c.wantLevel)
		}
	}
	buf.Reset()
	if _, err := f.Write([]byte("   \n")); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("blank line produced output: %q", buf.String())
	}
}

// TestRateLimitedWSAppliesFloor: sub-floor window remainders are raised to
// retryFloor so a refused peer is never told to retry in under 5 s.
func TestRateLimitedWSAppliesFloor(t *testing.T) {
	srv := lifecycleServer(t, DefaultLimits())
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		srv.rateLimitedWS(r.Context(), c, "req-1", time.Second)
	}))
	defer up.Close()
	conn := wsClient(t, up, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env, err := ReadEnvelope(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	var ep ErrorPayload
	if err := DecodePayload(env, &ep); err != nil {
		t.Fatal(err)
	}
	if ep.Code != "rate_limited" || ep.RetryAfterMS < retryFloor.Milliseconds() {
		t.Fatalf("payload %+v; want rate_limited with retry >= %dms", ep, retryFloor.Milliseconds())
	}
}

// TestPingHostControlFailure: a dead control conn fails the ping and fires
// the cancel exactly as the register loop relies on.
func TestPingHostControlFailure(t *testing.T) {
	srv := lifecycleServer(t, DefaultLimits())
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		<-r.Context().Done()
		_ = c.CloseNow()
	}))
	defer up.Close()
	conn := wsClient(t, up, "")
	// Close our side first: Ping on a locally-closed conn fails immediately,
	// where a peer-closed one would wait out the 10 s pong timeout (Ping
	// requires a concurrent Read to observe the pong at all).
	_ = conn.CloseNow()

	var failed atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		srv.pingHostControl(ctx, conn, func() { failed.Store(true) }, 20*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("pingHostControl did not return after ping failure")
	}
	if !failed.Load() {
		t.Fatal("fail cancel was not invoked")
	}
}

func TestSpliceAccountingHelpers(t *testing.T) {
	srv := lifecycleServer(t, DefaultLimits())
	if got := srv.Addr(); got != "127.0.0.1:0" {
		t.Fatalf("Addr=%q", got)
	}
	un1 := srv.trackSplice(func() {}, nil, nil, "h1")
	un2 := srv.trackSplice(func() {}, nil, nil, "h1")
	un3 := srv.trackSplice(func() {}, nil, nil, "h2")
	if srv.ActiveSplices() != 3 {
		t.Fatalf("active=%d", srv.ActiveSplices())
	}
	counts := srv.spliceHostCounts()
	if counts["h1"] != 2 || counts["h2"] != 1 {
		t.Fatalf("counts=%v", counts)
	}
	un1()
	un2()
	un3()
	if srv.ActiveSplices() != 0 {
		t.Fatalf("active=%d after untrack", srv.ActiveSplices())
	}
}

// TestListenAndServeRefusesPublicPlaintext: 0091 D5 fail-closed branch.
func TestListenAndServeRefusesPublicPlaintext(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:0123456789abcdef")
	srv := New(Config{ListenAddr: "0.0.0.0:0", Allow: []HostCredential{cred}}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := srv.ListenAndServe(ctx)
	if err == nil || !strings.Contains(err.Error(), "plaintext listen") {
		t.Fatalf("err=%v; want plaintext refusal", err)
	}
}

// TestListenAndServeLoopbackPlaintext: loopback is the sanctioned plaintext
// exception; the server must start and stop cleanly.
func TestListenAndServeLoopbackPlaintext(t *testing.T) {
	srv := lifecycleServer(t, DefaultLimits())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop on cancel")
	}
}

// TestListenAndServeTLSFiles: files-mode TLS with a generated self-signed
// pair (the branch the audit measured at 0 outside e2e).
func TestListenAndServeTLSFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := certs.Ensure(certs.Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	cred, _ := ParseAllowFlag("h1:0123456789abcdef")
	srv := New(Config{
		ListenAddr:  "127.0.0.1:0",
		Allow:       []HostCredential{cred},
		TLSCertFile: dir + "/" + certs.DefaultCertName,
		TLSKeyFile:  dir + "/" + certs.DefaultKeyName,
		Limits:      DefaultLimits(),
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("serve tls: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tls serve did not stop on cancel")
	}
}

// TestHandleTunnelRejections drives handleTunnel's refusal paths end to end.
func TestHandleTunnelRejections(t *testing.T) {
	srv := lifecycleServer(t, DefaultLimits())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	send := func(env Envelope) (Envelope, error) {
		conn := wsClient(t, ts, "/v1/tunnel")
		if err := WriteEnvelope(ctx, conn, env); err != nil {
			t.Fatal(err)
		}
		return ReadEnvelope(ctx, conn)
	}
	expect := func(env Envelope, code string) {
		t.Helper()
		got, err := send(env)
		if err != nil {
			t.Fatalf("code %s: %v", code, err)
		}
		var ep ErrorPayload
		_ = DecodePayload(got, &ep)
		if got.Type != TypeError || ep.Code != code {
			t.Fatalf("got %s/%s, want error/%s", got.Type, ep.Code, code)
		}
	}

	wrongType, _ := NewEnvelope(TypeJoin, "x", JoinPayload{HostID: "h1"})
	expect(wrongType, "bad_payload")
	badHost, _ := NewEnvelope(TypeTunnel, "x", TunnelPayload{SessionID: "s1", HostID: "no spaces!"})
	expect(badHost, "bad_payload")
	badSession, _ := NewEnvelope(TypeTunnel, "x", TunnelPayload{SessionID: "s?!", HostID: "h1"})
	expect(badSession, "bad_payload")
	unknown, _ := NewEnvelope(TypeTunnel, "x", TunnelPayload{SessionID: "does-not-exist", HostID: "h1", Token: "t"})
	expect(unknown, "unknown_session")
}

// TestHandleHostRejections drives handleHost's refusal paths: wrong first
// type, invalid id, and the per-IP register rate limit.
func TestHandleHostRejections(t *testing.T) {
	limits := DefaultLimits()
	limits.RegisterPerMinute = 1
	srv := lifecycleServer(t, limits)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Wrong first envelope type.
	conn := wsClient(t, ts, "/v1/host")
	j, _ := NewEnvelope(TypeJoin, "x", JoinPayload{HostID: "h1"})
	if err := WriteEnvelope(ctx, conn, j); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadEnvelope(ctx, conn); err != nil || got.Type != TypeError {
		t.Fatalf("wrong-type: %+v err=%v", got, err)
	}

	// Invalid host_id.
	conn = wsClient(t, ts, "/v1/host")
	bad, _ := NewEnvelope(TypeRegister, "x", RegisterPayload{HostID: "bad id!", Secret: "0123456789abcdef"})
	if err := WriteEnvelope(ctx, conn, bad); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadEnvelope(ctx, conn); err != nil || got.Type != TypeError {
		t.Fatalf("invalid-id: %+v err=%v", got, err)
	}

	// Register bucket: first register passes, second (same IP) is refused.
	reg, _ := NewEnvelope(TypeRegister, "1", RegisterPayload{HostID: "h1", Secret: "0123456789abcdef"})
	conn = wsClient(t, ts, "/v1/host")
	if err := WriteEnvelope(ctx, conn, reg); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadEnvelope(ctx, conn); err != nil || got.Type != TypeRegisterOK {
		t.Fatalf("first register: %+v err=%v", got, err)
	}
	conn2 := wsClient(t, ts, "/v1/host")
	if err := WriteEnvelope(ctx, conn2, reg); err != nil {
		t.Fatal(err)
	}
	got, err := ReadEnvelope(ctx, conn2)
	if err != nil {
		t.Fatal(err)
	}
	var ep ErrorPayload
	_ = DecodePayload(got, &ep)
	if got.Type != TypeError || ep.Code != "rate_limited" {
		t.Fatalf("second register: %s/%s, want error/rate_limited", got.Type, ep.Code)
	}
}

// TestRegisterReplacement: a reconnecting host replaces its stale control
// registration; the old control is closed with "replaced".
func TestRegisterReplacement(t *testing.T) {
	limits := DefaultLimits()
	srv := lifecycleServer(t, limits)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg, _ := NewEnvelope(TypeRegister, "1", RegisterPayload{HostID: "h1", Secret: "0123456789abcdef"})
	first := wsClient(t, ts, "/v1/host")
	if err := WriteEnvelope(ctx, first, reg); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadEnvelope(ctx, first); got.Type != TypeRegisterOK {
		t.Fatalf("first: %+v", got)
	}
	second := wsClient(t, ts, "/v1/host")
	if err := WriteEnvelope(ctx, second, reg); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadEnvelope(ctx, second); got.Type != TypeRegisterOK {
		t.Fatalf("second: %+v", got)
	}
	// The first control must observe closure promptly.
	rctx, rcancel := context.WithTimeout(ctx, 3*time.Second)
	defer rcancel()
	if _, err := ReadEnvelope(rctx, first); err == nil {
		t.Fatal("stale control still readable after replacement")
	}
}

func TestSlogHostIDSanitizes(t *testing.T) {
	if got := slogHostID(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if got := slogHostID("ok-host"); got != "ok-host" {
		t.Fatalf("clean: %q", got)
	}
	if got := slogHostID("a\x00b\x7fc"); got != "a?b?c" {
		t.Fatalf("control chars: %q", got)
	}
	long := strings.Repeat("x", LogHostIDMaxRunes+10)
	got := slogHostID(long)
	if len([]rune(got)) != LogHostIDMaxRunes+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncation: %d runes, %q", len([]rune(got)), got[len(got)-9:])
	}
}

func TestDecodePayloadEmpty(t *testing.T) {
	var dest JoinPayload
	if err := DecodePayload(Envelope{}, &dest); err != nil {
		t.Fatal(err)
	}
	if dest.HostID != "" {
		t.Fatal("empty payload must not mutate dest")
	}
}
