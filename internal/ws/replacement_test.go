package ws_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// MADR 0068 P2 (U3) — one live socket per device: a successful auth closes
// the device's older authenticated connections with CloseReplaced, so a
// reconnect-heavy client's own zombies can never exhaust the pool (A1 F11).

func newReplacementServer(t *testing.T, maxClients int) (string, string) {
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
		MaxClients:         maxClients,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return "ws" + ts.URL[len("http"):] + "/v1/ws", token
}

func authConn(ctx context.Context, t *testing.T, url, token, id string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", id, err)
	}
	env, _ := protocol.NewEnvelope(protocol.TypeAuth, id, protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, env)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("auth %s: want auth_ok got %s %s", id, got.Type, string(got.Payload))
	}
	return conn
}

func TestSecondAuthReplacesElderWith4001(t *testing.T) {
	url, token := newReplacementServer(t, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	elder := authConn(ctx, t, url, token, "1")
	defer elder.Close(websocket.StatusNormalClosure, "")
	newcomer := authConn(ctx, t, url, token, "2")
	defer newcomer.Close(websocket.StatusNormalClosure, "")

	// The elder is closed by the daemon with the typed code.
	_, _, err := elder.Read(ctx)
	if err == nil {
		t.Fatal("elder survived a newer auth of the same device")
	}
	if status := websocket.CloseStatus(err); status != protocol.CloseReplaced {
		t.Fatalf("elder close status = %d (%v), want %d", status, err, protocol.CloseReplaced)
	}

	// The newcomer is unaffected and live.
	ping, _ := protocol.NewEnvelope(protocol.TypePing, "p1", nil)
	writeEnv(ctx, t, newcomer, ping)
	if got := readEnv(ctx, t, newcomer); got.Type != protocol.TypePong {
		t.Fatalf("newcomer after replacement: got %s", got.Type)
	}
}

// The A1 self-eviction scenario, inverted: with a pool smaller than the
// reconnect count, sequential re-auths of one device always succeed because
// each newcomer frees its elder's slot.
func TestReconnectChurnCannotExhaustThePool(t *testing.T) {
	url, token := newReplacementServer(t, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var conns []*websocket.Conn
	t.Cleanup(func() {
		for _, c := range conns {
			_ = c.Close(websocket.StatusNormalClosure, "")
		}
	})
	// 6 cycles against a pool of 3, never closing client-side — the shape
	// of an iOS phone flapping while its old sockets are half-open. Without
	// replacement the 4th dial is refused `too many clients`. No retries
	// needed: replaceElders frees the elder's slot under the capacity
	// check's own lock, before auth_ok is even written.
	for i := 0; i < 6; i++ {
		conn, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			t.Fatalf("dial %d refused: %v", i, err)
		}
		conns = append(conns, conn)
		env, _ := protocol.NewEnvelope(protocol.TypeAuth, "a", protocol.AuthPayload{Token: token})
		writeEnv(ctx, t, conn, env)
		if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
			t.Fatalf("cycle %d: want auth_ok got %s %s", i, got.Type, string(got.Payload))
		}
	}
}

// Dev mode shares one identity across every client; replacement must not
// kick them all on each other's auth.
func TestNoReplacementWhenDeviceTokensOff(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := ws.New(ws.Options{
		Store:              store,
		RequireDeviceToken: false,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	url := "ws" + ts.URL[len("http"):] + "/v1/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := authConn(ctx, t, url, "", "1")
	defer a.Close(websocket.StatusNormalClosure, "")
	b := authConn(ctx, t, url, "", "2")
	defer b.Close(websocket.StatusNormalClosure, "")

	// Both stay live.
	ping, _ := protocol.NewEnvelope(protocol.TypePing, "p1", nil)
	writeEnv(ctx, t, a, ping)
	if got := readEnv(ctx, t, a); got.Type != protocol.TypePong {
		t.Fatalf("dev-mode client a after b's auth: got %s", got.Type)
	}
}
