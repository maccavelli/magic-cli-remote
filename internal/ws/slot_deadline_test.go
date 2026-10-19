package ws

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// overrideSlotTimings shortens the async deadline and the slot grace for one
// test.
func overrideSlotTimings(t *testing.T, timeout, grace time.Duration) {
	t.Helper()
	prevTimeout, prevGrace := asyncOpTimeoutFor, asyncSlotGrace
	asyncOpTimeoutFor = func(string) time.Duration { return timeout }
	asyncSlotGrace = grace
	t.Cleanup(func() { asyncOpTimeoutFor, asyncSlotGrace = prevTimeout, prevGrace })
}

// TestAnOverrunningHandlerReturnsItsSlotAtTheDeadline is MADR 0201 D6 (0073
// F-T5). A handler that ignores its context used to keep its async slot until
// it returned — eight of them turned every later operation on the connection
// into rate_limited. The slot now comes back at the deadline plus a grace.
func TestAnOverrunningHandlerReturnsItsSlotAtTheDeadline(t *testing.T) {
	overrideSlotTimings(t, 50*time.Millisecond, 20*time.Millisecond)
	s := newReceiptWaiterTestServer()
	c := newAuthedTestClient("dev-1")
	c.out = make(chan []byte, 64)

	release := make(chan struct{})
	defer close(release)
	stuck := func(context.Context, *client, protocol.Envelope, string) error {
		<-release // ignores its context on purpose
		return nil
	}
	env, _ := protocol.NewEnvelope(protocol.TypeSessionHistory, "h", nil)
	for range maxAsyncPerClient {
		if err := s.dispatchAsync(context.Background(), c, env, stuck); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(150 * time.Millisecond)

	var ran atomic.Bool
	ninth := func(context.Context, *client, protocol.Envelope, string) error {
		ran.Store(true)
		return nil
	}
	if err := s.dispatchAsync(context.Background(), c, env, ninth); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !ran.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !ran.Load() {
		for len(c.out) > 0 {
			if f := string(<-c.out); strings.Contains(f, "rate_limited") {
				t.Fatalf("a ninth operation was refused after the deadline passed: %s", f)
			}
		}
		t.Fatal("the ninth operation never ran")
	}
}

// TestASlotIsReturnedOnce: a handler that returns after its deadline must not
// release its slot a second time — the in-flight count never goes negative.
func TestASlotIsReturnedOnce(t *testing.T) {
	overrideSlotTimings(t, 20*time.Millisecond, 10*time.Millisecond)
	s := newReceiptWaiterTestServer()
	c := newAuthedTestClient("dev-1")
	c.out = make(chan []byte, 64)

	done := make(chan struct{})
	late := func(context.Context, *client, protocol.Envelope, string) error {
		time.Sleep(100 * time.Millisecond) // well past deadline + grace
		close(done)
		return nil
	}
	env, _ := protocol.NewEnvelope(protocol.TypeSessionHistory, "h", nil)
	if err := s.dispatchAsync(context.Background(), c, env, late); err != nil {
		t.Fatal(err)
	}
	<-done
	time.Sleep(30 * time.Millisecond)
	s.mu.Lock()
	n := c.asyncInFlight
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("asyncInFlight = %d after the handler finished, want 0", n)
	}
}
