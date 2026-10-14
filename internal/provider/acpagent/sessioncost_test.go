package acpagent

import (
	"encoding/json"
	"reflect"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// usageFrame builds a usage_update payload from the SDK's own types.
//
// It is the wire fixture for MADR 0189 D4, and it is not a capture: no agent
// Magic runs sends a cost. grok sends no standard usage_update, and gobble's
// sends `used` alone. Building it from the schema types keeps it to the shape
// the SDK would accept from a real agent.
func usageFrame(t *testing.T, cost *acp.Cost) string {
	t.Helper()
	b, err := json.Marshal(acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{
		Used: 1200, Size: 8000, Cost: cost,
	}})
	if err != nil {
		t.Fatalf("marshal usage_update: %v", err)
	}
	return string(b)
}

// TestUsageCostBecomesASessionCost is PLAN 0189 C1 (MADR 0189 D1): an agent's
// cumulative cost is carried as session_cost in its own currency, never as the
// per-turn cost_usd, and a cost Magic cannot trust is declined by name.
func TestUsageCostBecomesASessionCost(t *testing.T) {
	cases := []struct {
		name     string
		cost     *acp.Cost
		want     *event.SessionCost
		declines []string
	}{
		{name: "usd", cost: &acp.Cost{Amount: 0.42, Currency: "USD"},
			want: &event.SessionCost{Amount: 0.42, Currency: "USD"}},
		{name: "free is a real zero", cost: &acp.Cost{Amount: 0, Currency: "USD"},
			want: &event.SessionCost{Amount: 0, Currency: "USD"}},
		{name: "lower-case code", cost: &acp.Cost{Amount: 1.5, Currency: "eur"},
			want: &event.SessionCost{Amount: 1.5, Currency: "EUR"}},
		{name: "negative amount", cost: &acp.Cost{Amount: -0.01, Currency: "USD"},
			declines: []string{"usage_update.cost"}},
		{name: "short code", cost: &acp.Cost{Amount: 1, Currency: "US"},
			declines: []string{"usage_update.cost"}},
		{name: "non-letter code", cost: &acp.Cost{Amount: 1, Currency: "U$D"},
			declines: []string{"usage_update.cost"}},
		{name: "non-ascii code", cost: &acp.Cost{Amount: 1, Currency: "ÜSD"},
			declines: []string{"usage_update.cost"}},
		{name: "meta", cost: &acp.Cost{Amount: 2, Currency: "USD", Meta: map[string]any{"x": 1}},
			want:     &event.SessionCost{Amount: 2, Currency: "USD"},
			declines: []string{"usage_update.cost._meta"}},
		{name: "no cost"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, h := newUpdateSession()
			frame := usageFrame(t, c.cost)
			deliver(t, s, frame)
			deliver(t, s, frame) // a decline is logged once per session

			var usages []*event.Usage
			for len(s.events) > 0 {
				if ev := <-s.events; ev.Type == event.TypeUsage {
					usages = append(usages, ev.Usage)
				}
			}
			if len(usages) != 2 {
				t.Fatalf("usage events = %d, want 2", len(usages))
			}
			u := usages[0]
			if u.Used != 1200 || u.Size != 8000 {
				t.Errorf("used/size = %d/%d, want 1200/8000", u.Used, u.Size)
			}
			if u.CostUSD != nil {
				t.Errorf("cost_usd = %v: a session total must never become the per-turn figure", *u.CostUSD)
			}
			if !reflect.DeepEqual(u.SessionCost, c.want) {
				t.Errorf("session_cost = %+v, want %+v", u.SessionCost, c.want)
			}
			variants, _ := h.declined()
			if len(variants) != 0 || len(c.declines) != 0 {
				if !reflect.DeepEqual(variants, c.declines) {
					t.Errorf("declined %v, want %v (once each)", variants, c.declines)
				}
			}
		})
	}
}
