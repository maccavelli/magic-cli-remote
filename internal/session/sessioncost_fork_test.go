package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// TestForkStartsWithoutTheParentsSessionCost is PLAN 0189 C2: a fork is a
// different agent session with its own running total, so the parent's figure
// is not carried over (MADR 0189 D2).
func TestForkStartsWithoutTheParentsSessionCost(t *testing.T) {
	p := &forkProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := session.NewManager(reg, nil, nil, nil)

	parent, err := mgr.Create(context.Background(), "forker", provider.StartOptions{Name: "work"}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	ps := p.last
	p.mu.Unlock()
	ps.events <- event.Event{Type: event.TypeUsage, SessionID: ps.id, Usage: &event.Usage{
		Used: 10, Size: 100, SessionCost: &event.SessionCost{Amount: 0.5, Currency: "USD"}}}

	deadline := time.Now().Add(2 * time.Second)
	for {
		m, err := mgr.Get(parent.ID)
		if err != nil {
			t.Fatal(err)
		}
		if m.SessionCost != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the parent never recorded its session cost")
		}
		time.Sleep(5 * time.Millisecond)
	}

	child, err := mgr.Fork(context.Background(), parent.ID, "", "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if child.SessionCost != nil {
		t.Fatalf("fork session cost = %+v, want none", child.SessionCost)
	}
}
