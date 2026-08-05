package session

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// MADR 0068 P3 (U5) — the seq-lineage epoch: kept across clean restarts,
// minted fresh after an unclean one, so clients detect seq regression
// instead of silently filtering real events.

func newEpochManager(t *testing.T, store *Store) *Manager {
	t.Helper()
	return NewManager(provider.NewRegistry(), store, nil, func(event.Event) {})
}

func TestEpochKeptAcrossCleanRestart(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	m1 := newEpochManager(t, store)
	e1 := m1.Epoch()
	if e1 == "" {
		t.Fatal("manager with store must have an epoch")
	}
	m1.CloseAll(context.Background()) // clean shutdown marks the epoch clean

	m2 := newEpochManager(t, store)
	if e2 := m2.Epoch(); e2 != e1 {
		t.Fatalf("clean restart changed epoch: %s → %s", e1, e2)
	}
}

// The kill -9 shape: the first manager never runs CloseAll, so the dirty
// marker written at startup is still in place — the next boot must mint a
// fresh epoch because up to persistDebounce of events may be unflushed.
func TestEpochChangesAfterUncleanExit(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	m1 := newEpochManager(t, store)
	e1 := m1.Epoch()
	// No CloseAll — the process "died".

	m2 := newEpochManager(t, store)
	if e2 := m2.Epoch(); e2 == e1 {
		t.Fatalf("unclean restart kept epoch %s — seq regression undetectable", e1)
	}
}

func TestEpochEmptyWithoutStore(t *testing.T) {
	m := NewManager(provider.NewRegistry(), nil, nil, func(event.Event) {})
	if e := m.Epoch(); e != "" {
		t.Fatalf("storeless manager epoch = %q, want empty", e)
	}
}

// SeqBounds reads the durable ring for sessions that are not live — the
// long-gap reconnect shape — and reports the retained window exactly.
func TestSeqBoundsFromDurableHistory(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	events := make([]event.Event, 0, 51)
	for seq := uint64(100); seq <= 150; seq++ {
		events = append(events, event.Event{Type: "noop", SessionID: "s1", Seq: seq})
	}
	if err := store.SaveHistory("s1", events); err != nil {
		t.Fatal(err)
	}

	m := newEpochManager(t, store)
	first, latest := m.SeqBounds("s1")
	if first != 100 || latest != 150 {
		t.Fatalf("SeqBounds = (%d,%d), want (100,150)", first, latest)
	}
	// Unknown session: nothing retained.
	if f, l := m.SeqBounds("nope"); f != 0 || l != 0 {
		t.Fatalf("unknown session bounds = (%d,%d), want (0,0)", f, l)
	}
}
