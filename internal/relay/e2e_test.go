package relay_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/relay"
	"github.com/maccavelli/magic-cli-remote/internal/relayhost"
)

// TestE2EHostClientPhoneSplice is the R20 CI e2e path: mcrelay + relayhost
// register + phone join + opaque bytes through the tunnel bridge.
func TestE2EHostClientPhoneSplice(t *testing.T) {
	const hostID = "e2e-host"
	const secret = "sixteen-chars-e2e-sec"

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()

	cred, err := relay.ParseAllowFlag(hostID + ":" + secret)
	if err != nil {
		t.Fatal(err)
	}
	srv := relay.New(relay.Config{
		Allow: []relay.HostCredential{cred},
		Limits: relay.Limits{
			SpliceIdle: -1, // disable idle for this path
			SpliceMax:  -1,
		},
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	relayURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	client := relayhost.New(relayhost.Config{
		URL:       relayURL,
		HostID:    hostID,
		Secret:    secret,
		LocalAddr: ln.Addr().String(),
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- client.Run(ctx) }()

	phone := waitJoinOK(t, relayURL, hostID, 5*time.Second)
	defer phone.Close(websocket.StatusNormalClosure, "")

	payload := []byte("e2e-r20-payload")
	if err := phone.Write(context.Background(), websocket.MessageBinary, payload); err != nil {
		t.Fatal(err)
	}
	rctx, rcancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer rcancel()
	_, data, err := phone.Read(rctx)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("echo %q", data)
	}

	// Unauthorized register must not mint a host session.
	badCtx, badCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer badCancel()
	bad, _, err := websocket.Dial(badCtx, relayURL+"/v1/host", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := relay.NewEnvelope(relay.TypeRegister, "x", relay.RegisterPayload{
		HostID: hostID,
		Secret: "wrong-secret-value!",
	})
	b, _ := json.Marshal(reg)
	if err := bad.Write(badCtx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
	_, data, err = bad.Read(badCtx)
	_ = bad.Close(websocket.StatusNormalClosure, "")
	if err != nil {
		t.Fatal(err)
	}
	var env relay.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	if env.Type != relay.TypeError {
		t.Fatalf("want register error, got %s", env.Type)
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
	}
}

// TestE2ESpliceIdleTimeout verifies R15: silent splices end after SpliceIdle.
func TestE2ESpliceIdleTimeout(t *testing.T) {
	const hostID = "idle-host"
	const secret = "sixteen-chars-idle-1"

	cred, _ := relay.ParseAllowFlag(hostID + ":" + secret)
	srv := relay.New(relay.Config{
		Allow: []relay.HostCredential{cred},
		Limits: relay.Limits{
			TunnelWait:   5 * time.Second,
			RegisterIdle: time.Hour, // don't ping-kill during test
			SpliceIdle:   2 * time.Second,
			SpliceMax:    -1,
		},
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	base := "ws" + strings.TrimPrefix(ts.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	hostConn := registerHost(t, ctx, base, hostID, secret)
	defer hostConn.Close(websocket.StatusNormalClosure, "")
	go hostEchoTunnels(t, ctx, base, hostID, secret, hostConn)

	phone, _, err := websocket.Dial(ctx, base+"/v1/phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close(websocket.StatusNormalClosure, "")
	join, _ := relay.NewEnvelope(relay.TypeJoin, "1", relay.JoinPayload{HostID: hostID})
	writeJSON(t, ctx, phone, join)
	if got := readJSON(t, ctx, phone); got.Type != relay.TypeJoinOK {
		t.Fatalf("join: %+v", got)
	}

	// No traffic → idle watchdog should cancel the splice.
	rctx, rcancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer rcancel()
	_, _, err = phone.Read(rctx)
	if err == nil {
		t.Fatal("expected splice to end on idle timeout")
	}
}

// TestE2EShutdownDrainsSplices verifies R17: cancel Serve closes live splices.
func TestE2EShutdownDrainsSplices(t *testing.T) {
	const hostID = "drain-host"
	const secret = "sixteen-chars-drain1"

	cred, _ := relay.ParseAllowFlag(hostID + ":" + secret)
	srv := relay.New(relay.Config{
		Allow: []relay.HostCredential{cred},
		Limits: relay.Limits{
			SpliceIdle: -1,
			SpliceMax:  -1,
		},
	}, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	serveCtx, serveCancel := context.WithCancel(context.Background())
	defer serveCancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(serveCtx, ln) }()

	base := "ws://" + ln.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hostConn := registerHost(t, ctx, base, hostID, secret)
	defer hostConn.Close(websocket.StatusNormalClosure, "")
	go hostEchoTunnels(t, ctx, base, hostID, secret, hostConn)

	phone, _, err := websocket.Dial(ctx, base+"/v1/phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close(websocket.StatusNormalClosure, "")
	join, _ := relay.NewEnvelope(relay.TypeJoin, "1", relay.JoinPayload{HostID: hostID})
	writeJSON(t, ctx, phone, join)
	if got := readJSON(t, ctx, phone); got.Type != relay.TypeJoinOK {
		t.Fatalf("join: %+v", got)
	}

	// Wait until splice is tracked.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && srv.ActiveSplices() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.ActiveSplices() == 0 {
		t.Fatal("splice not tracked")
	}

	serveCancel()
	// Serve grants http.Shutdown a 5s drain grace (see Server.Serve), so the
	// wait here must exceed it — a 3s budget could fire while the server is
	// still legitimately draining, and did so intermittently on loaded CI.
	select {
	case <-errCh:
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
	// Drain may race with untrack; allow a brief settle.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && srv.ActiveSplices() > 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if n := srv.ActiveSplices(); n != 0 {
		t.Fatalf("active splices after shutdown: %d", n)
	}
}

func registerHost(t *testing.T, ctx context.Context, base, hostID, secret string) *websocket.Conn {
	t.Helper()
	hostConn, _, err := websocket.Dial(ctx, base+"/v1/host", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := relay.NewEnvelope(relay.TypeRegister, "1", relay.RegisterPayload{
		HostID: hostID,
		Secret: secret,
	})
	writeJSON(t, ctx, hostConn, reg)
	if got := readJSON(t, ctx, hostConn); got.Type != relay.TypeRegisterOK {
		t.Fatalf("register: %+v", got)
	}
	return hostConn
}

func hostEchoTunnels(t *testing.T, ctx context.Context, base, hostID, secret string, hostConn *websocket.Conn) {
	t.Helper()
	for {
		_, data, err := hostConn.Read(ctx)
		if err != nil {
			return
		}
		var env relay.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			return
		}
		if env.Type != relay.TypeDial {
			continue
		}
		var sess relay.SessionPayload
		if err := relay.DecodePayload(env, &sess); err != nil {
			return
		}
		tun, _, err := websocket.Dial(ctx, base+"/v1/tunnel", nil)
		if err != nil {
			return
		}
		// R12: claim with dial token; secret only as legacy fallback.
		tp := relay.TunnelPayload{SessionID: sess.SessionID, HostID: hostID, Token: sess.TunnelToken}
		if sess.TunnelToken == "" {
			tp.Secret = secret
		}
		tenv, _ := relay.NewEnvelope(relay.TypeTunnel, "t", tp)
		b, _ := json.Marshal(tenv)
		if err := tun.Write(ctx, websocket.MessageText, b); err != nil {
			_ = tun.Close(websocket.StatusInternalError, "")
			return
		}
		_, tokData, err := tun.Read(ctx)
		if err != nil {
			_ = tun.Close(websocket.StatusInternalError, "")
			return
		}
		var tok relay.Envelope
		if err := json.Unmarshal(tokData, &tok); err != nil || tok.Type != relay.TypeTunnelOK {
			_ = tun.Close(websocket.StatusInternalError, "")
			return
		}
		go func(tun *websocket.Conn) {
			for {
				typ, data, err := tun.Read(ctx)
				if err != nil {
					_ = tun.Close(websocket.StatusNormalClosure, "")
					return
				}
				if err := tun.Write(ctx, typ, data); err != nil {
					return
				}
			}
		}(tun)
	}
}

// TestE2EPhaseESecurity covers automated Phase E exit security cases:
// unauthorized register, join alone does not need secret, unknown host_id
// looks like host_offline (no allowlist enumeration), host drop fails pending.
func TestE2EPhaseESecurity(t *testing.T) {
	const hostID = "sec-host"
	const secret = "sixteen-chars-sec-host1"

	cred, _ := relay.ParseAllowFlag(hostID + ":" + secret)
	srv := relay.New(relay.Config{
		Allow: []relay.HostCredential{cred},
		Limits: relay.Limits{
			SpliceIdle: -1,
			SpliceMax:  -1,
			TunnelWait: 2 * time.Second,
		},
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	base := "ws" + strings.TrimPrefix(ts.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1) Wrong registration secret cannot register.
	bad, _, err := websocket.Dial(ctx, base+"/v1/host", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := relay.NewEnvelope(relay.TypeRegister, "1", relay.RegisterPayload{
		HostID: hostID,
		Secret: "wrong-secret-value!",
	})
	writeJSON(t, ctx, bad, reg)
	if got := readJSON(t, ctx, bad); got.Type != relay.TypeError {
		t.Fatalf("want register error, got %+v", got)
	}
	_ = bad.Close(websocket.StatusNormalClosure, "")

	// 2) Unknown host_id register → same unauthorized (no enumeration).
	unk, _, err := websocket.Dial(ctx, base+"/v1/host", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg2, _ := relay.NewEnvelope(relay.TypeRegister, "2", relay.RegisterPayload{
		HostID: "not-in-allowlist",
		Secret: "sixteen-chars-whatever",
	})
	writeJSON(t, ctx, unk, reg2)
	if got := readJSON(t, ctx, unk); got.Type != relay.TypeError {
		t.Fatalf("want unauthorized for unknown host, got %+v", got)
	}
	_ = unk.Close(websocket.StatusNormalClosure, "")

	// 3) Join when offline → host_offline (not "unknown_host").
	phone, _, err := websocket.Dial(ctx, base+"/v1/phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	join, _ := relay.NewEnvelope(relay.TypeJoin, "j", relay.JoinPayload{HostID: hostID})
	writeJSON(t, ctx, phone, join)
	got := readJSON(t, ctx, phone)
	if got.Type != relay.TypeError {
		t.Fatalf("want join error offline, got %+v", got)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), "host_offline") {
		t.Fatalf("want host_offline, got %s", b)
	}
	_ = phone.Close(websocket.StatusNormalClosure, "")

	// 4) Host unregister fails in-flight join (S12).
	hostConn := registerHost(t, ctx, base, hostID, secret)
	phone2, _, err := websocket.Dial(ctx, base+"/v1/phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer phone2.Close(websocket.StatusNormalClosure, "")
	join2, _ := relay.NewEnvelope(relay.TypeJoin, "j2", relay.JoinPayload{HostID: hostID})
	writeJSON(t, ctx, phone2, join2)
	// Drop host before tunnel.
	_ = hostConn.Close(websocket.StatusGoingAway, "revoke")
	// Phone should get error or closed connection (not a successful splice).
	rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rcancel()
	_, data, err := phone2.Read(rctx)
	if err == nil {
		var env relay.Envelope
		_ = json.Unmarshal(data, &env)
		if env.Type == relay.TypeJoinOK {
			t.Fatal("join_ok after host revoke — path should fail")
		}
	}

	// 5) Healthz is liveness-only (R11).
	res, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("healthz: %s", body)
	}
	if strings.Contains(string(body), "host") || strings.Contains(string(body), "allow") {
		t.Fatalf("healthz leaks: %s", body)
	}
}

// TestE2ETunnelRejectsWrongToken verifies R12: tunnel claim needs dial token.
func TestE2ETunnelRejectsWrongToken(t *testing.T) {
	const hostID = "tok-host"
	const secret = "sixteen-chars-tok-host1"
	cred, _ := relay.ParseAllowFlag(hostID + ":" + secret)
	srv := relay.New(relay.Config{
		Allow:  []relay.HostCredential{cred},
		Limits: relay.Limits{SpliceIdle: -1, SpliceMax: -1, TunnelWait: 3 * time.Second},
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	base := "ws" + strings.TrimPrefix(ts.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hostConn := registerHost(t, ctx, base, hostID, secret)
	defer hostConn.Close(websocket.StatusNormalClosure, "")

	// Phone join so a pending exists; capture dial token then reject wrong claim.
	go func() {
		phone, _, err := websocket.Dial(ctx, base+"/v1/phone", nil)
		if err != nil {
			return
		}
		defer phone.Close(websocket.StatusNormalClosure, "")
		join, _ := relay.NewEnvelope(relay.TypeJoin, "1", relay.JoinPayload{HostID: hostID})
		writeJSON(t, ctx, phone, join)
		_, _, _ = phone.Read(ctx)
	}()

	// Wait for dial on host control.
	var sess relay.SessionPayload
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rctx, rcancel := context.WithTimeout(ctx, 500*time.Millisecond)
		_, data, err := hostConn.Read(rctx)
		rcancel()
		if err != nil {
			continue
		}
		var env relay.Envelope
		if json.Unmarshal(data, &env) != nil || env.Type != relay.TypeDial {
			continue
		}
		_ = relay.DecodePayload(env, &sess)
		break
	}
	if sess.SessionID == "" || sess.TunnelToken == "" {
		t.Fatal("no dial with tunnel_token")
	}

	tun, _, err := websocket.Dial(ctx, base+"/v1/tunnel", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close(websocket.StatusNormalClosure, "")
	tenv, _ := relay.NewEnvelope(relay.TypeTunnel, "t", relay.TunnelPayload{
		SessionID: sess.SessionID,
		HostID:    hostID,
		Token:     "definitely-wrong-token-value-xxxxxxxx",
	})
	writeJSON(t, ctx, tun, tenv)
	got := readJSON(t, ctx, tun)
	if got.Type != relay.TypeError {
		t.Fatalf("want tunnel error, got %+v", got)
	}
}

func waitJoinOK(t *testing.T, relayURL, hostID string, timeout time.Duration) *websocket.Conn {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pctx, pcancel := context.WithTimeout(context.Background(), time.Second)
		phone, _, err := websocket.Dial(pctx, relayURL+"/v1/phone", nil)
		if err != nil {
			pcancel()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		join, _ := relay.NewEnvelope(relay.TypeJoin, "1", relay.JoinPayload{HostID: hostID})
		b, _ := json.Marshal(join)
		if err := phone.Write(pctx, websocket.MessageText, b); err != nil {
			_ = phone.Close(websocket.StatusNormalClosure, "")
			pcancel()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		_, data, err := phone.Read(pctx)
		pcancel()
		if err != nil {
			_ = phone.Close(websocket.StatusNormalClosure, "")
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var env relay.Envelope
		if err := json.Unmarshal(data, &env); err != nil || env.Type != relay.TypeJoinOK {
			_ = phone.Close(websocket.StatusNormalClosure, "")
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return phone
	}
	t.Fatal("phone never got join_ok")
	return nil
}
