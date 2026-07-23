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
	select {
	case <-errCh:
	case <-time.After(3 * time.Second):
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
		tenv, _ := relay.NewEnvelope(relay.TypeTunnel, "t", relay.TunnelPayload{
			SessionID: sess.SessionID,
			HostID:    hostID,
			Secret:    secret,
		})
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
