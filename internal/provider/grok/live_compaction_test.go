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

// Run with: go test -tags live_grok ./internal/provider/grok/ -run CompactionAtSessionStart -count=1
//
// PLAN 0190 K4, zero-token half: Magic offers grok session.compaction at
// initialize (MADR 0190 D4). This opens a session the way production does and
// watches its start, with no prompt, so it spends no model tokens. grok must
// accept the advertisement; any compaction event it sends is logged, so the
// run records whether grok uses the capability at session start.
func TestLiveGrokCompactionAtSessionStart(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "compaction-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start with session.compaction advertised: %v", err)
	}
	defer s.Close(context.Background())

	// Session start settles within a few seconds: MCP servers connect and grok
	// advertises its commands. Watch long enough to see that through.
	watch := time.After(12 * time.Second)
	compactions := 0
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeCompaction {
				compactions++
				t.Logf("compaction: id=%q status=%q summary=%d bytes truncated=%t",
					ev.CompactionID, ev.CompactionStatus, len(ev.CompactionSummary), ev.CompactionTruncated)
			}
		case <-watch:
			t.Logf("grok sent %d compaction event(s) at session start", compactions)
			return
		}
	}
}
