package acpagent

import (
	"context"
	"errors"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestListAgentSessionsRequiresTheAdvertisedCapability(t *testing.T) {
	// Gated on the wire, not on the provider's name. An agent that does not
	// advertise session/list is asked for nothing and reports itself
	// unsupported, exactly as before this existed.
	s := &session{}
	_, err := s.ListAgentSessions(context.Background())
	if !errors.Is(err, provider.ErrNotImplemented) {
		t.Fatalf("err = %v, want ErrNotImplemented for an agent with no session/list capability", err)
	}
}

func TestSupportsReadsTheAdvertisedCapabilities(t *testing.T) {
	s := &session{}
	if s.supportsList() || s.supportsDelete() {
		t.Fatal("an empty capability block must advertise nothing")
	}

	// grok 1.0.13: list, resume, close — no delete.
	s.agentCaps = acp.AgentCapabilities{SessionCapabilities: acp.SessionCapabilities{
		List:  &acp.SessionListCapabilities{},
		Close: &acp.SessionCloseCapabilities{},
	}}
	if !s.supportsList() {
		t.Fatal("session/list was advertised and must be read")
	}
	if s.supportsDelete() {
		t.Fatal("session/delete was not advertised and must not be assumed")
	}
}

func TestCurrentModelPrefersTheSessionThenTheEngine(t *testing.T) {
	// Every one of the seven grok turn-latency records in MADR 0138 carried no
	// model, because the manager reads what the *client asked for* and the
	// normal path asks for nothing.
	s := &session{}
	if got := s.CurrentModel(); got != "" {
		t.Fatalf("CurrentModel = %q, want empty when the agent said nothing — never a guess", got)
	}

	s.engineModelID = "grok-4.6"
	if got := s.CurrentModel(); got != "grok-4.6" {
		t.Fatalf("CurrentModel = %q, want the model reported at initialize", got)
	}

	// A session that harvested its own model wins: it is the more specific
	// answer, and a mid-session switch has to be visible.
	s.currentModelID = "grok-4.6-fast"
	if got := s.CurrentModel(); got != "grok-4.6-fast" {
		t.Fatalf("CurrentModel = %q, want the session's own model", got)
	}
}

func TestSessionSatisfiesTheInterfacesPhase8Adds(t *testing.T) {
	// MADR 0138 F7: grok answered "this agent can't" to /compact, /rename,
	// /sessions and /delete — mcremote's limitation, not grok's. This pins the
	// interfaces rather than the count, so a later refactor that drops one
	// fails here by name.
	var s any = &session{}
	for name, ok := range map[string]bool{
		"CompactSession":     assertImpl[provider.CompactSession](s),
		"RenameSession":      assertImpl[provider.RenameSession](s),
		"PurgeSession":       assertImpl[provider.PurgeSession](s),
		"AgentSessionLister": assertImpl[provider.AgentSessionLister](s),
		"ModelReporter":      assertImpl[provider.ModelReporter](s),
	} {
		if !ok {
			t.Errorf("the grok session no longer implements provider.%s", name)
		}
	}
}

func assertImpl[T any](v any) bool {
	_, ok := v.(T)
	return ok
}

func TestExtensionCallsFailClosedOnAClosedSession(t *testing.T) {
	// Every one of these reaches for the connection. A closed session has
	// none, and must say so rather than dereference nil.
	s := &session{closed: true}
	ctx := context.Background()
	if err := s.Compact(ctx); err == nil {
		t.Error("Compact on a closed session must fail")
	}
	if err := s.Rename(ctx, "x"); err == nil {
		t.Error("Rename on a closed session must fail")
	}
	if err := s.Rename(ctx, "   "); err == nil {
		t.Error("Rename with a blank title must fail before it reaches the wire")
	}
}

func TestIsMethodNotFoundReadsTheJSONRPCCode(t *testing.T) {
	// grok publishes no list of its seventy ext methods, so "unsupported" can
	// only be answered by calling and reading -32601. Mistaking any other
	// failure for it would silently hide a real error as a missing feature.
	if !isMethodNotFound(acp.NewMethodNotFound("_x.ai/nope")) {
		t.Fatal("-32601 must read as method-not-found")
	}
	if isMethodNotFound(errors.New("connection reset")) {
		t.Fatal("a transport failure is not a missing method")
	}
	if isMethodNotFound(&acp.RequestError{Code: -32602, Message: "Invalid params"}) {
		t.Fatal("invalid params is not a missing method")
	}
}

func TestParseACPTimeNeverInventsATimestamp(t *testing.T) {
	// An unknown timestamp went on the wire as year 1 once, and the session
	// picker rendered it as an age of about two thousand years (MADR 0046 L-13).
	if got := parseACPTime(nil); !got.IsZero() {
		t.Fatalf("nil = %v, want the zero time", got)
	}
	bad := "not a time"
	if got := parseACPTime(&bad); !got.IsZero() {
		t.Fatalf("unparseable = %v, want the zero time", got)
	}
	good := "2026-09-04T04:44:41Z"
	got := parseACPTime(&good)
	if got.IsZero() || got.Year() != 2026 || got.Location() != time.UTC {
		t.Fatalf("parsed = %v, want 2026 in UTC", got)
	}
}
