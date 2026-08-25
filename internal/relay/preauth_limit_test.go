package relay_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/relay"
)

// 0115 P3 (F2): the first, unauthenticated envelope on every plane is read
// under ControlReadLimitBytes (64 KiB), not the splice MaxMessageBytes.
// Post-auth traffic keeps the full splice limit.

func preAuthServer(t *testing.T) (base string, srv *relay.Server, done func()) {
	t.Helper()
	cred, err := relay.ParseAllowFlag("devbox-1:sixteen-chars-min-secret")
	if err != nil {
		t.Fatal(err)
	}
	s := relay.New(relay.Config{
		ListenAddr: "127.0.0.1:0",
		Allow:      []relay.HostCredential{cred},
		Limits:     relay.DefaultLimits(),
	}, nil)
	ts := httptest.NewServer(s.Handler())
	return "ws" + strings.TrimPrefix(ts.URL, "http"), s, ts.Close
}

// oversizedFirstFrame dials path and sends a first frame that is a VALID
// envelope of the given type, padded past the control limit. Validity is the
// discriminator: under the old 1 MiB pre-auth limit the server would parse it
// and answer (host_offline / unknown_session); under the 64 KiB control limit
// it must kill the connection with StatusMessageTooBig before answering.
func oversizedFirstFrame(t *testing.T, path, msgType string, payload map[string]any) {
	t.Helper()
	base, _, done := preAuthServer(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	payload["pad"] = strings.Repeat("a", relay.ControlReadLimitBytes)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	env := relay.Envelope{V: relay.Version, Type: msgType, ID: "big", Payload: body}
	frame, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) <= relay.ControlReadLimitBytes {
		t.Fatalf("frame %d bytes; must exceed the control limit", len(frame))
	}
	if err := conn.Write(ctx, websocket.MessageText, frame); err != nil {
		// A server that already hung up mid-write is also a pass.
		return
	}
	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Fatalf("%s answered a %d-byte pre-auth frame; want the connection killed", path, len(frame))
	}
	if got := websocket.CloseStatus(err); got != websocket.StatusMessageTooBig {
		t.Fatalf("%s close status = %v (%v), want StatusMessageTooBig", path, got, err)
	}
}

func TestPreAuthFrameCapPhone(t *testing.T) {
	oversizedFirstFrame(t, "/v1/phone", relay.TypeJoin, map[string]any{"host_id": "devbox-1"})
}

func TestPreAuthFrameCapTunnel(t *testing.T) {
	oversizedFirstFrame(t, "/v1/tunnel", relay.TypeTunnel, map[string]any{
		"session_id": "s1", "host_id": "devbox-1", "token": "tt",
	})
}

// TestPostAuthSpliceKeepsFullLimit guards against capping the wrong side:
// after a real join, a 200 KiB frame (over the 64 KiB control limit, under
// the 1 MiB splice limit) must still splice through and echo back.
func TestPostAuthSpliceKeepsFullLimit(t *testing.T) {
	const hostID = "devbox-1"
	const secret = "sixteen-chars-min-secret"
	base, _, done := preAuthServer(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	hostConn := registerHost(ctx, t, base, hostID, secret)
	defer hostConn.Close(websocket.StatusNormalClosure, "")
	go hostEchoTunnelsWithLimit(ctx, t, base, hostID, secret, hostConn)

	phone, _, err := websocket.Dial(ctx, base+"/v1/phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close(websocket.StatusNormalClosure, "")
	phone.SetReadLimit(int64(relay.DefaultLimits().MaxMessageBytes) + 1024)

	join, _ := relay.NewEnvelope(relay.TypeJoin, "j1", relay.JoinPayload{HostID: hostID})
	writeJSON(ctx, t, phone, join)
	if got := readJSON(ctx, t, phone); got.Type != relay.TypeJoinOK {
		t.Fatalf("join: %+v", got)
	}

	payload := bytes.Repeat([]byte("b"), 200<<10)
	if err := phone.Write(ctx, websocket.MessageBinary, payload); err != nil {
		t.Fatalf("post-auth write: %v", err)
	}
	typ, echo, err := phone.Read(ctx)
	if err != nil {
		t.Fatalf("post-auth echo read: %v", err)
	}
	if typ != websocket.MessageBinary || !bytes.Equal(echo, payload) {
		t.Fatalf("echo mismatch: type=%v len=%d", typ, len(echo))
	}
}

// hostEchoTunnelsWithLimit is hostEchoTunnels with the tunnel read limit
// raised to the splice maximum, so oversized-but-legal frames echo through.
func hostEchoTunnelsWithLimit(ctx context.Context, t *testing.T, base, hostID, secret string, hostConn *websocket.Conn) {
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
		tun.SetReadLimit(int64(relay.DefaultLimits().MaxMessageBytes) + 1024)
		tenv, _ := relay.NewEnvelope(relay.TypeTunnel, "t", relay.TunnelPayload{
			SessionID: sess.SessionID, HostID: hostID, Token: sess.TunnelToken,
		})
		b, _ := json.Marshal(tenv)
		if err := tun.Write(ctx, websocket.MessageText, b); err != nil {
			_ = tun.Close(websocket.StatusInternalError, "")
			return
		}
		if _, _, err := tun.Read(ctx); err != nil {
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
