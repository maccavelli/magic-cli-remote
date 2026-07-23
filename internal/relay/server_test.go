package relay_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/relay"
)

func TestRelayRegisterJoinSplice(t *testing.T) {
	const hostID = "devbox-1"
	const secret = "sixteen-chars-min-secret"
	cred, err := relay.ParseAllowFlag(hostID + ":" + secret)
	if err != nil {
		t.Fatal(err)
	}
	srv := relay.New(relay.Config{
		ListenAddr: "127.0.0.1:0",
		Allow:      []relay.HostCredential{cred},
		Limits:     relay.DefaultLimits(),
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Host control
	hostWS := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/host"
	hostConn, _, err := websocket.Dial(ctx, hostWS, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer hostConn.Close(websocket.StatusNormalClosure, "")

	reg, _ := relay.NewEnvelope(relay.TypeRegister, "1", relay.RegisterPayload{
		HostID: hostID,
		Secret: secret,
	})
	writeJSON(t, ctx, hostConn, reg)
	got := readJSON(t, ctx, hostConn)
	if got.Type != relay.TypeRegisterOK {
		t.Fatalf("register: %+v", got)
	}

	// Host listens for dial and opens tunnel
	tunnelDone := make(chan struct{})
	go func() {
		defer close(tunnelDone)
		for {
			env := readJSON(t, ctx, hostConn)
			if env.Type != relay.TypeDial {
				continue
			}
			var sess relay.SessionPayload
			if err := relay.DecodePayload(env, &sess); err != nil {
				t.Errorf("dial payload: %v", err)
				return
			}
			if sess.TunnelToken == "" {
				t.Error("dial missing tunnel_token (R12)")
			}
			tunURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/tunnel"
			tun, _, err := websocket.Dial(ctx, tunURL, nil)
			if err != nil {
				t.Errorf("tunnel dial: %v", err)
				return
			}
			tenv, _ := relay.NewEnvelope(relay.TypeTunnel, "t1", relay.TunnelPayload{
				SessionID: sess.SessionID,
				HostID:    hostID,
				Token:     sess.TunnelToken,
			})
			writeJSON(t, ctx, tun, tenv)
			tok := readJSON(t, ctx, tun)
			if tok.Type != relay.TypeTunnelOK {
				t.Errorf("tunnel_ok: %+v", tok)
				_ = tun.Close(websocket.StatusInternalError, "")
				return
			}
			// Echo splice: host reflects phone frames.
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
		}
	}()

	// Phone join
	phoneWS := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/phone"
	phone, _, err := websocket.Dial(ctx, phoneWS, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close(websocket.StatusNormalClosure, "")

	join, _ := relay.NewEnvelope(relay.TypeJoin, "2", relay.JoinPayload{HostID: hostID})
	writeJSON(t, ctx, phone, join)
	jok := readJSON(t, ctx, phone)
	if jok.Type != relay.TypeJoinOK {
		t.Fatalf("join: %+v payload=%s", jok, string(jok.Payload))
	}

	// Opaque payload through splice
	payload := []byte(`{"v":1,"type":"ping","id":"p"}`)
	if err := phone.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatal(err)
	}
	_, data, err := phone.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("echo got %q", data)
	}

	_ = phone.Close(websocket.StatusNormalClosure, "")
	select {
	case <-tunnelDone:
	case <-time.After(2 * time.Second):
		// host goroutine may still be in control read; ok
	}
}

func TestRelayUnauthorizedRegister(t *testing.T) {
	cred, _ := relay.ParseAllowFlag("h1:sixteen-chars-min-secret")
	srv := relay.New(relay.Config{
		Allow:  []relay.HostCredential{cred},
		Limits: relay.DefaultLimits(),
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hostWS := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/host"
	conn, _, err := websocket.Dial(ctx, hostWS, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	reg, _ := relay.NewEnvelope(relay.TypeRegister, "1", relay.RegisterPayload{
		HostID: "h1",
		Secret: "wrong-secret-value!!",
	})
	writeJSON(t, ctx, conn, reg)
	got := readJSON(t, ctx, conn)
	if got.Type != relay.TypeError {
		t.Fatalf("want error got %s", got.Type)
	}
}

func TestRelayJoinHostOffline(t *testing.T) {
	cred, _ := relay.ParseAllowFlag("h1:sixteen-chars-min-secret")
	srv := relay.New(relay.Config{
		Allow:  []relay.HostCredential{cred},
		Limits: relay.DefaultLimits(),
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	phoneWS := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/phone"
	phone, _, err := websocket.Dial(ctx, phoneWS, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close(websocket.StatusNormalClosure, "")
	join, _ := relay.NewEnvelope(relay.TypeJoin, "1", relay.JoinPayload{HostID: "h1"})
	writeJSON(t, ctx, phone, join)
	got := readJSON(t, ctx, phone)
	if got.Type != relay.TypeError {
		t.Fatalf("want error got %s", got.Type)
	}
}

func TestRelayJoinRejectsInvalidHostID(t *testing.T) {
	// MADR 0017 R29: oversized / invalid host_id never reaches rate-map keys.
	cred, _ := relay.ParseAllowFlag("h1:sixteen-chars-min-secret")
	srv := relay.New(relay.Config{
		Allow:  []relay.HostCredential{cred},
		Limits: relay.DefaultLimits(),
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	phoneWS := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/phone"
	phone, _, err := websocket.Dial(ctx, phoneWS, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close(websocket.StatusNormalClosure, "")
	join, _ := relay.NewEnvelope(relay.TypeJoin, "1", relay.JoinPayload{
		HostID: strings.Repeat("a", 200),
	})
	writeJSON(t, ctx, phone, join)
	got := readJSON(t, ctx, phone)
	if got.Type != relay.TypeError {
		t.Fatalf("want error got %s", got.Type)
	}
	var ep relay.ErrorPayload
	_ = relay.DecodePayload(got, &ep)
	if ep.Code != "bad_payload" {
		t.Fatalf("code=%s want bad_payload", ep.Code)
	}
}

func writeJSON(t *testing.T, ctx context.Context, c *websocket.Conn, env relay.Envelope) {
	t.Helper()
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, ctx context.Context, c *websocket.Conn) relay.Envelope {
	t.Helper()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var env relay.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return env
}
