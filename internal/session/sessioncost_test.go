package session

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// PLAN 0189 C2: the daemon keeps an agent's latest session cost, persists it,
// and reports it in /context (MADR 0189 D2; as JSON since 0208-PLAN P5).

// sendUsage delivers a usage report and returns once the manager has processed
// it, as sendTitle does: the events channel is unbuffered.
func sendUsage(t *testing.T, p *metadataProvider, cost *event.SessionCost) {
	t.Helper()
	s := p.session
	s.events <- event.Event{Type: event.TypeUsage, SessionID: s.id,
		Usage: &event.Usage{Used: 1200, Size: 8000, SessionCost: cost}}
	s.events <- event.Event{Type: event.TypeThoughtChunk, SessionID: s.id, Text: "sync"}
}

func sessionCostOf(t *testing.T, mgr *Manager, id string) *event.SessionCost {
	t.Helper()
	got, err := mgr.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return got.SessionCost
}

func usd(amount float64) *event.SessionCost {
	return &event.SessionCost{Amount: amount, Currency: "USD"}
}

func TestSessionCostIsTheLatestReportedTotal(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr, p := newNameManager(t, store)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{Name: "costed"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if meta.SessionCost != nil {
		t.Fatalf("new session cost = %+v, want none", meta.SessionCost)
	}

	sendUsage(t, p, usd(0.10))
	if got := sessionCostOf(t, mgr, meta.ID); !reflect.DeepEqual(got, usd(0.10)) {
		t.Fatalf("after a report: %+v, want 0.10 USD", got)
	}
	sendUsage(t, p, nil) // grok's own usage events carry no cost
	if got := sessionCostOf(t, mgr, meta.ID); !reflect.DeepEqual(got, usd(0.10)) {
		t.Fatalf("a report without a cost changed it: %+v, want 0.10 USD", got)
	}
	sendUsage(t, p, usd(0.25))
	if got := sessionCostOf(t, mgr, meta.ID); !reflect.DeepEqual(got, usd(0.25)) {
		t.Fatalf("after a second report: %+v, want 0.25 USD (replaced, not added)", got)
	}

	// Persisted, so a daemon restart lists and resumes it.
	mgr.FlushPersist()
	rec, err := store.Get(meta.ID)
	if err != nil || !reflect.DeepEqual(rec.SessionCost, usd(0.25)) {
		t.Fatalf("stored record: %+v %v; want session_cost 0.25 USD", rec.SessionCost, err)
	}

	// Relaunch of the live session keeps it.
	again, err := mgr.Create(context.Background(), "metadata",
		provider.StartOptions{LocalSessionID: meta.ID, Name: "costed"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.SessionCost, usd(0.25)) {
		t.Fatalf("relaunched: %+v, want 0.25 USD", again.SessionCost)
	}

	// A second daemon on the same store: listed from disk, then resumed.
	mgr2, _ := newNameManager(t, store)
	var listed *event.SessionCost
	for _, m := range mgr2.ListFor("owner") {
		if m.ID == meta.ID {
			listed = m.SessionCost
		}
	}
	if !reflect.DeepEqual(listed, usd(0.25)) {
		t.Fatalf("listed from disk: %+v, want 0.25 USD", listed)
	}
	resumed, err := mgr2.Create(context.Background(), "metadata",
		provider.StartOptions{LocalSessionID: meta.ID, Name: "costed"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resumed.SessionCost, usd(0.25)) {
		t.Fatalf("resumed from disk: %+v, want 0.25 USD", resumed.SessionCost)
	}
}

// lastEngineResult returns the session's most recent engine_result event.
func lastEngineResult(t *testing.T, mgr *Manager, id string) event.Event {
	t.Helper()
	for _, v := range slices.Backward(mgr.History(id)) {
		if v.Type == event.TypeEngineResult {
			return v
		}
	}
	t.Fatal("no engine_result was sent")
	return event.Event{}
}

// TestContextReportsTheSessionCost is 0189's Confirmation, as amended
// 2026-11-04: /context is Magic's usage record as JSON (0208-PLAN P5 Deviation
// 6), and its session_cost is the agent's latest total when known, kept
// through a report without one, and absent when none was ever reported. The
// phone formats it (MADR 0189 D3).
func TestContextReportsTheSessionCost(t *testing.T) {
	cases := []struct {
		name    string
		reports []*event.SessionCost // the session costs of the usage reports, in order
		want    *event.SessionCost
	}{
		{name: "none", reports: []*event.SessionCost{nil}},
		{name: "under a dollar", reports: []*event.SessionCost{usd(0.0234)}, want: usd(0.0234)},
		{name: "free", reports: []*event.SessionCost{usd(0)}, want: usd(0)},
		{name: "other currency", reports: []*event.SessionCost{{Amount: 0.42, Currency: "EUR"}},
			want: &event.SessionCost{Amount: 0.42, Currency: "EUR"}},
		{name: "kept through a report without one", reports: []*event.SessionCost{usd(1.5), nil}, want: usd(1.5)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mgr, p := newNameManager(t, nil)
			meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{}, "owner")
			if err != nil {
				t.Fatal(err)
			}
			for _, cost := range c.reports {
				sendUsage(t, p, cost)
			}
			if err := mgr.cmdContext(meta.ID); err != nil {
				t.Fatal(err)
			}
			ev := lastEngineResult(t, mgr, meta.ID)
			if ev.ResultMethod != "usage_update" {
				t.Errorf("method = %q, want usage_update, the event the record came from", ev.ResultMethod)
			}
			var got event.Usage
			if err := json.Unmarshal([]byte(ev.Text), &got); err != nil {
				t.Fatalf("/context sent %q, not the usage record as JSON: %v", ev.Text, err)
			}
			if got.Used != 1200 || got.Size != 8000 {
				t.Errorf("record = %+v, want used 1200 of 8000", got)
			}
			if !reflect.DeepEqual(got.SessionCost, c.want) {
				t.Errorf("session_cost = %+v, want %+v", got.SessionCost, c.want)
			}
		})
	}
}

// TestContextBeforeAnyReportSaysSo: with no usage record there is nothing to
// send, so /context says so rather than sending an empty record.
func TestContextBeforeAnyReportSaysSo(t *testing.T) {
	mgr, _ := newNameManager(t, nil)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.cmdContext(meta.ID); err != nil {
		t.Fatal(err)
	}
	for _, v := range mgr.History(meta.ID) {
		if v.Type == event.TypeEngineResult {
			t.Fatalf("/context sent %q before any usage report", v.Text)
		}
	}
	var said bool
	for _, v := range mgr.History(meta.ID) {
		said = said || v.Type == event.TypeNotice && strings.Contains(v.Text, "No context report yet")
	}
	if !said {
		t.Error("/context said nothing before any usage report")
	}
}
