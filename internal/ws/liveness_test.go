package ws_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// MADR 0068 P1 (U6, in-CI half) — transport pongs extend the v2 read
// horizon; v1 semantics are untouched. The kernel-keepalive layer cannot be
// exercised without a blackholed peer and is validated by the config
// mapping test plus hardware Part F.

func newLivenessServer(t *testing.T, deadline time.Duration) (string, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}
	srv := ws.New(ws.Options{
		Store:              store,
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
		ReadDeadline:       deadline,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return "ws" + ts.URL[len("http"):] + "/v1/ws", token
}

// authAs dials and authenticates, offering the given protocols (nil = v1).
func authAs(ctx context.Context, t *testing.T, url, token string, protocols []int) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{
		Token:     token,
		Protocols: protocols,
	})
	writeEnv(ctx, t, conn, env)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("want auth_ok got %s %s", got.Type, string(got.Payload))
	}
	return conn
}

// startReader pumps frames on a background context — a read context with a
// deadline would close the connection at expiry (coder/websocket semantics),
// which is exactly the failure mode these tests must not self-inflict. The
// blocked Read also answers the server's transport pings.
func startReader(conn *websocket.Conn) (<-chan []byte, <-chan error) {
	frames := make(chan []byte, 8)
	errCh := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				errCh <- err
				return
			}
			frames <- data
		}
	}()
	return frames, errCh
}

func TestV2QuietConnectionSurvivesOnPongs(t *testing.T) {
	url, token := newLivenessServer(t, 1500*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := authAs(ctx, t, url, token, []int{1, 2})
	defer conn.Close(websocket.StatusNormalClosure, "")
	frames, errCh := startReader(conn)

	// 4 s ≈ 2.7× the deadline: only pong-extension can carry a connection
	// that sends no data frames this far.
	select {
	case err := <-errCh:
		t.Fatalf("v2 quiet connection died before the window: %v", err)
	case <-time.After(4 * time.Second):
	}

	// And it is genuinely usable afterwards.
	ping, _ := protocol.NewEnvelope(protocol.TypePing, "p1", nil)
	writeEnv(ctx, t, conn, ping)
	select {
	case data := <-frames:
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatal(err)
		}
		if env.Type != protocol.TypePong {
			t.Fatalf("post-window ping: got %s", env.Type)
		}
	case err := <-errCh:
		t.Fatalf("post-window ping: connection error %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("post-window ping: no pong")
	}
}

func TestV1QuietConnectionIsReapedAtDeadline(t *testing.T) {
	url, token := newLivenessServer(t, 1500*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := authAs(ctx, t, url, token, nil)
	defer conn.Close(websocket.StatusNormalClosure, "")
	_, errCh := startReader(conn)

	start := time.Now()
	select {
	case <-errCh:
		if elapsed := time.Since(start); elapsed < time.Second || elapsed > 4*time.Second {
			t.Fatalf("v1 reap at %v, want ≈1.5s deadline", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("v1 quiet connection survived past the read deadline")
	}
}
