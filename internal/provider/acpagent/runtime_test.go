package acpagent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
)

// TestTheNonBlockingFunnelNeverProbesBilling pins the Phase 10 deviation.
//
// emitClassifiedTurnError is reached from the stderr copier goroutine and from
// the ACP SDK's notification consumer. Blocking the latter tears the connection
// down in 7.16 ms (MADR 0138 F5, measured in Phase 9's G2), and a Spec's
// ConfirmLimit, such as a billing probe, is a network round trip bounded
// at six seconds — four orders of magnitude the wrong side of that.
func TestTheNonBlockingFunnelNeverProbesBilling(t *testing.T) {
	s, _ := newUpdateSession()
	var probed atomic.Bool
	s.ops.ConfirmLimit = func(context.Context, Host, agenterr.Kind) (string, bool) {
		probed.Store(true)
		return "credits spent", true
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.emitClassifiedTurnError("Monthly usage limit reached. Resets in 4 days")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the non-blocking funnel blocked")
	}

	if probed.Load() {
		t.Fatal("emitClassifiedTurnError called the Spec's ConfirmLimit. It runs on the SDK's " +
			"notification consumer and on the stderr copier; a six-second call there is F5")
	}
}

// TestAnnotateLimitLeavesAnUnconfirmedMessageAlone.
func TestAnnotateLimitLeavesAnUnconfirmedMessageAlone(t *testing.T) {
	const msg = "Monthly usage limit reached."
	if got := annotateLimit(msg, ""); got != msg {
		t.Errorf("annotateLimit with no summary changed the message: %q", got)
	}
	got := annotateLimit(msg, "included credits 99.4% used")
	if !strings.Contains(got, "plan usage") || !strings.Contains(got, "99.4") {
		t.Errorf("annotateLimit = %q", got)
	}
}
