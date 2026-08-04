package relay

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// MADR 0068 P1 — a peer that upgrades and then goes silent (an iOS phone
// suspending mid-join) is reaped by the first-envelope deadline instead of
// holding a goroutine forever (A1 Go finding 12).
func TestFirstEnvelopeDeadlineReapsSilentUpgrade(t *testing.T) {
	old := firstEnvelopeTimeout
	firstEnvelopeTimeout = 300 * time.Millisecond
	t.Cleanup(func() { firstEnvelopeTimeout = old })

	srv := New(Config{
		ListenAddr: "127.0.0.1:0",
		Limits:     DefaultLimits(),
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/v1/phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send nothing. The server must close us on its own initiative.
	start := time.Now()
	_, _, rerr := conn.Read(ctx)
	if rerr == nil {
		t.Fatal("silent upgrade was not reaped")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("reap took %v, want ≈300ms deadline", elapsed)
	}
}
