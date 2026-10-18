package acpagent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// usdOf is how a tick total must reach the wire: grok's 1e10 ticks to the
// dollar, as a session cost in USD.
func usdOf(ticks int64) event.SessionCost {
	return event.SessionCost{Amount: float64(ticks) / usdTicksPerUSD, Currency: "USD"}
}

// liveTurn is a live `_x.ai/session_notification` turn_completed. An empty
// promptID leaves the field out; a nil ticks leaves the cost out.
func liveTurn(t *testing.T, s *session, promptID string, ticks *int64, partial, replay bool) {
	t.Helper()
	usage := map[string]any{"totalTokens": 10, "inputTokens": 8, "outputTokens": 2}
	if ticks != nil {
		usage["costUsdTicks"] = *ticks
	}
	if partial {
		usage["costIsPartial"] = true
	}
	update := map[string]any{"sessionUpdate": "turn_completed", "usage": usage}
	if promptID != "" {
		update["prompt_id"] = promptID
	}
	params := map[string]any{"sessionId": s.agentID, "update": update}
	if replay {
		params["_meta"] = map[string]any{"isReplay": true}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	HandleXAISessionNotification(context.Background(), s, raw)
}

func ticksOf(n int64) *int64 { return &n }

// drainEvents returns every event queued so far.
func drainEvents(s *session) []event.Event {
	var out []event.Event
	for {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// TestReplayedTurnCostsRebuildTheSessionTotal is 0199 E1 and E2 against grok's
// own frames: a session/load replays every turn on `_x.ai/session/update`, each
// with its original cost. Those add to the sum and report nothing; the first
// live turn reports the whole session's total.
func TestReplayedTurnCostsRebuildTheSessionTotal(t *testing.T) {
	f, err := os.Open("../grok/testdata/wire/1.0.50/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := newQueueTestSession()
	s.loading = true
	replayed := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var frame struct {
			Method string `json:"method"`
			Params struct {
				SessionID string `json:"sessionId"`
			} `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &frame) != nil || frame.Method != "_x.ai/session/update" {
			continue
		}
		s.agentID = frame.Params.SessionID
		var params struct {
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &params); err != nil {
			t.Fatal(err)
		}
		HandleXAISessionUpdate(context.Background(), s, params.Params)
		replayed++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if replayed != 3 {
		t.Fatalf("replayed %d `_x.ai/session/update` frames, want the capture's 3", replayed)
	}
	if evs := drainEvents(s); len(evs) != 0 {
		t.Fatalf("the replay channel emitted %d events, want none: %+v", len(evs), evs)
	}

	// A turn grok marks as a replay on the live channel adds and reports no
	// session cost either, even after loading has ended.
	s.loading = false
	liveTurn(t, s, "replayed-elsewhere", ticksOf(5), false, true)
	if u := drainUsage(t, s); u.SessionCost != nil {
		t.Fatalf("a replay-marked turn reported session cost %+v", *u.SessionCost)
	}

	liveTurn(t, s, "live-1", ticksOf(10_000_000), false, false)
	u := drainUsage(t, s)
	want := usdOf(118_673_600 + 31_320_800 + 31_633_600 + 5 + 10_000_000)
	if u.SessionCost == nil || *u.SessionCost != want {
		t.Fatalf("SessionCost = %+v, want %+v: the replayed turns and the live one", u.SessionCost, want)
	}
	if u.CostUSD != nil {
		t.Fatalf("CostUSD = %v: grok's cost is a session total, never the per-turn figure (0189 D1)", *u.CostUSD)
	}
}

// TestARepeatedTurnCountsOnce is 0199 E1's dedupe by prompt_id.
func TestARepeatedTurnCountsOnce(t *testing.T) {
	s := newQueueTestSession()
	liveTurn(t, s, "p1", ticksOf(100), false, false)
	_ = drainUsage(t, s)
	liveTurn(t, s, "p1", ticksOf(100), false, false)
	u := drainUsage(t, s)
	if u.SessionCost == nil || *u.SessionCost != usdOf(100) {
		t.Fatalf("SessionCost = %+v, want %+v: a repeated prompt_id counted twice", u.SessionCost, usdOf(100))
	}
}

// TestALiveTurnWithoutAPromptIDStillCounts: nothing can match such a turn, so
// it is counted rather than lost.
func TestALiveTurnWithoutAPromptIDStillCounts(t *testing.T) {
	s := newQueueTestSession()
	liveTurn(t, s, "", ticksOf(100), false, false)
	_ = drainUsage(t, s)
	liveTurn(t, s, "", ticksOf(100), false, false)
	u := drainUsage(t, s)
	if u.SessionCost == nil || *u.SessionCost != usdOf(200) {
		t.Fatalf("SessionCost = %+v, want %+v", u.SessionCost, usdOf(200))
	}
}

// TestAnUnknownTurnCostStopsTheSessionTotal is 0199 E3: a total missing a turn
// is too low and would read as complete, so it is not reported at all. The
// token counts still are.
func TestAnUnknownTurnCostStopsTheSessionTotal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ticks   *int64
		partial bool
	}{
		{"no cost", nil, false},
		{"partial cost", ticksOf(50), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newQueueTestSession()
			liveTurn(t, s, "p1", ticksOf(100), false, false)
			if u := drainUsage(t, s); u.SessionCost == nil {
				t.Fatal("first turn reported no session cost")
			}
			liveTurn(t, s, "p2", tc.ticks, tc.partial, false)
			if u := drainUsage(t, s); u.SessionCost != nil || u.Used != 10 {
				t.Fatalf("turn with an unknown cost: SessionCost %+v, Used %d; want none and 10",
					u.SessionCost, u.Used)
			}
			liveTurn(t, s, "p3", ticksOf(100), false, false)
			if u := drainUsage(t, s); u.SessionCost != nil {
				t.Fatalf("a later turn reported %+v after an unknown cost", *u.SessionCost)
			}
		})
	}
}

// TestReplayChannelDeclinesOtherVariantsByName: `_x.ai/session/update` carries
// more than turns (a `background_tasks` in the 1.0.50 load capture). Those are
// declined by name, not silently dropped, and nothing is emitted.
func TestReplayChannelDeclinesOtherVariantsByName(t *testing.T) {
	s := newQueueTestSession()
	params := json.RawMessage(`{"sessionId":"agent-q","update":{"sessionUpdate":"background_tasks","tasks":[]},` +
		`"_meta":{"isReplay":true}}`)
	HandleXAISessionUpdate(context.Background(), s, params)
	HandleXAISessionUpdate(context.Background(), s, params)
	if evs := drainEvents(s); len(evs) != 0 {
		t.Fatalf("emitted %d events for background_tasks, want none", len(evs))
	}
	s.declinedMu.Lock()
	declined := s.declined["_x.ai/session/update.background_tasks"]
	s.declinedMu.Unlock()
	if !declined {
		t.Fatalf("background_tasks was not declined by name; declined = %v", s.declined)
	}
}
