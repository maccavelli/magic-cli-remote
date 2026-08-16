package session

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// feedSession is a provider session whose event stream the test drives directly,
// so debounced-persist / history-flush interleavings can be reproduced without
// relying on wall-clock timers.
type feedSession struct {
	id     string
	mu     sync.Mutex
	closed bool
	events chan event.Event
}

func (s *feedSession) ID() string                                       { return s.id }
func (s *feedSession) ProviderID() provider.ID                          { return provider.IDFake }
func (s *feedSession) AgentSessionID() string                           { return s.id }
func (s *feedSession) Events() <-chan event.Event                       { return s.events }
func (s *feedSession) Prompt(context.Context, []provider.Content) error { return nil }
func (s *feedSession) Cancel(context.Context) error                     { return nil }

func (s *feedSession) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.events)
	return nil
}

func (s *feedSession) emit(ev event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.events <- ev:
	default:
	}
}

type feedProvider struct {
	mu       sync.Mutex
	sessions map[string]*feedSession
}

func newFeedProvider() *feedProvider {
	return &feedProvider{sessions: make(map[string]*feedSession)}
}

func (p *feedProvider) ID() provider.ID { return provider.IDFake }
func (p *feedProvider) Ready() bool     { return true }

func (p *feedProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	s := &feedSession{id: opts.LocalSessionID, events: make(chan event.Event, 64)}
	p.mu.Lock()
	p.sessions[opts.LocalSessionID] = s
	p.mu.Unlock()
	return s, nil
}

// feed pushes an event onto the current session for id (best-effort; dropped if
// the session was closed/replaced, matching real provider emit semantics).
func (p *feedProvider) feed(id string, ev event.Event) {
	p.mu.Lock()
	s := p.sessions[id]
	p.mu.Unlock()
	if s != nil {
		s.emit(ev)
	}
}

func statusEvent(id, status string) event.Event {
	return event.Event{Type: event.TypeSessionStatus, SessionID: id, Status: status, Timestamp: time.Now().UTC()}
}

func chunkEvent(id, text string) event.Event {
	return event.Event{Type: event.TypeAssistantChunk, SessionID: id, Text: text, Timestamp: time.Now().UTC()}
}

// persistFixture wires a manager to a real store and a feed provider, exposing a
// channel that receives every broadcast event so tests can synchronize on the
// pump having processed a fed event (dirtyPersist/dirtyHistory are set by then).
func persistFixture(t *testing.T) (*Store, *Manager, *feedProvider, chan event.Event) {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prov := newFeedProvider()
	reg := provider.NewRegistry()
	reg.Register(prov)
	recv := make(chan event.Event, 256)
	mgr := NewManager(reg, store, nil, func(ev event.Event) { recv <- ev })
	return store, mgr, prov, recv
}

// waitEvent waits for the pump to process one provider event. The daemon's own
// command advertisement (remote_commands, emitted at create and whenever
// resolution changes) is skipped: it is not what these tests synchronize on, and
// consuming it instead would let them run before the fed event was handled.
func waitEvent(t *testing.T, recv chan event.Event) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-recv:
			if ev.Type == event.TypeRemoteCommands {
				continue
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for pump to process event")
		}
	}
}

// H1: deleting a session must cancel its pending debounced meta write, or a
// timer firing after store.Delete resurrects the record.
func TestDeleteCancelsPendingDebouncedPersist(t *testing.T) {
	store, mgr, prov, recv := persistFixture(t)
	ctx := context.Background()

	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: "sess-del"}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	// A non-terminal status schedules a *debounced* write (dirtyPersist set).
	prov.feed(meta.ID, statusEvent(meta.ID, "running"))
	waitEvent(t, recv)

	if err := mgr.Delete(ctx, meta.ID, "dev"); err != nil {
		t.Fatal(err)
	}
	// Simulate the debounce timer firing after the delete.
	mgr.FlushPersist()

	if rec, err := store.Get(meta.ID); err == nil {
		t.Fatalf("deleted session resurrected on disk: %+v", rec)
	}
}

// FlushPersist copies live meta then writes without holding persistMu. If
// Delete runs in that window, Save would recreate the row and the phone
// would show a resumable session the user just ended (kilo after End).
func TestDeleteWinsInFlightFlushPersist(t *testing.T) {
	store, mgr, prov, recv := persistFixture(t)
	ctx := context.Background()

	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: "sess-race"}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	prov.feed(meta.ID, statusEvent(meta.ID, "running"))
	waitEvent(t, recv)

	// Snapshot as FlushPersist does, then delete, then write that snapshot.
	mgr.mu.RLock()
	snap := mgr.sessions[meta.ID].meta
	mgr.mu.RUnlock()

	if err := mgr.Delete(ctx, meta.ID, "dev"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.writePersist(snap); err != nil {
		t.Fatal(err)
	}
	if rec, err := store.Get(meta.ID); err == nil {
		t.Fatalf("in-flight flush resurrected deleted session: %+v", rec)
	}
}

