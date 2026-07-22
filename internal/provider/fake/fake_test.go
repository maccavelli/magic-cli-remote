package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
)

func TestStartHonorsLocalSessionID(t *testing.T) {
	p := fake.New()
	s, err := p.Start(context.Background(), provider.StartOptions{LocalSessionID: "fixed-id"})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID() != "fixed-id" {
		t.Fatalf("id=%q want fixed-id", s.ID())
	}
	_ = s.Close(context.Background())
}

// drain reads events into a slice until `until` is satisfied, the session
// closes, or the deadline hits — so control-event delivery can be asserted
// without dropping any.
func drain(t *testing.T, s provider.Session, until func([]event.Event) bool) []event.Event {
	t.Helper()
	var got []event.Event
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				return got
			}
			got = append(got, ev)
			if until != nil && until(got) {
				return got
			}
		case <-deadline:
			t.Fatalf("timed out; events so far: %+v", got)
			return got
		}
	}
}

func hasType(evs []event.Event, typ event.Type) bool {
	for _, e := range evs {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// A second Prompt while a turn is active must be rejected, matching the real
// transports (httpagent returns "prompt already in progress"), not silently
// restart the turn.
func TestPromptRejectsConcurrentTurn(t *testing.T) {
	p := fake.New()
	s, err := p.Start(context.Background(), provider.StartOptions{LocalSessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	// Consume events so the buffer never blocks the turn goroutine.
	go func() {
		for range s.Events() {
		}
	}()

	ctx := context.Background()
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	err = s.Prompt(ctx, []provider.Content{{Type: "text", Text: "again"}})
	if err == nil {
		t.Fatal("second prompt during active turn should error")
	}
	if err.Error() != "prompt already in progress" {
		t.Fatalf("second prompt err=%q want \"prompt already in progress\"", err.Error())
	}
}

// After a turn completes the session accepts the next prompt (turnActive clears).
func TestPromptAcceptedAfterTurnCompletes(t *testing.T) {
	p := fake.New()
	s, err := p.Start(context.Background(), provider.StartOptions{LocalSessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	ctx := context.Background()
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "one"}}); err != nil {
		t.Fatal(err)
	}
	// Wait for the turn to complete, then a second prompt must succeed.
	drain(t, s, func(evs []event.Event) bool { return hasType(evs, event.TypeTurnComplete) })
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "two"}}); err != nil {
		t.Fatalf("prompt after turn complete should succeed, got %v", err)
	}
}

// Cancel must end the turn with a turn_complete{cancelled} (then idle), so a
// consumer waiting on turn_complete after Stop cannot hang.
func TestCancelEmitsTurnComplete(t *testing.T) {
	p := fake.New()
	s, err := p.Start(context.Background(), provider.StartOptions{LocalSessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	ctx := context.Background()
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	evs := drain(t, s, func(evs []event.Event) bool { return hasType(evs, event.TypeTurnComplete) })

	var tc event.Event
	for _, e := range evs {
		if e.Type == event.TypeTurnComplete {
			tc = e
		}
	}
	if tc.Status != "cancelled" || tc.StopReason != "cancelled" {
		t.Fatalf("turn_complete after cancel = status=%q stop=%q want cancelled/cancelled", tc.Status, tc.StopReason)
	}
}

// Control events are never dropped even against a slow, buffer-filling consumer:
// every turn_complete and its trailing idle status must arrive. This guards the
// blocking-delivery contract shared with the real transports ([event.IsControl]).
func TestControlEventsNotDropped(t *testing.T) {
	p := fake.New()
	s, err := p.Start(context.Background(), provider.StartOptions{LocalSessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	ctx := context.Background()
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	// Read slowly so a non-blocking send would drop; control events must still
	// all land because emit blocks for them.
	var sawTurnComplete, sawIdleAfter bool
	deadline := time.After(3 * time.Second)
	for !(sawTurnComplete && sawIdleAfter) {
		select {
		case ev := <-s.Events():
			time.Sleep(2 * time.Millisecond)
			switch {
			case ev.Type == event.TypeTurnComplete:
				sawTurnComplete = true
			case ev.Type == event.TypeSessionStatus && ev.Status == "idle" && sawTurnComplete:
				sawIdleAfter = true
			}
		case <-deadline:
			t.Fatalf("control events dropped: turn_complete=%v idle=%v", sawTurnComplete, sawIdleAfter)
		}
	}
}
