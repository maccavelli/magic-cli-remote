package relayhost

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/relay"
)

// 0115 P6 in-package tests: registerTimeout and bridge read limits are
// unexported seams.

// TestRegisterExchangeDeadline (F5): a relay that accepts the WebSocket
// upgrade and then never answers register must not wedge the reconnect loop.
// Pre-fix, session read on the undeadlined root ctx and hung until kernel
// keepalive noticed a *dead* peer — a live-but-stalled one hung forever.
func TestRegisterExchangeDeadline(t *testing.T) {
	stall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// Read the register but never reply.
		_, _, _ = c.Read(r.Context())
		<-r.Context().Done()
	}))
	defer stall.Close()

	old := registerTimeout
	registerTimeout = 500 * time.Millisecond
	defer func() { registerTimeout = old }()

	c := New(Config{
		URL:    stall.URL,
		HostID: "h1",
		Secret: "0123456789abcdef",
	}, nil)
	backoff := time.Second
	start := time.Now()
	err := c.session(context.Background(), &backoff)
	if err == nil {
		t.Fatal("session must fail against a stalled relay")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("session took %v; the register exchange is not bounded", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) && websocket.CloseStatus(err) == -1 && !errors.Is(err, io.EOF) {
		t.Logf("err=%v (accepted: any bounded failure)", err)
	}
}

// TestBridgeFrameLimitFollowsConfig (F6): the bridge read limit is the
// configured value, not a hardcoded copy of the relay default.
func TestBridgeFrameLimitFollowsConfig(t *testing.T) {
	payload := make([]byte, 5<<10) // 5 KiB
	mkServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			if err := c.Write(r.Context(), websocket.MessageBinary, payload); err != nil {
				return
			}
			<-r.Context().Done()
		}))
	}

	// run bridges one frame under the given limit and returns how many bytes
	// reached the TCP side in total. Streaming semantics forward up to
	// limit+1 bytes before the limit error fires, so the invariant is
	// "intact or not", not "zero or all".
	run := func(limit int64) int {
		ts := mkServer()
		defer ts.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
		bridge(ctx, conn, tcpA, limit, slog.Default())
		return <-total
	}

	// Configured 4 KiB: the 5 KiB frame must not cross intact.
	if n := run(4096); n >= len(payload) {
		t.Fatalf("limit=4096: %d bytes crossed; the frame must be cut short", n)
	}
	// Default-sized limit: the same frame crosses intact.
	if n := run(bridgeWSReadLimit); n != len(payload) {
		t.Fatalf("limit=default: n=%d, want %d intact", n, len(payload))
	}
}

// TestEnvelopeVersionRejected (F14): the shared reader refuses unsupported
// envelope versions — behaviour the host client previously lacked, gained by
// adopting relay.ReadEnvelope.
func TestEnvelopeVersionRejected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = c.Write(r.Context(), websocket.MessageText, []byte(`{"v":99,"type":"register_ok"}`))
		<-r.Context().Done()
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if _, err := relay.ReadEnvelope(ctx, conn); err == nil {
		t.Fatal("v:99 envelope must be rejected by the shared reader")
	}
}

// countReader closes its pipe end once want bytes have arrived, so the
// intact-frame case terminates without waiting out the bridge context.
type countReader struct {
	r    net.Conn
	n    int
	want int
}

func (c *countReader) Read(p []byte) (int, error) {
	// Per-read deadline: the cut-short case ends with the bridge's TCP leg
	// blocked in a pipe read that only a peer close can break, and this side
	// is that peer — without a deadline reader and bridge deadlock waiting
	// for each other.
	_ = c.r.SetReadDeadline(time.Now().Add(time.Second))
	n, err := c.r.Read(p)
	c.n += n
	if err == nil && c.n >= c.want {
		return n, io.EOF
	}
	return n, err
}

// TestBridgeUnblocksTCPLegOnWSDeath pins the P6 deviation fix: when the WS
// side dies first, the TCP leg's blocking Read must be broken by the bridge
// itself (context.AfterFunc closing the conn), not by waiting for the daemon
// to close the local side. Pre-fix this deadlocks: the TCP leg parks in
// tcp.Read on a pipe whose only other owner is the bridge's caller.
func TestBridgeUnblocksTCPLegOnWSDeath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// Die immediately: the WS leg errors, the TCP leg must follow.
		_ = c.Close(websocket.StatusGoingAway, "relay gone")
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	tcpA, tcpB := net.Pipe()
	defer tcpB.Close()
	done := make(chan struct{})
	go func() {
		bridge(context.Background(), conn, tcpA, bridgeWSReadLimit, slog.Default())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not return after the WS side died; TCP leg is parked")
	}
}