// H2: a debounced flush must write the session's *current* meta, not a snapshot
// captured when the id was first marked dirty. This is what stops a stale
// ownerless write from reverting an owner claimed in between. Reproduced by
// marking the id dirty, then changing the live meta (as a concurrent claim
// would) before the flush: with snapshot semantics the flush would revert; with
// re-read semantics it must reflect the change.
func TestFlushPersistReReadsCurrentMeta(t *testing.T) {
	store, mgr, prov, recv := persistFixture(t)
	ctx := context.Background()

	// Unowned (legacy) session: owner is empty until first-touch claim.
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: "sess-claim"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Schedule a debounced write while the session is still ownerless.
	prov.feed(meta.ID, statusEvent(meta.ID, "running"))
	waitEvent(t, recv)

	// An owner claim lands after the id was marked dirty. Mutate the live meta
	// directly (bypassing persistNow, which would clear the dirty mark) to model
	// the claim winning the race against a pending debounced flush.
	mgr.mu.Lock()
	mgr.sessions[meta.ID].meta.OwnerDeviceID = "device-x"
	mgr.mu.Unlock()

	mgr.FlushPersist()

	rec, err := store.Get(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.OwnerDeviceID != "device-x" {
		t.Fatalf("debounced flush wrote stale meta: owner=%q want device-x", rec.OwnerDeviceID)
	}
}

// M4: FlushHistory must skip a dead-but-not-yet-removed entry; writing its nil
// snapshot would clobber the full transcript the close path is about to flush.
func TestFlushHistorySkipsDeadEntry(t *testing.T) {
	store, mgr, prov, recv := persistFixture(t)
	ctx := context.Background()

	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: "sess-hist"}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	prov.feed(meta.ID, chunkEvent(meta.ID, "alpha"))
	waitEvent(t, recv)
	prov.feed(meta.ID, chunkEvent(meta.ID, "beta"))
	waitEvent(t, recv)

	mgr.FlushHistory()
	if got := transcript(store.LoadHistory(meta.ID)); len(got) != 2 {
		t.Fatalf("precondition: disk history len=%d want 2", len(got))
	}

	// Reproduce the window: entry flagged dead (disconnect in flight) and marked
	// dirty, but closeMatching has not yet removed it and flushed the full ring.
	mgr.mu.Lock()
	mgr.sessions[meta.ID].dead = true
	mgr.mu.Unlock()
	mgr.scheduleHistoryPersist(meta.ID)
	mgr.FlushHistory()

	if got := transcript(store.LoadHistory(meta.ID)); len(got) != 2 {
		t.Fatalf("dead-entry flush clobbered transcript: disk len=%d want 2", len(got))
	}
}

// Concurrency stress: many goroutines create/claim/status/delete the same small
// id space while flushes race. Under -race this exercises the persist pipeline's
// locking; the invariant is that no deleted session is left resurrected on disk.
// FlushPersistConcurrentMetaUpdate: a meta update that arrives while
// FlushPersist is writing must not be lost — the re-arm check at the end
// of FlushPersist must start a new timer for entries that were added during
// the flush window.
func TestFlushPersistConcurrentMetaUpdate(t *testing.T) {
	store, mgr, prov, recv := persistFixture(t)
	ctx := context.Background()

	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: "sess-meta"}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	// Feed a status event to mark the session dirty.
	prov.feed(meta.ID, statusEvent(meta.ID, "running"))
	waitEvent(t, recv)

	// FlushPersist snapshots dirtyPersist, clears it, releases persistMu,
	// then iterates and writes each entry. After the loop it re-acquires
	// persistMu to re-arm if new entries arrived.
	mgr.FlushPersist()

	// While the first flush was in flight, the pump may have processed
	// nothing new — we verify that a *subsequent* dirty write is not lost.
	// Feed another status change to re-dirty the id.
	prov.feed(meta.ID, statusEvent(meta.ID, "idle"))
	waitEvent(t, recv)

	// Simulate the debounce timer firing (the re-arm from FlushPersist
	// would do this). Without a re-arm, this second flush would be empty
	// and the "idle" status would never reach disk.
	mgr.FlushPersist()

	rec, err := store.Get(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != "idle" {
		t.Fatalf("meta status = %q, want idle (concurrent update lost)", rec.Status)
	}
}

// FlushHistoryConcurrentAppend: history appended while FlushHistory is
// writing must not be lost — the re-arm check must schedule a follow-up
// flush for entries dirtied during the flush window.
func TestFlushHistoryConcurrentAppend(t *testing.T) {
	store, mgr, prov, recv := persistFixture(t)
	ctx := context.Background()

	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: "sess-hist2"}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	// Feed a chunk to schedule a history persist.
	prov.feed(meta.ID, chunkEvent(meta.ID, "alpha"))
	waitEvent(t, recv)

	// Drain the first flush.
	mgr.FlushHistory()

	// Feed a second chunk, then flush again to simulate the re-arm path.
	prov.feed(meta.ID, chunkEvent(meta.ID, "beta"))
	waitEvent(t, recv)
	mgr.FlushHistory()

	if got := transcript(store.LoadHistory(meta.ID)); len(got) != 2 {
		t.Fatalf("concurrent append lost events: disk len=%d want 2", len(got))
	}
}

func TestManagerConcurrentCreateDeleteFlush(t *testing.T) {
	store, mgr, prov, recv := persistFixture(t)
	// Drain broadcasts so the buffered recv channel never blocks the pump.
	go func() {
		for range recv {
		}
	}()
	ctx := context.Background()

	ids := []string{"c0", "c1", "c2", "c3", "c4", "c5", "c6", "c7"}
	var wg sync.WaitGroup
	for range 12 {
		for _, id := range ids {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				m, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: id}, "dev")
				if err != nil {
					return
				}
				prov.feed(m.ID, statusEvent(m.ID, "running"))
				_ = mgr.Authorize(m.ID, "dev", true)
				mgr.FlushPersist()
				_ = mgr.Delete(ctx, m.ID, "dev")
			}(id)
		}
	}
	wg.Wait()

	// Final flushes simulate the shutdown drain; nothing should be resurrected.
	mgr.FlushPersist()
	mgr.FlushHistory()

	recs, _, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, rc := range recs {
		if slices.Contains(ids, rc.ID) {
			t.Errorf("deleted session %q resurrected on disk after concurrent churn", rc.ID)
		}
	}
}
