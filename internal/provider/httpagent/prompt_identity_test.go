package httpagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// identifyingDialect implements IdentifiedPromptDialectSession and records the
// ids it was submitted with.
type identifyingDialect struct {
	fakeDialectSession
	mu        sync.Mutex
	minted    int
	gotIDs    []string
	plainN    int
	promptErr error
}

func (d *identifyingDialect) NewPromptMessageID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.minted++
	return "msg_test_" + string(rune('a'+d.minted-1))
}

func (d *identifyingDialect) PromptWithMessageID(_ context.Context, id string, _ []provider.Content) error {
	d.mu.Lock()
	d.gotIDs = append(d.gotIDs, id)
	d.mu.Unlock()
	return d.promptErr
}

func (d *identifyingDialect) Prompt(context.Context, []provider.Content) error {
	d.mu.Lock()
	d.plainN++
	d.mu.Unlock()
	return d.promptErr
}

func (d *identifyingDialect) submitted() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.gotIDs...)
}

func text(s string) []provider.Content { return []provider.Content{{Type: "text", Text: s}} }

// userIDs returns the native message ids of emitted user rows.
func userIDs(evs []event.Event) []string {
	var out []string
	for _, ev := range evs {
		if ev.Type == event.TypeUserMessage {
			out = append(out, ev.NativeMessageID)
		}
	}
	return out
}

// TestImmediatePromptSendsTheOptimisticID is the core identity contract: the id
// on the row the user already sees is the id the engine is told to use.
func TestImmediatePromptSendsTheOptimisticID(t *testing.T) {
	ds := &identifyingDialect{}
	s := newHookSession(t, ds)
	ds.h = s

	if err := s.Prompt(context.Background(), text("hi")); err != nil {
		t.Fatal(err)
	}
	shown := userIDs(drainEvents(s))
	sent := ds.submitted()
	if len(shown) != 1 || len(sent) != 1 {
		t.Fatalf("shown=%v sent=%v", shown, sent)
	}
	if shown[0] != sent[0] {
		t.Fatalf("optimistic row %q but submitted %q", shown[0], sent[0])
	}
	if !strings.HasPrefix(sent[0], "msg") {
		t.Fatalf("id %q does not satisfy the MessageID schema prefix", sent[0])
	}
	if ds.plainN != 0 {
		t.Fatal("the ID-less path was used despite an identifying dialect")
	}
}

// TestQueuedPromptKeepsItsIDUntilDrained proves the id is minted at accept
// time, not at drain time: the optimistic row is rendered immediately and the
// engine must later be told that same id.
func TestQueuedPromptKeepsItsIDUntilDrained(t *testing.T) {
	ds := &identifyingDialect{}
	s := newHookSession(t, ds)
	ds.h = s

	s.mu.Lock()
	s.turnActive = true
	s.mu.Unlock()

	if err := s.Prompt(context.Background(), text("queued")); err != nil {
		t.Fatal(err)
	}
	shown := userIDs(drainEvents(s))
	if len(shown) != 1 || shown[0] == "" {
		t.Fatalf("queued prompt showed no identified row: %v", shown)
	}
	if len(ds.submitted()) != 0 {
		t.Fatal("a queued prompt was submitted immediately")
	}

	s.EndTurn()
	s.tryDrainQueue()

	sent := ds.submitted()
	if len(sent) != 1 {
		t.Fatalf("submitted %v after drain", sent)
	}
	if sent[0] != shown[0] {
		t.Fatalf("drained with %q but the row showed %q", sent[0], shown[0])
	}
	// Draining must not render a second user row.
	if got := userIDs(drainEvents(s)); len(got) != 0 {
		t.Fatalf("drain emitted another user row: %v", got)
	}
}

// TestEachPromptGetsADistinctID proves ids are per-message, so two queued
// prompts cannot reconcile onto one row.
func TestEachPromptGetsADistinctID(t *testing.T) {
	ds := &identifyingDialect{}
	s := newHookSession(t, ds)
	ds.h = s
	s.mu.Lock()
	s.turnActive = true
	s.mu.Unlock()

	for _, msg := range []string{"one", "two", "three"} {
		if err := s.Prompt(context.Background(), text(msg)); err != nil {
			t.Fatal(err)
		}
	}
	shown := userIDs(drainEvents(s))
	if len(shown) != 3 {
		t.Fatalf("shown = %v", shown)
	}
	seen := map[string]bool{}
	for _, id := range shown {
		if id == "" || seen[id] {
			t.Fatalf("duplicate or empty id in %v", shown)
		}
		seen[id] = true
	}
}

// TestQueueOverflowMintsNoID proves a refused prompt neither renders a row nor
// consumes an identity.
func TestQueueOverflowMintsNoID(t *testing.T) {
	ds := &identifyingDialect{}
	s := newHookSession(t, ds)
	ds.h = s
	s.mu.Lock()
	s.turnActive = true
	s.promptQueue = make([]queuedPrompt, maxPromptQueue)
	s.mu.Unlock()

	before := ds.minted
	if err := s.Prompt(context.Background(), text("overflow")); !errors.Is(err, provider.ErrTurnBusy) {
		t.Fatalf("err = %v, want ErrTurnBusy", err)
	}
	if ds.minted != before {
		t.Fatal("a refused prompt still minted an id")
	}
	if got := userIDs(drainEvents(s)); len(got) != 0 {
		t.Fatalf("a refused prompt rendered a row: %v", got)
	}
}

// TestSubmitFailureKeepsTheVisibleRow proves a failed submit does not fabricate
// an upstream response, and does not claim a part id.
func TestSubmitFailureKeepsTheVisibleRow(t *testing.T) {
	ds := &identifyingDialect{promptErr: errors.New("engine refused")}
	s := newHookSession(t, ds)
	ds.h = s

	if err := s.Prompt(context.Background(), text("doomed")); err == nil {
		t.Fatal("expected the submit error to surface")
	}
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeUserMessage && ev.NativePartID != "" {
			t.Fatalf("optimistic row claimed a part id: %+v", ev)
		}
	}
}

// TestNonIdentifyingDialectUsesThePlainPath proves other providers are
// untouched: no id is minted, no row claims one, and Prompt is called.
func TestNonIdentifyingDialectUsesThePlainPath(t *testing.T) {
	s := newHookSession(t, nil)
	if err := s.Prompt(context.Background(), text("hi")); err != nil {
		t.Fatal(err)
	}
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeUserMessage && ev.NativeMessageID != "" {
			t.Fatalf("a non-identifying dialect produced an identified row: %+v", ev)
		}
	}
}

// TestOptimisticRowCarriesNoPartID proves the optimistic row is message-level,
// which is what lets the first authoritative user part replace it wholesale.
func TestOptimisticRowCarriesNoPartID(t *testing.T) {
	ds := &identifyingDialect{}
	s := newHookSession(t, ds)
	ds.h = s
	if err := s.Prompt(context.Background(), text("hi")); err != nil {
		t.Fatal(err)
	}
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeUserMessage {
			if ev.NativeMessageID == "" {
				t.Fatal("optimistic row has no message id")
			}
			if ev.NativePartID != "" {
				t.Fatalf("optimistic row claimed part id %q", ev.NativePartID)
			}
			if ev.Replace {
				t.Fatal("optimistic row claimed to be authoritative")
			}
		}
	}
}
