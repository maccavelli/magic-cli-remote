package relay

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// BenchmarkSpliceRoundTrip measures opaque splice throughput with the E2
// Reader/Writer + pooled CopyBuffer path (MADR 0017).
func BenchmarkSpliceRoundTrip(b *testing.B) {
	const hostID = "bench-host"
	const secret = "sixteen-chars-bench-1"
	cred, err := ParseAllowFlag(hostID + ":" + secret)
	if err != nil {
		b.Fatal(err)
	}
	srv := New(Config{
		Allow: []HostCredential{cred},
		Limits: Limits{
			SpliceIdle: -1,
			SpliceMax:  -1,
			TunnelWait: 5 * time.Second,
		},
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	base := "ws" + strings.TrimPrefix(ts.URL, "http")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hostConn, _, err := websocket.Dial(ctx, base+"/v1/host", nil)
	if err != nil {
		b.Fatal(err)
	}
	defer hostConn.Close(websocket.StatusNormalClosure, "")
	reg, _ := NewEnvelope(TypeRegister, "1", RegisterPayload{HostID: hostID, Secret: secret})
	if err := WriteEnvelope(ctx, hostConn, reg); err != nil {
		b.Fatal(err)
	}
	if env, err := ReadEnvelope(ctx, hostConn); err != nil || env.Type != TypeRegisterOK {
		b.Fatalf("register: %v %+v", err, env)
	}

	// Host dial handler: open tunnel and echo.
	go func() {
		for {
			env, err := ReadEnvelope(ctx, hostConn)
			if err != nil {
				return
			}
			if env.Type != TypeDial {
				continue
			}
			var sess SessionPayload
			if err := DecodePayload(env, &sess); err != nil {
				return
			}
			tun, _, err := websocket.Dial(ctx, base+"/v1/tunnel", nil)
			if err != nil {
				return
			}
			tenv, _ := NewEnvelope(TypeTunnel, "t", TunnelPayload{
				SessionID: sess.SessionID,
				HostID:    hostID,
				Token:     sess.TunnelToken,
			})
			if err := WriteEnvelope(ctx, tun, tenv); err != nil {
				_ = tun.Close(websocket.StatusInternalError, "")
				return
			}
			if resp, err := ReadEnvelope(ctx, tun); err != nil || resp.Type != TypeTunnelOK {
				_ = tun.Close(websocket.StatusInternalError, "")
				return
			}
			go func(tun *websocket.Conn) {
				for {
					typ, r, err := tun.Reader(ctx)
					if err != nil {
						_ = tun.Close(websocket.StatusNormalClosure, "")
						return
					}
					w, err := tun.Writer(ctx, typ)
					if err != nil {
						_, _ = io.Copy(io.Discard, r)
						return
					}
					_, _ = io.Copy(w, r)
					_ = w.Close()
				}
			}(tun)
		}
	}()

	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		phone, _, err := websocket.Dial(ctx, base+"/v1/phone", nil)
		if err != nil {
			b.Fatal(err)
		}
		join, _ := NewEnvelope(TypeJoin, "j", JoinPayload{HostID: hostID})
		if err := WriteEnvelope(ctx, phone, join); err != nil {
			b.Fatal(err)
		}
		if env, err := ReadEnvelope(ctx, phone); err != nil || env.Type != TypeJoinOK {
			b.Fatalf("join: %v %+v", err, env)
		}
		if err := phone.Write(ctx, websocket.MessageBinary, payload); err != nil {
			b.Fatal(err)
		}
		_, data, err := phone.Read(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if len(data) != len(payload) {
			b.Fatalf("len %d", len(data))
		}
		_ = phone.Close(websocket.StatusNormalClosure, "")
	}
}
