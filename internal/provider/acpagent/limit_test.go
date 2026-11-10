package acpagent

import (
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// quotaError has the shape of an agent's captured quota error (MADR 0243
// F1), with vendor names kept out, as acpagent's tests keep them: a
// JSON-RPC wrapper, the agent's and the SDK's prefixes, the SDK header with a
// free-tier code and a Go-duration retry-after, and the provider's words.
const quotaError = `{"code":-32603,"message":"Internal error","data":{"error":"llm: api status 429: llm: quota exhausted: llmprovider: quota exhausted: vendor-a/chat_completions HTTP 429 FreeUsageLimitError (retry-after 1h33m21s): Free usage limit reached. Subscribe to keep going."}}`

// errorEvent emits a failed turn's error for raw and returns its error event.
func errorEvent(t *testing.T, s *session, raw string) event.Event {
	t.Helper()
	s.emitClassifiedTurnErrorWith(raw, "")
	for range 3 {
		if ev := recvEvent(t, s.events); ev.Type == event.TypeError {
			return ev
		}
	}
	t.Fatal("no error event")
	return event.Event{}
}

// TestErrorEventCarriesTheLimit is MADR 0243 D5: a failed turn's error event
// carries the limit agenterr read from the text.
func TestErrorEventCarriesTheLimit(t *testing.T) {
	ev := errorEvent(t, newBareSession(8), quotaError)
	if ev.ErrorKind != "quota" || ev.LimitScope != agenterr.ScopeFreeTier || ev.LimitProvider != "vendor-a" ||
		ev.LimitCode != "FreeUsageLimitError" || ev.RetryAt.IsZero() {
		t.Fatalf("event = kind %q scope %q provider %q code %q retry %v; want the free-tier limit",
			ev.ErrorKind, ev.LimitScope, ev.LimitProvider, ev.LimitCode, ev.RetryAt)
	}
}

// TestBindSpecCarriesErrorLimit: the Spec's ErrorLimit reaches the session,
// as Start binds it.
func TestBindSpecCarriesErrorLimit(t *testing.T) {
	s := newBareSession(8)
	(&Provider{spec: Spec{ErrorLimit: func(string) (agenterr.Limit, time.Time, bool) {
		return agenterr.Limit{Scope: agenterr.ScopeMonthly}, time.Time{}, true
	}}}).bindSpec(s)
	if ev := errorEvent(t, s, quotaError); ev.LimitScope != agenterr.ScopeMonthly {
		t.Fatalf("scope = %q, want the bound hook's monthly", ev.LimitScope)
	}
}

// TestErrorLimitHookWins is D3 source 1: a Spec's structured limit data
// replaces the text's reading, its reset replaces the text's, and the message
// is the agent's provider words.
func TestErrorLimitHookWins(t *testing.T) {
	reset := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	s := newBareSession(8)
	s.errorLimit = func(string) (agenterr.Limit, time.Time, bool) {
		return agenterr.Limit{Scope: agenterr.ScopeWeekly, Provider: "vendor", ProviderMessage: "Weekly limit reached on your plan."}, reset, true
	}
	ev := errorEvent(t, s, quotaError)
	if ev.LimitScope != agenterr.ScopeWeekly || ev.LimitProvider != "vendor" || !ev.RetryAt.Equal(reset) {
		t.Errorf("event = scope %q provider %q retry %v; want the hook's weekly, vendor, %v", ev.LimitScope, ev.LimitProvider, ev.RetryAt, reset)
	}
	if ev.LimitCode != "FreeUsageLimitError" {
		t.Errorf("code = %q; a field the hook left empty keeps the text's", ev.LimitCode)
	}
	if ev.Error != "Weekly limit reached on your plan. Resets around "+reset.Local().Format("Jan 2")+" (about 3 days)." {
		t.Errorf("message = %q, want the hook's provider words and the hook's reset", ev.Error)
	}

	s = newBareSession(8)
	s.errorLimit = func(string) (agenterr.Limit, time.Time, bool) { return agenterr.Limit{Scope: "x"}, time.Time{}, false }
	if ev := errorEvent(t, s, quotaError); ev.LimitScope != agenterr.ScopeFreeTier {
		t.Errorf("ok false: scope = %q, want the text's free_tier", ev.LimitScope)
	}
}
