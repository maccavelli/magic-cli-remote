package relay_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/relay"
)

// MADR 0223 D3: mcrelay's default message limit is acp-go-sdk2's 10 MiB.

func TestDefaultMaxMessageBytesIsTenMiB(t *testing.T) {
	if got, want := relay.DefaultLimits().MaxMessageBytes, 10<<20; got != want {
		t.Fatalf("DefaultLimits().MaxMessageBytes = %d, want %d", got, want)
	}
}

// TestSpliceCarriesTwoMiBUnderDefaults is TestPostAuthSpliceKeepsFullLimit
// with a message over the old 1 MiB default: under DefaultLimits it splices
// phone to host and echoes back intact.
func TestSpliceCarriesTwoMiBUnderDefaults(t *testing.T) {
	const hostID = "devbox-1"
	const secret = "sixteen-chars-min-secret"
	base, _, done := preAuthServer(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	payload := bytes.Repeat([]byte("b"), 2<<20)
	if err := phone.Write(ctx, websocket.MessageBinary, payload); err != nil {
		t.Fatalf("2 MiB write: %v", err)
	}
	typ, echo, err := phone.Read(ctx)
	if err != nil {
		t.Fatalf("2 MiB echo read: %v", err)
	}
	if typ != websocket.MessageBinary || !bytes.Equal(echo, payload) {
		t.Fatalf("echo mismatch: type=%v len=%d, want %d", typ, len(echo), len(payload))
	}
}
