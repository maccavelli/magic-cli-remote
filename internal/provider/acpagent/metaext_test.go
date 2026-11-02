package acpagent

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// retryKey is a vendor's retry marker in `_meta` (MADR 0214 D14, as
// corrected).
const retryKey = "vendor/retry"

// The marker's frames, in the shape the 0214 records give one; the values are
// illustrative. The vendor's own frames are tested in its package.
const (
	// A retry is scheduled.
	retryValue     = `{"attempt":2,"nextRetryAt":"2026-11-01T12:00:05.000Z","error":{"type":"api","message":"Rate limited","status":429,"retryAfterMs":5000}}`
	retryScheduled = `{"sessionUpdate":"session_info_update","_meta":{"` + retryKey + `":` + retryValue + `}}`
	// The next step started, so the retry is cleared.
	retryCleared = `{"sessionUpdate":"session_info_update","_meta":{"` + retryKey + `":null}}`
	// A turn interrupted while a retry was pending.
	retryAtInterrupt = `{"stopReason":"cancelled","_meta":{"` + retryKey + `":` + retryValue + `}}`
)

// metaCall is one call of a MetaHandler.
type metaCall struct {
	from  MetaSource
	value json.RawMessage
}

// metaRecorder is a MetaHandler that keeps each call.
type metaRecorder struct {
	mu    sync.Mutex
	calls []metaCall
}

func (r *metaRecorder) handle(_ context.Context, _ Host, from MetaSource, value json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, metaCall{from: from, value: append(json.RawMessage(nil), value...)})
}

func (r *metaRecorder) got() []metaCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// TestRetryMetaReachesItsHandler is D14 as corrected (0214-PLAN P2): a
// session_info_update's `_meta` key a Spec registered goes to its handler and
// is not declined, and a `_meta`-only update leaves the title alone.
func TestRetryMetaReachesItsHandler(t *testing.T) {
	for _, tc := range []struct {
		name, frame, want string
	}{
		{"scheduled", retryScheduled, retryValue},
		{"cleared", retryCleared, `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, h := newUpdateSession()
			rec := &metaRecorder{}
			s.metaHandlers = map[string]MetaHandler{retryKey: rec.handle}
			deliver(t, s, tc.frame)
			calls := rec.got()
			if len(calls) != 1 {
				t.Fatalf("the handler was called %d time(s), want 1", len(calls))
			}
			if calls[0].from != MetaFromSessionInfo || !jsonEqual(t, calls[0].value, json.RawMessage(tc.want)) {
				t.Errorf("call = %s %s, want %s %s", calls[0].from, calls[0].value, MetaFromSessionInfo, tc.want)
			}
			if variants, _ := h.declined(); len(variants) != 0 {
				t.Errorf("declined %v; the key has a handler", variants)
			}
			if len(s.events) != 0 {
				t.Errorf("%d event(s); a _meta-only update sets no title", len(s.events))
			}
		})
	}
}

// TestUnregisteredMetaKeyIsDeclinedAsBefore: a `_meta` key no handler is
// registered for is declined, by a Spec with handlers for other keys and by
// one with none, as every key was before D14.
func TestUnregisteredMetaKeyIsDeclinedAsBefore(t *testing.T) {
	for name, handlers := range map[string]map[string]MetaHandler{
		"none":  nil,
		"other": {"vendor/other": (&metaRecorder{}).handle},
	} {
		t.Run(name, func(t *testing.T) {
			s, h := newUpdateSession()
			s.metaHandlers = handlers
			deliver(t, s, retryScheduled)
			if variants, _ := h.declined(); !slices.Equal(variants, []string{"session_info_update._meta"}) {
				t.Errorf("declined %v, want session_info_update._meta", variants)
			}
		})
	}
}

// promptOnce runs one turn over the wire, answers it with result, and waits
// for turn_complete.
func promptOnce(t *testing.T, s *session, a *wireAgent, result string) {
	t.Helper()
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	req := a.request("session/prompt", wireBudget)
	var answer map[string]any
	if err := json.Unmarshal([]byte(result), &answer); err != nil {
		t.Fatal(err)
	}
	a.respond(req, answer)
	deadline := time.After(wireBudget)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeTurnComplete {
				return
			}
		case <-deadline:
			t.Fatal("no turn_complete")
		}
	}
}

// TestPromptResultMetaReachesItsHandler is D14 as corrected: the prompt
// result's `_meta` key a Spec registered goes to its handler before the turn
// ends.
func TestPromptResultMetaReachesItsHandler(t *testing.T) {
	rec := &metaRecorder{}
	s, a := newWireSessionFor(t, "{}", &Provider{spec: Spec{MetaHandlers: map[string]MetaHandler{retryKey: rec.handle}}})
	promptOnce(t, s, a, retryAtInterrupt)
	calls := rec.got()
	if len(calls) != 1 {
		t.Fatalf("the handler was called %d time(s) before turn_complete, want 1", len(calls))
	}
	if calls[0].from != MetaFromPromptResult || !jsonEqual(t, calls[0].value, json.RawMessage(retryValue)) {
		t.Errorf("call = %s %s, want %s %s", calls[0].from, calls[0].value, MetaFromPromptResult, retryValue)
	}
}

// TestEmptyPromptResultMetaCallsNoHandler: the fixture's two prompt results,
// each with `_meta: {}`, call no handler.
func TestEmptyPromptResultMetaCallsNoHandler(t *testing.T) {
	var results []string
	for _, f := range fixtureFrames(t, "") {
		var m wireMsg
		if err := json.Unmarshal([]byte(f), &m); err != nil {
			t.Fatal(err)
		}
		var r struct {
			StopReason string `json:"stopReason"`
		}
		if json.Unmarshal(m.Result, &r) == nil && r.StopReason != "" {
			results = append(results, string(m.Result))
		}
	}
	if len(results) != 2 {
		t.Fatalf("the fixture has %d prompt results, want 2", len(results))
	}
	rec := &metaRecorder{}
	s, a := newWireSessionFor(t, "{}", &Provider{spec: Spec{MetaHandlers: map[string]MetaHandler{retryKey: rec.handle}}})
	for _, r := range results {
		promptOnce(t, s, a, r)
	}
	if calls := rec.got(); len(calls) != 0 {
		t.Fatalf("the handler was called %d time(s); every fixture _meta is empty", len(calls))
	}
}
