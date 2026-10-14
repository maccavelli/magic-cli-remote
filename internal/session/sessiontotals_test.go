package session

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// PLAN 0138 Phase 12: the session totals 6.3 accrues are what this session has
// spent so far, so they survive a relaunch, a flush and a daemon restart
// (MADR 0138, amendment 2026-10-14).

type totals struct {
	turns                 int
	input, output, cached int64
}

func totalsOf(m Meta) totals {
	return totals{m.Turns, m.InputTokens, m.OutputTokens, m.CachedTokens}
}

// runTurn starts the turn clock, reports usage and ends the turn, and returns
// once the manager has processed it (the events channel is unbuffered).
func runTurn(t *testing.T, mgr *Manager, p *metadataProvider, id string, in, out, cached int64) {
	t.Helper()
	mgr.markPromptStart(id)
	s := p.session
	s.events <- event.Event{Type: event.TypeUsage, SessionID: s.id,
		Usage: &event.Usage{Used: 100, Input: in, Output: out, CacheRead: cached}}
	s.events <- event.Event{Type: event.TypeTurnComplete, SessionID: s.id, StopReason: "end_turn"}
	s.events <- event.Event{Type: event.TypeThoughtChunk, SessionID: s.id, Text: "sync"}
}

func TestSessionTotalsSurviveRelaunchAndRestart(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr, p := newNameManager(t, store)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{Name: "spend"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	runTurn(t, mgr, p, meta.ID, 1000, 50, 400)
	runTurn(t, mgr, p, meta.ID, 2000, 70, 1500)
	want := totals{turns: 2, input: 3000, output: 120, cached: 1900}

	got, err := mgr.Get(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if totalsOf(got) != want {
		t.Fatalf("accrued %+v, want %+v", totalsOf(got), want)
	}

	// A relaunch (/clear, /reset, model recovery) re-creates the live session
	// under its own id.
	relaunched, err := mgr.Create(context.Background(), "metadata",
		provider.StartOptions{LocalSessionID: meta.ID, Name: "spend"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if totalsOf(relaunched) != want {
		t.Fatalf("after a relaunch: %+v, want %+v", totalsOf(relaunched), want)
	}

	// Written to disk.
	mgr.FlushPersist()
	rec, err := store.Get(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := (totals{rec.Turns, rec.InputTokens, rec.OutputTokens, rec.CachedTokens}); got != want {
		t.Fatalf("stored record: %+v, want %+v", got, want)
	}

	// A second daemon on the same store: listed from disk, then resumed, and
	// the next turn adds to what was there.
	mgr2, p2 := newNameManager(t, store)
	var listed *Meta
	for _, m := range mgr2.ListFor("owner") {
		if m.ID == meta.ID {
			listed = &m
		}
	}
	if listed == nil || totalsOf(*listed) != want {
		t.Fatalf("listed from disk: %+v, want %+v", listed, want)
	}
	resumed, err := mgr2.Create(context.Background(), "metadata",
		provider.StartOptions{LocalSessionID: meta.ID, Name: "spend"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if totalsOf(resumed) != want {
		t.Fatalf("resumed from disk: %+v, want %+v", totalsOf(resumed), want)
	}
	runTurn(t, mgr2, p2, meta.ID, 10, 1, 5)
	after, err := mgr2.Get(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next := (totals{3, 3010, 121, 1905}); totalsOf(after) != next {
		t.Fatalf("a turn after resume: %+v, want %+v", totalsOf(after), next)
	}
}
