package ws

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// TestErrorFramesAreNotReplayedToARetry is MADR 0201 D5 (0073 F-T4). A handler
// that answers with an error frame and returns nil — the usual shape — must
// leave the request retryable: the retry runs the handler again rather than
// replaying a stale turn_busy or timeout for the ledger's whole TTL.
func TestErrorFramesAreNotReplayedToARetry(t *testing.T) {
	s := newReceiptWaiterTestServer()
	s.idem = newIdempotencyLedger()
	c := newAuthedTestClient("dev-1")

	if _, _, action := s.idem.begin("dev-1", "req-1"); action != idemExecute {
		t.Fatalf("first begin: action %v, want execute", action)
	}
	if err := s.writeError(context.Background(), c, "req-1", "turn_busy", "a turn is running"); err != nil {
		t.Fatal(err)
	}
	// dispatchAsync's success path runs after a handler that wrote an error
	// frame and returned nil.
	s.idem.complete("dev-1", "req-1", nil)

	frame, _, action := s.idem.begin("dev-1", "req-1")
	if action != idemExecute {
		t.Fatalf("retry after an error frame: action %v (frame %q), want execute", action, frame)
	}
}

// TestSuccessFramesStillReplay keeps the other half of the ledger's contract:
// a request answered with a success frame is replayed to a retry, not re-run.
func TestSuccessFramesStillReplay(t *testing.T) {
	s := newReceiptWaiterTestServer()
	s.idem = newIdempotencyLedger()
	c := newAuthedTestClient("dev-1")

	if _, _, action := s.idem.begin("dev-1", "req-2"); action != idemExecute {
		t.Fatalf("first begin: action %v, want execute", action)
	}
	ok, _ := protocol.NewEnvelope(protocol.TypeOK, "req-2", nil)
	if err := s.writeJSON(context.Background(), c, ok); err != nil {
		t.Fatal(err)
	}
	s.idem.complete("dev-1", "req-2", nil)

	frame, _, action := s.idem.begin("dev-1", "req-2")
	if action != idemReplay || len(frame) == 0 {
		t.Fatalf("retry after success: action %v, frame %d bytes; want replay of the ok frame", action, len(frame))
	}
}
