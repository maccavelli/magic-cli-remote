//go:build live_grok

package grok_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run PlanAtSessionStart -count=1
//
// PLAN 0192 P4, zero-token half: Magic offers grok the unstable plan
// capability at initialize (MADR 0192 D5). This opens a session the way
// production does and watches its start, with no prompt, so it spends no model
// tokens. grok must accept the advertisement; any plan it sends is logged, so
// the run records whether grok uses the capability at session start.
func TestLiveGrokPlanAtSessionStart(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "plan-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start with plan advertised: %v", err)
	}
	defer s.Close(context.Background())

	// Session start settles within a few seconds: MCP servers connect and grok
	// advertises its commands. Watch long enough to see that through.
	watch := time.After(12 * time.Second)
	stable, identified := 0, 0
	for {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypePlan:
				stable++
				t.Logf("plan: %d entries", len(ev.Entries))
			case event.TypeIdentifiedPlan:
				identified++
				t.Logf("identified_plan: kind=%q removed=%t", ev.PlanKind, ev.PlanRemoved)
			}
		case <-watch:
			t.Logf("grok sent %d plan and %d identified plan event(s) at session start", stable, identified)
			return
		}
	}
}
