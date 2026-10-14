package session

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// PLAN 0189 C2: the daemon keeps an agent's latest session cost, persists it,
// and reports it in /context (MADR 0189 D2).

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

// lastNotice returns the text of the session's most recent notice.
func lastNotice(mgr *Manager, id string) string {
	h := mgr.History(id)
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Type == event.TypeNotice {
			return h[i].Text
		}
	}
	return ""
}

func TestContextReportsTheSessionCost(t *testing.T) {
	cases := []struct {
		name string
		cost *event.SessionCost
		want string // "" means no cost suffix at all
	}{
		{name: "none"},
		{name: "under a dollar", cost: usd(0.0234), want: " · session cost $0.0234"},
		{name: "a dollar or more", cost: usd(1.5), want: " · session cost $1.50"},
		{name: "free", cost: usd(0), want: " · session cost $0.0000"},
		{name: "other currency", cost: &event.SessionCost{Amount: 0.42, Currency: "EUR"},
			want: " · session cost 0.4200 EUR"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mgr, p := newNameManager(t, nil)
			meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{}, "owner")
			if err != nil {
				t.Fatal(err)
			}
			sendUsage(t, p, c.cost)
			if err := mgr.cmdContext(meta.ID); err != nil {
				t.Fatal(err)
			}
			got := lastNotice(mgr, meta.ID)
			if !strings.HasPrefix(got, "Context: 1,200 tokens of 8,000 (15%)") {
				t.Fatalf("notice = %q", got)
			}
			switch {
			case c.want == "" && strings.Contains(got, "session cost"):
				t.Fatalf("notice = %q, want no cost when none was reported", got)
			// The session has no model or mode, so the cost ends the line;
			// a suffix check keeps "$1.5000" from passing for "$1.50".
			case c.want != "" && !strings.HasSuffix(got, c.want):
				t.Fatalf("notice = %q, want it to end with %q", got, c.want)
			}
		})
	}
}
