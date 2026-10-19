package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestWriteControlHasADeadline is MADR 0201 D7 (0073 F-T7). writeControl holds
// the host slot's writeMu while it writes the dial envelope; with no deadline,
// one stalled control connection blocked every join for that host. Here the
// host never reads and its connection is wedged; writeControl must return
// within the bound, and so must a second join behind it.
func TestWriteControlHasADeadline(t *testing.T) {
	prev := controlWriteTimeout
	controlWriteTimeout = 100 * time.Millisecond
	t.Cleanup(func() { controlWriteTimeout = prev })

	// A host end that accepts and never reads.
	hold := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		<-hold
		c.CloseNow()
	}))
	defer ts.Close()
	defer close(hold)

	ctx := context.Background()
	control, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer control.CloseNow()

	// Wedge the connection: a writer keeps sending to a peer that never reads,
	// until the transport's buffers are full and it blocks holding the
	// connection's own write lock.
	go func() {
		big := make([]byte, 1<<20)
		for {
			if err := control.Write(context.Background(), websocket.MessageBinary, big); err != nil {
				return
			}
		}
	}()
	time.Sleep(300 * time.Millisecond)

	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	if err := h.register("h1", control, func() {}); err != nil {
		t.Fatal(err)
	}
	dial, _ := NewEnvelope(TypeDial, "", SessionPayload{SessionID: "s1", HostID: "h1"})

	results := make(chan time.Duration, 2)
	for range 2 {
		go func() {
			start := time.Now()
			_ = h.writeControl(context.Background(), "h1", dial)
			results <- time.Since(start)
		}()
	}
	for i := range 2 {
		select {
		case d := <-results:
			if d > time.Second {
				t.Fatalf("join %d waited %v on a wedged control connection", i+1, d)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("join %d never returned: writeControl has no deadline", i+1)
		}
	}
}
