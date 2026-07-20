package session

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// scriptedSession emits a fixed batch of events then idles, so the ring buffer
// and History replay can be driven deterministically.
type scriptedSession struct {
	id     string
	events chan event.Event
}

func (s *scriptedSession) ID() string                                       { return s.id }
func (s *scriptedSession) ProviderID() provider.ID                          { return provider.IDFake }
func (s *scriptedSession) AgentSessionID() string                           { return s.id }
func (s *scriptedSession) Events() <-chan event.Event                       { return s.events }
func (s *scriptedSession) Prompt(context.Context, []provider.Content) error { return nil }
func (s *scriptedSession) Cancel(context.Context) error                     { return nil }
func (s *scriptedSession) Close(context.Context) error                      { return nil }

type scriptedProvider struct {
	count int
}

func (p *scriptedProvider) ID() provider.ID { return provider.IDFake }
func (p *scriptedProvider) Ready() bool     { return true }
func (p *scriptedProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	s := &scriptedSession{id: opts.LocalSessionID, events: make(chan event.Event, p.count+1)}
	for i := 0; i < p.count; i++ {
		s.events <- event.Event{
			Type:      event.TypeAssistantChunk,
			SessionID: s.id,
			Timestamp: time.Now().UTC(),
			Text:      strconv.Itoa(i),
		}
	}
	return s, nil
}

// The per-session ring buffer caps at historyBufferCap and drops the oldest
// events; History returns the survivors in emission order, unknown -> empty.
func TestHistoryRingBufferCapsAndOrders(t *testing.T) {
	const emitted = historyBufferCap + 100

	reg := provider.NewRegistry()
	reg.Register(&scriptedProvider{count: emitted})

	var mu sync.Mutex
	seen := 0
	done := make(chan struct{})
	mgr := NewManager(reg, nil, nil, func(event.Event) {
		mu.Lock()
		seen++
		if seen == emitted {
			close(done)
		}
		mu.Unlock()
	})

	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "t"})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for events to pump")
	}

	hist := mgr.History(meta.ID)
	if len(hist) != historyBufferCap {
		t.Fatalf("history len = %d, want cap %d", len(hist), historyBufferCap)
	}
	// Oldest 100 dropped: first retained event has index emitted-cap = 100.
	if got := hist[0].Text; got != strconv.Itoa(emitted-historyBufferCap) {
		t.Fatalf("oldest retained = %q, want %q", got, strconv.Itoa(emitted-historyBufferCap))
	}
	if got := hist[len(hist)-1].Text; got != strconv.Itoa(emitted-1) {
		t.Fatalf("newest retained = %q, want %q", got, strconv.Itoa(emitted-1))
	}
	// Strictly increasing (emission) order.
	for i := 1; i < len(hist); i++ {
		prev, _ := strconv.Atoi(hist[i-1].Text)
		cur, _ := strconv.Atoi(hist[i].Text)
		if cur != prev+1 {
			t.Fatalf("out of order at %d: %d then %d", i, prev, cur)
		}
	}

	// Unknown session yields an empty, non-nil slice (not an error).
	if got := mgr.History("no-such-session"); got == nil || len(got) != 0 {
		t.Fatalf("unknown session history = %v, want empty non-nil", got)
	}
}
