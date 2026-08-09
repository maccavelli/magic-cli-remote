package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

func newReceiptWaiterTestServer() *Server {
	return &Server{
		clients: map[*client]struct{}{},
		log:     slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func newAuthedTestClient(deviceID string) *client {
	return &client{
		deviceID: deviceID,
		authed:   true,
		out:      make(chan []byte, 8),
		closed:   make(chan struct{}),
	}
}

func receiptEnvelope(t *testing.T, permissionID, jws string) protocol.Envelope {
	t.Helper()
	env, err := protocol.NewEnvelope(protocol.TypePermissionReceipt, "r1", protocol.PermissionReceiptPayload{
		SessionID:    "sess-1",
		PermissionID: permissionID,
		JWS:          jws,
	})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// TestReceiptWaiterRejectsWrongDevice is the regression guard for the
// waiter's device binding (found in the 0077 post-implementation debug
// pass): a permission.receipt from an authed device that was NOT the one
// asked to sign must never reach the waiter — without the binding, any
// paired device could race-deliver a garbage JWS for a permission id it
// observed, consume the waiter, and downgrade the real device's legitimate
// receipt to an invalid_signature marker.
func TestReceiptWaiterRejectsWrongDevice(t *testing.T) {
	s := newReceiptWaiterTestServer()
	target := newAuthedTestClient("dev-target")
	imposter := newAuthedTestClient("dev-imposter")
	s.clients[target] = struct{}{}
	s.clients[imposter] = struct{}{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got := make(chan string, 1)
	gotErr := make(chan error, 1)
	go func() {
		jws, err := s.RequestPermissionReceipt(ctx, "dev-target", "sess-1", "perm-1", json.RawMessage(`{}`))
		got <- jws
		gotErr <- err
	}()

	// Wait until the waiter is registered (RequestPermissionReceipt registers
	// it before sending), so handlePermissionReceipt has something to find.
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.receiptMu.Lock()
		_, registered := s.receiptWaiters["perm-1"]
		s.receiptMu.Unlock()
		if registered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The imposter delivers first. It must be ignored (and still get ok, so
	// waiter state never leaks) — the waiter stays available for the target.
	if err := s.handlePermissionReceipt(ctx, imposter, receiptEnvelope(t, "perm-1", "imposter-garbage")); err != nil {
		t.Fatalf("imposter delivery errored: %v", err)
	}
	select {
	case jws := <-got:
		t.Fatalf("imposter's JWS reached the waiter: %q", jws)
	case <-time.After(100 * time.Millisecond):
	}

	// The real device's reply still lands.
	if err := s.handlePermissionReceipt(ctx, target, receiptEnvelope(t, "perm-1", "target-jws")); err != nil {
		t.Fatalf("target delivery errored: %v", err)
	}
	select {
	case jws := <-got:
		if jws != "target-jws" {
			t.Fatalf("waiter received %q, want target-jws", jws)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("target's JWS never reached the waiter")
	}
	if err := <-gotErr; err != nil {
		t.Fatalf("RequestPermissionReceipt returned error: %v", err)
	}
}

// TestRequestPermissionReceiptNoConnection: a device with no live connection
// fails immediately (the caller records a timeout marker) rather than
// burning the full round-trip window waiting for a device that is offline.
func TestRequestPermissionReceiptNoConnection(t *testing.T) {
	s := newReceiptWaiterTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	_, err := s.RequestPermissionReceipt(ctx, "dev-offline", "sess-1", "perm-1", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want an error for a device with no live connection")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("took %s — must fail fast, not wait out the round-trip window", elapsed)
	}
	// The waiter must not leak.
	s.receiptMu.Lock()
	_, leaked := s.receiptWaiters["perm-1"]
	s.receiptMu.Unlock()
	if leaked {
		t.Fatal("waiter leaked after a no-connection failure")
	}
}
