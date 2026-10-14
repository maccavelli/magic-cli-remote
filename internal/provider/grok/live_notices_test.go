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

// Run with: go test -tags live_grok ./internal/provider/grok/ -run NoticesAtSessionStart -count=1
//
// PLAN 0188 N4, zero-token half: Magic offers grok session.notices at
// initialize (MADR 0188 D4). This opens a session the way production does and
// watches its start, with no prompt, so it spends no model tokens. grok must
// accept the advertisement; any agent_notice it sends is logged, so the run
// records whether grok uses the capability at session start.
func TestLiveGrokNoticesAtSessionStart(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "notices-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start with session.notices advertised: %v", err)
	}
	defer s.Close(context.Background())

	// Session start settles within a few seconds: MCP servers connect and grok
	// advertises its commands. Watch long enough to see that through.
	watch := time.After(12 * time.Second)
	notices := 0
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeAgentNotice {
				notices++
				t.Logf("agent_notice: severity=%q title=%q description=%q",
					ev.NoticeSeverity, ev.NoticeTitle, ev.NoticeDescription)
			}
		case <-watch:
			t.Logf("grok sent %d agent_notice event(s) at session start", notices)
			return
		}
	}
}
