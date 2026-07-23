package relayhost_test

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

func TestHostRegistersAndBridgesTunnel(t *testing.T) {
	const hostID = "devbox-e2"
	const secret = "sixteen-chars-min-e2!!"

	// Local TCP echo — stands in for mcremote's TLS listener.
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
		Allow:  []relay.HostCredential{cred},
		Limits: relay.DefaultLimits(),
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

	// Wait until registered (poll join until host is online).
	deadline := time.Now().Add(5 * time.Second)
	var phone *websocket.Conn
	for time.Now().Before(deadline) {
		pctx, pcancel := context.WithTimeout(context.Background(), time.Second)
		phoneWS := relayURL + "/v1/phone"
		var dialErr error
		phone, _, dialErr = websocket.Dial(pctx, phoneWS, nil)
		if dialErr != nil {
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
		break
	}
	if phone == nil {
		t.Fatal("phone never got join_ok")
	}
	defer phone.Close(websocket.StatusNormalClosure, "")

	// After join_ok, opaque frames are bridged to local TCP echo.
	// Host writes binary WS frames from TCP; phone may send binary.
	payload := []byte("hello-e2-tunnel")
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
		t.Fatalf("echo got %q", data)
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
	}
}
