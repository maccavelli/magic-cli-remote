package provider_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// The absent field is the whole point of the type (MADR 0123 C2). An older
// daemon omits thinking_mutability entirely, and a client that reads the zero
// value as "fixed" would tell the user a provider cannot do something it can —
// which is exactly the defect 0123 F5 recorded and this field exists to end.
func TestThinkingMutabilityAbsentIsUnknownAndSettable(t *testing.T) {
	var meta provider.AgentSessionMeta
	if err := json.Unmarshal([]byte(`{"id":"s1","thinking_level":"medium"}`), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.ThinkingMutability != provider.ThinkingMutabilityUnknown {
		t.Fatalf("absent field decoded to %q, want the unknown zero value",
			meta.ThinkingMutability)
	}
	if meta.ThinkingMutability == provider.ThinkingMutabilityFixed {
		t.Fatal("absent field decoded as fixed — an older daemon would render a false banner")
	}
	if !meta.ThinkingMutability.Settable() {
		t.Fatal("unknown must be settable: assume the provider can, and report the refusal if it cannot")
	}
}

func TestThinkingMutabilityRoundTrips(t *testing.T) {
	for _, m := range []provider.ThinkingMutability{
		provider.ThinkingMutabilityLive,
		provider.ThinkingMutabilityNextTurn,
		provider.ThinkingMutabilityFixed,
	} {
		raw, err := json.Marshal(provider.AgentSessionMeta{ID: "s1", ThinkingMutability: m})
		if err != nil {
			t.Fatal(err)
		}
		var back provider.AgentSessionMeta
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		if back.ThinkingMutability != m {
			t.Fatalf("round trip: got %q want %q (%s)", back.ThinkingMutability, m, raw)
		}
		if !m.Valid() {
			t.Fatalf("%q must be Valid", m)
		}
	}
}

// omitempty on the unknown value keeps the field off the wire entirely, so a
// v1 client sees exactly the payload it saw before this field existed.
func TestThinkingMutabilityUnknownIsOmittedFromTheWire(t *testing.T) {
	raw, err := json.Marshal(provider.AgentSessionMeta{ID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); strings.Contains(got, "thinking_mutability") {
		t.Fatalf("unknown must not be serialised, got %s", got)
	}
}

// Fixed is the only value that withholds the control; everything else offers
// it. Stated as a test so a later edit cannot quietly widen the refusal.
func TestOnlyFixedWithholdsTheControl(t *testing.T) {
	settable := map[provider.ThinkingMutability]bool{
		provider.ThinkingMutabilityUnknown:  true,
		provider.ThinkingMutabilityLive:     true,
		provider.ThinkingMutabilityNextTurn: true,
		provider.ThinkingMutabilityFixed:    false,
	}
	for m, want := range settable {
		if got := m.Settable(); got != want {
			t.Errorf("%q.Settable() = %v, want %v", m, got, want)
		}
	}
	if provider.ThinkingMutability("bogus").Valid() {
		t.Error("an unrecognised value must not report Valid")
	}
}

// Every ThinkingSession must state a value the type recognises. The interface
// makes forgetting a compile error, but it cannot stop a provider returning
// the unknown zero value and being silently read as "assume settable" — which
// is the same silence MADR 0123 F5 came from. Implementations are listed here
// by hand deliberately: a new provider that omits itself is a missing row in a
// review, whereas reflection over the package would pass without anyone
// looking (MADR 0123 P2).
func TestEveryDeclaredMutabilityIsValid(t *testing.T) {
	declared := map[string]provider.ThinkingMutability{
		"acpagent (grok, MADR 0106)":        provider.ThinkingMutabilityLive,
		"codex (turn/start effort)":         provider.ThinkingMutabilityNextTurn,
		"httpagent (opencode/kilo variant)": provider.ThinkingMutabilityLive,
		"fake":                              provider.ThinkingMutabilityLive,
	}
	for name, m := range declared {
		if !m.Valid() {
			t.Errorf("%s: %q is not a recognised value", name, m)
		}
		if m == provider.ThinkingMutabilityUnknown {
			t.Errorf("%s: declared the unknown zero value — state the real behaviour", name)
		}
	}
}
