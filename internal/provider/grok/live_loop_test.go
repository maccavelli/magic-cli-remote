//go:build live_grok

package grok_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// T-G1: /loop is advertised on grok 1.0.4. Promote to specs.go only if this
// prompt actually schedules (MADR 0081 Phase G). Silence means TUI-only —
// do not add the canonical command.
func TestLiveGrokLoopSchedules(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "loop-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	advertised := waitForCommands(t, s, 30*time.Second)
	if !slicesContainsFold(advertised, "loop") {
		t.Log("loop not advertised; leave specs.go untouched")
		return
	}

	got := promptText(t, s, "/loop 60s reply with the single word pong", 90*time.Second)
	t.Logf("/loop reply (%d bytes): %s", len(got), got)
	if strings.TrimSpace(got) == "" {
		t.Fatal("advertised /loop returned silence — do not promote to specs.go")
	}
	lower := strings.ToLower(got)
	scheduled := strings.Contains(lower, "loop") ||
		strings.Contains(lower, "schedul") ||
		strings.Contains(lower, "interval") ||
		strings.Contains(lower, "every 60") ||
		strings.Contains(lower, "job")
	if !scheduled {
		t.Log("reply did not clearly name a scheduled job; treat as not scheduled")
		t.Fatal("do not promote /loop: no schedule evidence in the reply")
	}
	// Best-effort cancel so a passing test does not leave a 7-day loop.
	const marker = "**task_id:** `"
	if i := strings.Index(got, marker); i >= 0 {
		rest := got[i+len(marker):]
		if j := strings.Index(rest, "`"); j > 0 {
			_ = promptText(t, s, "scheduler_delete "+rest[:j], 45*time.Second)
		}
	}
}
