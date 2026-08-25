package opencode

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func f64(v float64) *float64 { return &v }

// lastUsageEvent returns the final usage report the host saw.
func lastUsageEvent(h *captureHost) *event.Usage {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out *event.Usage
	for _, ev := range h.events {
		if ev.Type == event.TypeUsage {
			out = ev.Usage
		}
	}
	return out
}

// TestUsageCarriesLatestTurnDetail proves the additive buckets reach the wire
// alongside the unchanged legacy fields (MADR 0112 A4).
func TestUsageCarriesLatestTurnDetail(t *testing.T) {
	h, s := removalSession(t)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{
		"id":"m1","role":"assistant","cost":0.0125,
		"tokens":{"input":100,"output":50,"reasoning":25,"cache":{"read":10,"write":5}}}}`))

	u := lastUsageEvent(h)
	if u == nil {
		t.Fatal("no usage report")
	}
	// Legacy fields keep their meaning for older clients.
	if u.Used != 100+10+50+25 {
		t.Fatalf("used = %d", u.Used)
	}
	if u.Input != 100 || u.Output != 50 || u.Reasoning != 25 ||
		u.CacheRead != 10 || u.CacheWrite != 5 {
		t.Fatalf("detail = %+v", u)
	}
	if u.CostUSD == nil || *u.CostUSD != 0.0125 {
		t.Fatalf("cost = %v", u.CostUSD)
	}
}

// TestFreeTurnIsDistinguishableFromUnknownCost is the reason cost is a pointer.
func TestFreeTurnIsDistinguishableFromUnknownCost(t *testing.T) {
	h1, s1 := removalSession(t)
	s1.HandleEvent("message.updated", json.RawMessage(`{"info":{
		"id":"m1","role":"assistant","cost":0,
		"tokens":{"input":10,"output":5,"reasoning":0,"cache":{"read":0,"write":0}}}}`))
	free := lastUsageEvent(h1)
	if free == nil || free.CostUSD == nil {
		t.Fatalf("a known-free turn lost its zero cost: %+v", free)
	}
	if *free.CostUSD != 0 {
		t.Fatalf("cost = %v, want 0", *free.CostUSD)
	}

	h2, s2 := removalSession(t)
	s2.HandleEvent("message.updated", json.RawMessage(`{"info":{
		"id":"m1","role":"assistant",
		"tokens":{"input":10,"output":5,"reasoning":0,"cache":{"read":0,"write":0}}}}`))
	unknown := lastUsageEvent(h2)
	if unknown == nil {
		t.Fatal("no usage report")
	}
	if unknown.CostUSD != nil {
		t.Fatalf("an absent cost was reported as %v", *unknown.CostUSD)
	}
}

// TestInvalidCostIsOmittedNotClamped proves a value that cannot be a price is
// reported as unknown rather than as a fabricated number.
func TestInvalidCostIsOmittedNotClamped(t *testing.T) {
	for _, bad := range []*float64{
		f64(math.NaN()), f64(math.Inf(1)), f64(math.Inf(-1)), f64(-0.5),
	} {
		if got := validCost(bad); got != nil {
			t.Fatalf("validCost(%v) = %v, want nil", *bad, *got)
		}
	}
	if got := validCost(nil); got != nil {
		t.Fatal("validCost(nil) invented a value")
	}
	if got := validCost(f64(0)); got == nil || *got != 0 {
		t.Fatalf("a finite zero must survive, got %v", got)
	}
	if got := validCost(f64(1.5)); got == nil || *got != 1.5 {
		t.Fatalf("validCost(1.5) = %v", got)
	}
}

// TestNegativeCountsAreDroppedNotClamped proves a negative bucket is reported
// as zero-unknown rather than as a negative token count on the wire.
func TestNegativeCountsAreDroppedNotClamped(t *testing.T) {
	tk := msgTokens{Input: -5, Output: 10, Reasoning: -1}
	tk.Cache.Read = -2
	tk.Cache.Write = 3
	in, out, reasoning, cr, cw := tk.detail()
	if in != 0 || reasoning != 0 || cr != 0 {
		t.Fatalf("negative counts leaked: %d %d %d", in, reasoning, cr)
	}
	if out != 10 || cw != 3 {
		t.Fatalf("valid counts lost: %d %d", out, cw)
	}
}

// TestUsageLatchTracksTheDetail proves a report whose context total is
// unchanged but whose buckets or cost moved is still emitted — otherwise the
// breakdown would go stale while the headline looked current.
func TestUsageLatchTracksTheDetail(t *testing.T) {
	h, s := removalSession(t)
	base := `{"info":{"id":"m1","role":"assistant","cost":%s,
		"tokens":{"input":%d,"output":%d,"reasoning":0,"cache":{"read":0,"write":%d}}}}`

	// Same in-context total (input+output), different cache-write and cost.
	s.HandleEvent("message.updated", json.RawMessage(fmt.Sprintf(base, "0.01", 100, 50, 0)))
	first := countUsage(h)
	s.HandleEvent("message.updated", json.RawMessage(fmt.Sprintf(base, "0.02", 100, 50, 7)))
	second := countUsage(h)
	if second <= first {
		t.Fatalf("a changed breakdown was suppressed (%d -> %d reports)", first, second)
	}
	// An identical repeat is still suppressed.
	s.HandleEvent("message.updated", json.RawMessage(fmt.Sprintf(base, "0.02", 100, 50, 7)))
	if third := countUsage(h); third != second {
		t.Fatalf("an identical repeat was re-emitted (%d -> %d)", second, third)
	}
}

func countUsage(h *captureHost) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, ev := range h.events {
		if ev.Type == event.TypeUsage {
			n++
		}
	}
	return n
}

// TestUserMessagesReportNoUsage proves only assistant turns produce accounting.
func TestUserMessagesReportNoUsage(t *testing.T) {
	h, s := removalSession(t)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{
		"id":"m1","role":"user","cost":1,
		"tokens":{"input":10,"output":5,"reasoning":0,"cache":{"read":0,"write":0}}}}`))
	if u := lastUsageEvent(h); u != nil {
		t.Fatalf("a user message reported usage: %+v", u)
	}
}
