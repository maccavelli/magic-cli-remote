package acpagent

import (
	"context"
	"errors"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
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
	// No connection, so no negotiated record: nothing is supported.
	if s := (&session{}); s.supportsList() || s.supportsDelete() {
		t.Fatal("a session with no negotiated record must support nothing")
	}

	// An agent that advertises nothing optional.
	if s, _ := newWireSession(t, `{}`); s.supportsList() || s.supportsDelete() {
		t.Fatal("an empty capability block must advertise nothing")
	}

	// grok 1.0.13: list, resume, close — no delete. Read from the SDK's
	// negotiated record, which its gates also check (MADR 0184 D4).
	s, _ := newWireSession(t, `{"sessionCapabilities":{"list":{},"resume":{},"close":{}}}`)
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

// acp.IsMethodNotFound replaced Magic's own reader (MADR 0207 amendment H, H-6); this pins
// the two meanings Magic relies on.
func TestIsMethodNotFoundReadsTheJSONRPCCode(t *testing.T) {
	// grok publishes no list of its seventy ext methods, so "unsupported" can
	// only be answered by calling and reading -32601. Mistaking any other
	// failure for it would silently hide a real error as a missing feature.
	if !acp.IsMethodNotFound(acp.NewMethodNotFound("_x.ai/nope")) {
		t.Fatal("-32601 must read as method-not-found")
	}
	if acp.IsMethodNotFound(errors.New("connection reset")) {
		t.Fatal("a transport failure is not a missing method")
	}
	if acp.IsMethodNotFound(&acp.RequestError{Code: -32602, Message: "Invalid params"}) {
		t.Fatal("invalid params is not a missing method")
	}
	// The SDK refusing to send an unadvertised method means the same thing
	// (MADR 0184 D5); a refused property or variant does not.
	if !acp.IsMethodNotFound(&acp.CapabilityError{Method: "session/delete", Capability: "sessionCapabilities.delete"}) {
		t.Fatal("an unadvertised method must read as method-not-found")
	}
	if acp.IsMethodNotFound(&acp.CapabilityError{Method: "session/prompt", Capability: "promptCapabilities.image", Variant: "image"}) {
		t.Fatal("a refused variant is not a missing method")
	}
}
