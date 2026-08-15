package relay_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/relay"
	"github.com/maccavelli/magic-cli-remote/internal/relayhost"
)

//nolint:gosec // live D4 probe against a throwaway self-signed listener
var insecureTLS = &tls.Config{InsecureSkipVerify: true}

// TestD4LiveSandbox is the 0091 D4 probe: register + join + splice against a
// live mcrelay started under RestrictAddressFamilies + MemoryDenyWriteExecute.
// Skipped unless MCRELAY_D4_URL is set (e.g. wss://127.0.0.1:18443).
func TestD4LiveSandbox(t *testing.T) {
	base := os.Getenv("MCRELAY_D4_URL")
	if base == "" {
		t.Skip("set MCRELAY_D4_URL to run 0091 D4 live probe")
	}
	hostID := getenv("MCRELAY_D4_HOST", "d4-host")
	secret := getenv("MCRELAY_D4_SECRET", "sixteen-chars-d4probe")

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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := relayhost.New(relayhost.Config{
		URL:                base,
		HostID:             hostID,
		Secret:             secret,
		LocalAddr:          ln.Addr().String(),
		InsecureSkipVerify: true,
	}, nil)
	errCh := make(chan error, 1)
	go func() { errCh <- client.Run(ctx) }()

	phone := waitJoinOKInsecure(t, base, hostID, 8*time.Second)
	defer phone.Close(websocket.StatusNormalClosure, "")

	payload := []byte("d4-probe-payload")
	if err := phone.Write(ctx, websocket.MessageBinary, payload); err != nil {
		t.Fatal(err)
	}
	_, data, err := phone.Read(ctx)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("echo %q", data)
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func waitJoinOKInsecure(t *testing.T, base, hostID string, timeout time.Duration) *websocket.Conn {
	t.Helper()
	hc := &http.Client{Transport: &http.Transport{
		TLSClientConfig: insecureTLS,
	}}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, _, err := websocket.Dial(ctx, base+"/v1/phone", &websocket.DialOptions{HTTPClient: hc})
		if err != nil {
			cancel()
			time.Sleep(80 * time.Millisecond)
			continue
		}
		env, err := relay.NewEnvelope(relay.TypeJoin, "j", relay.JoinPayload{HostID: hostID})
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		b, err := json.Marshal(env)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if err := c.Write(ctx, websocket.MessageText, b); err != nil {
			_ = c.CloseNow()
			cancel()
			time.Sleep(80 * time.Millisecond)
			continue
		}
		_, data, err := c.Read(ctx)
		cancel()
		if err != nil {
			_ = c.CloseNow()
			time.Sleep(80 * time.Millisecond)
			continue
		}
		var got relay.Envelope
		if err := json.Unmarshal(data, &got); err != nil {
			_ = c.CloseNow()
			continue
		}
		if got.Type == relay.TypeJoinOK {
			return c
		}
		_ = c.CloseNow()
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatalf("join not ok within %s", timeout)
	return nil
}
