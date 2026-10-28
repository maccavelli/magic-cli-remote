package acpagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// answerWithUsage is a prompt answer carrying every field of the unstable
// Usage, as an agent writes it. Decoding it through acp.PromptResponse pins the
// field's shape (MADR 0207 amendment H, D25).
const answerWithUsage = `{"stopReason":"end_turn","usage":{"inputTokens":1000,"outputTokens":200,"totalTokens":1200,` +
	`"thoughtTokens":50,"cachedReadTokens":700,"cachedWriteTokens":30}}`

func decodeAnswer(t *testing.T, raw string) acp.PromptResponse {
	t.Helper()
	var resp acp.PromptResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return resp
}

func usageEvents(s *session) []*event.Usage {
	var out []*event.Usage
	for len(s.events) > 0 {
		if ev := <-s.events; ev.Type == event.TypeUsage {
			out = append(out, ev.Usage)
		}
	}
	return out
}

// TestTurnUsageReachesThePhoneOnce is D25: an answer that carries usage gives
// one usage event with the turn's split: input, output, reasoning and both cache
// counts. With no context report yet, Used is the turn's total and Size stays
// 0, as grok's xAI usage reports it.
func TestTurnUsageReachesThePhoneOnce(t *testing.T) {
	s, _ := newUpdateSession()
	s.beginTurnUsage()
	s.emitTurnUsage(decodeAnswer(t, answerWithUsage))
	got := usageEvents(s)
	if len(got) != 1 {
		t.Fatalf("usage events = %d, want 1", len(got))
	}
	u := got[0]
	if u.Input != 1000 || u.Output != 200 || u.Reasoning != 50 || u.CacheRead != 700 || u.CacheWrite != 30 {
		t.Errorf("split = %+v, want input 1000, output 200, reasoning 50, cache read 700, cache write 30", *u)
	}
	if u.Used != 1200 || u.Size != 0 {
		t.Errorf("used %d of %d, want the turn's 1200 of 0", u.Used, u.Size)
	}
}

// TestTurnUsageIsNotCountedBesideXAIUsage is D25: grok reports a turn's usage on
// its own channel before the answer (frames 82 and 85 of grok 1.0.13's capture),
// so an answer that also carries usage adds no second event.
func TestTurnUsageIsNotCountedBesideXAIUsage(t *testing.T) {
	s, _ := newUpdateSession()
	s.beginTurnUsage()
	s.emitXAIUsage(&xaiUsage{InputTokens: 900, OutputTokens: 100, TotalTokens: 1000}, false)
	s.emitTurnUsage(decodeAnswer(t, answerWithUsage))
	got := usageEvents(s)
	if len(got) != 1 || got[0].Input != 900 {
		t.Fatalf("usage events = %+v, want only grok's own, input 900", got)
	}

	// The next turn starts clean: its answer's usage is reported.
	s.beginTurnUsage()
	s.emitTurnUsage(decodeAnswer(t, answerWithUsage))
	if got := usageEvents(s); len(got) != 1 || got[0].Input != 1000 {
		t.Fatalf("next turn's usage events = %+v, want the answer's, input 1000", got)
	}
}

// TestReplayedXAIUsageDoesNotSilenceTheTurn: a replayed xAI record, as a
// session/load sends, is not this turn's report.
func TestReplayedXAIUsageDoesNotSilenceTheTurn(t *testing.T) {
	s, _ := newUpdateSession()
	s.beginTurnUsage()
	s.emitXAIUsage(&xaiUsage{InputTokens: 900, OutputTokens: 100, TotalTokens: 1000}, true)
	s.emitTurnUsage(decodeAnswer(t, answerWithUsage))
	if got := usageEvents(s); len(got) != 2 || got[1].Input != 1000 {
		t.Fatalf("usage events = %+v, want the replay's and then the answer's", got)
	}
}

// TestTurnWithoutUsageReportsNone: an answer with no usage, and no xAI usage,
// adds no usage event, as before D25.
func TestTurnWithoutUsageReportsNone(t *testing.T) {
	s, _ := newUpdateSession()
	s.beginTurnUsage()
	s.emitTurnUsage(decodeAnswer(t, `{"stopReason":"end_turn"}`))
	if got := usageEvents(s); len(got) != 0 {
		t.Fatalf("usage events = %+v, want none", got)
	}
}

// TestTurnUsageArrivesOverTheWire: a real prompt whose answer carries usage, on
// a connection with production's options, reports the split before the turn
// ends.
func TestTurnUsageArrivesOverTheWire(t *testing.T) {
	s, a := newWireSession(t, `{}`)
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	req := a.request("session/prompt", wireBudget)
	var answer map[string]any
	if err := json.Unmarshal([]byte(answerWithUsage), &answer); err != nil {
		t.Fatal(err)
	}
	a.respond(req, answer)
	var usage *event.Usage
	deadline := time.After(wireBudget)
	for {
		select {
		case ev := <-s.events:
			switch ev.Type {
			case event.TypeUsage:
				usage = ev.Usage
			case event.TypeTurnComplete:
				if usage == nil || usage.Input != 1000 || usage.CacheRead != 700 {
					t.Fatalf("turn_complete came with usage %+v before it, want the answer's split", usage)
				}
				return
			}
		case <-deadline:
			t.Fatal("no turn_complete")
		}
	}
}

// TestTurnUsageKeepsTheContextGauge: the phone replaces its usage with each
// report's, so the turn's report carries the context gauge the last
// usage_update set, not 0 or the turn's total.
func TestTurnUsageKeepsTheContextGauge(t *testing.T) {
	s, _ := newUpdateSession()
	s.beginTurnUsage()
	deliver(t, s, `{"sessionUpdate":"usage_update","used":5300,"size":200000}`)
	s.emitTurnUsage(decodeAnswer(t, answerWithUsage))
	got := usageEvents(s)
	if len(got) != 2 {
		t.Fatalf("usage events = %d, want the gauge's and the turn's", len(got))
	}
	if u := got[1]; u.Used != 5300 || u.Size != 200000 || u.Input != 1000 {
		t.Fatalf("turn report = %+v, want used 5300 of 200000 with input 1000", *u)
	}
}
