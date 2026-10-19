package session

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
)

// seqRecorder is an onEvent hook that keeps the seqs it was handed, in the
// order it was handed them.
type seqRecorder struct {
	mu   sync.Mutex
	seqs []uint64
	evs  []event.Event
}

func (r *seqRecorder) record(ev event.Event) {
	r.mu.Lock()
	r.seqs = append(r.seqs, ev.Seq)
	r.evs = append(r.evs, ev)
	r.mu.Unlock()
}

func (r *seqRecorder) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seqs)
}

// checkIncreasing fails unless every delivered seq is above the one before.
func (r *seqRecorder) checkIncreasing(t *testing.T, label string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := 1; i < len(r.seqs); i++ {
		if r.seqs[i] <= r.seqs[i-1] {
			t.Fatalf("%s: delivery %d has seq %d (%s) after seq %d (%s); want strictly increasing",
				label, i, r.seqs[i], r.evs[i].Type, r.seqs[i-1], r.evs[i-1].Type)
		}
	}
}

// waitTurnComplete waits until rec has seen a turn_complete.
func waitTurnComplete(t *testing.T, rec *seqRecorder) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec.mu.Lock()
		done := false
		for _, ev := range rec.evs {
			done = done || ev.Type == event.TypeTurnComplete
		}
		n := len(rec.evs)
		rec.mu.Unlock()
		if done {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no turn_complete among %d deliveries", n)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitDeliveries waits until rec has seen at least n events.
func waitDeliveries(t *testing.T, rec *seqRecorder, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for rec.len() < n {
		if time.Now().After(deadline) {
			t.Fatalf("saw %d deliveries, want at least %d", rec.len(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestFirstEventsOfASessionArriveInSeqOrder is MADR 0201 D11 (0073 F-S1).
// The pump stamped an event's seq, then delivered the remote_commands it
// triggered, and only then the event itself: a client saw seq N+1 before N
// and dropped N as a duplicate. A new fake session's start-up events (a mode
// list and the first usage report, each re-advertising commands) and its
// first turn must reach clients in seq order.
func TestFirstEventsOfASessionArriveInSeqOrder(t *testing.T) {
	for round := range 20 {
		rec := &seqRecorder{}
		reg := provider.NewRegistry()
		reg.Register(fake.New())
		mgr := NewManager(reg, nil, nil, rec.record)
		meta, err := mgr.Create(context.Background(), provider.IDFake, provider.StartOptions{}, "dev-a")
		if err != nil {
			t.Fatal(err)
		}
		if err := mgr.Prompt(context.Background(), meta.ID, "hello", nil, "dev-a"); err != nil {
			t.Fatal(err)
		}
		waitTurnComplete(t, rec)
		_ = mgr.Close(context.Background(), meta.ID, "dev-a")
		rec.checkIncreasing(t, fmt.Sprintf("round %d", round))
	}
}

// TestConcurrentEmitsKeepSeqOrder is MADR 0201 D11 (0073 F-S1). Daemon
// notices emitted from several goroutines while the pump delivers provider
// events must still reach clients in seq order: emitEvent stamped under m.mu
// and delivered after dropping it, so two emitters could swap.
func TestConcurrentEmitsKeepSeqOrder(t *testing.T) {
	const (
		emitters    = 4
		perEmitter  = 50
		providerEvs = 200
	)
	rec := &seqRecorder{}
	p := &metadataProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, nil, nil, rec.record)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close(context.Background(), meta.ID, "dev-a") })
	base := rec.len()

	var wg sync.WaitGroup
	for g := range emitters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perEmitter {
				mgr.emitNotice(meta.ID, fmt.Sprintf("emitter %d notice %d", g, i))
			}
		}()
	}
	for i := range providerEvs {
		p.session.events <- event.Event{Type: event.TypeAssistantChunk, SessionID: meta.ID,
			Text: fmt.Sprintf("chunk %d", i)}
	}
	wg.Wait()
	waitDeliveries(t, rec, base+emitters*perEmitter+providerEvs)
	rec.checkIncreasing(t, "concurrent emits")
}
