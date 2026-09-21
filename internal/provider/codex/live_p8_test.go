//go:build live_codex

package codex

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestLiveResumeEarnsNoDeprecationNotice is acceptance criterion A13 of PLAN 0163,
// and the proof that the paginated-history migration actually happened rather than
// merely being written.
//
// Codex emits a deprecationNotice on every full-history hydration — for
// thread/read with includeTurns:true, and for a resume that does not set
// excludeTurns (thread_processor.rs:33-34). The notice is advisory and arrives
// after the response, so nothing fails when we earn one: the only way to know we
// stopped is to resume a real thread and watch for it.
//
// It asserts an ABSENCE over a live session, which is the shape reviewers call
// flaky and delete. It is not flaky: the notice is emitted synchronously by the
// request processor that handles the resume, so it is already queued by the time
// resume returns. The drain below is bounded and does not wait for it to "maybe"
// arrive.
func TestLiveResumeEarnsNoDeprecationNotice(t *testing.T) {
	p := NewWithLogger(Config{Bin: "codex"}, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// An EXISTING thread, deliberately, rather than one this test creates. A
	// freshly started thread has taken no turns, so Codex has written no rollout
	// for it and there is nothing to resume — the first version of this test
	// created a thread and resumed it, and failed with "failed to locate thread
	// id" for that reason rather than for the reason it was testing. Resuming real
	// history also costs no tokens, unlike earning history with a turn.
	existing, err := p.ListAgentSessions(ctx)
	if err != nil {
		t.Fatalf("listing threads: %v", err)
	}
	threadID := ""
	for _, meta := range existing {
		if meta.ID != "" {
			threadID = meta.ID
			break
		}
	}
	if threadID == "" {
		t.Skip("no existing codex thread to resume; run a turn first")
	}
	cwd := liveThreadCwd(t)

	// Resuming is what exercises both halves of P8: excludeTurns on the resume
	// itself, and the thread/items/list paging that replaces the full read.
	resumed, err := p.Start(ctx, provider.StartOptions{AgentSessionID: threadID, CWD: cwd})
	if err != nil {
		t.Fatalf("resume session %s: %v", threadID, err)
	}
	defer resumed.Close(context.Background())

	deadline := time.After(10 * time.Second)
	var warnings []string
	for {
		select {
		case ev := <-resumed.Events():
			if ev.Type != event.TypeCodexWarning || ev.Codex == nil {
				continue
			}
			if ev.Codex.Kind == "deprecation" {
				// Report the text: it names which hydration path earned it, which is
				// the difference between the resume and the replay being at fault.
				t.Errorf("resume earned a deprecationNotice: %q", ev.Codex.Text)
			}
			warnings = append(warnings, ev.Codex.Kind)
		case <-deadline:
			t.Logf("drained live warnings without a deprecation notice: %v", warnings)
			return
		case <-ctx.Done():
			t.Fatalf("context expired before the drain completed: %v", ctx.Err())
		}
	}
}
