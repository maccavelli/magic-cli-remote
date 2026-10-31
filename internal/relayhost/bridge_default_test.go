package relayhost

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/relay"
)

// MADR 0223 D4: with relay.max_frame_bytes 0, the bridge uses the relay's own
// default, so the two cannot drift apart.

func TestBridgeDefaultIsRelayDefault(t *testing.T) {
	if got, want := int64(bridgeWSReadLimit), int64(relay.DefaultLimits().MaxMessageBytes); got != want {
		t.Fatalf("bridge default %d, relay default %d: they must be the same value", got, want)
	}
}

// TestBridgeCarriesTwoMiBByDefault is TestBridgeFrameLimitFollowsConfig's
// default case with a frame over the old 1 MiB default.
func TestBridgeCarriesTwoMiBByDefault(t *testing.T) {
	payload := make([]byte, 2<<20)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		if err := c.Write(r.Context(), websocket.MessageBinary, payload); err != nil {
			return
		}
		answerClose(r.Context(), c)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), hangGuard)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	tcpA, tcpB := net.Pipe()
	defer tcpA.Close()
	total := make(chan int, 1)
	go func() {
		n64, _ := io.Copy(io.Discard, &countReader{r: tcpB, want: len(payload)})
		total <- int(n64)
		_ = tcpB.Close()
	}()
	bridge(ctx, conn, tcpA, bridgeWSReadLimit, slog.Default())
	if n := <-total; n != len(payload) {
		t.Fatalf("default limit: %d bytes crossed, want %d intact", n, len(payload))
	}
}
