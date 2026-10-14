package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// TestForkStartsWithZeroSessionTotals is PLAN 0138 Phase 12: a fork is a new
// session under a new id, so it does not inherit its parent's spend
// (MADR 0138, amendment 2026-10-14).
func TestForkStartsWithZeroSessionTotals(t *testing.T) {
	p := &forkProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := session.NewManager(reg, nil, nil, nil)

	parent, err := mgr.Create(context.Background(), "forker", provider.StartOptions{Name: "work"}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Prompt(context.Background(), parent.ID, "hi", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	ps := p.last
	p.mu.Unlock()
	ps.events <- event.Event{Type: event.TypeUsage, SessionID: ps.id,
		Usage: &event.Usage{Used: 10, Input: 500, Output: 20, CacheRead: 100}}
	ps.events <- event.Event{Type: event.TypeTurnComplete, SessionID: ps.id, StopReason: "end_turn"}

	deadline := time.Now().Add(2 * time.Second)
	for {
		m, err := mgr.Get(parent.ID)
		if err != nil {
			t.Fatal(err)
		}
		if m.Turns == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the parent never accrued its turn: %+v", m)
		}
		time.Sleep(5 * time.Millisecond)
	}

	child, err := mgr.Fork(context.Background(), parent.ID, "", "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if child.Turns != 0 || child.InputTokens != 0 || child.OutputTokens != 0 || child.CachedTokens != 0 {
		t.Fatalf("fork totals = %d turns, %d/%d/%d tokens; want zero",
			child.Turns, child.InputTokens, child.OutputTokens, child.CachedTokens)
	}
}
