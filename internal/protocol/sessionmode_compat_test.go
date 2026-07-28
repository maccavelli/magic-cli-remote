package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// SessionMode.Dangerous is an additive wire field (MADR 0044). These guards
// cover both directions of the compatibility promise made in protocol-v1.md:
// an old client can ignore it, and an old daemon that never sends it produces
// the safe default.

func TestSessionModeDangerousIsOmittedWhenFalse(t *testing.T) {
	raw, err := json.Marshal(event.SessionMode{ID: "build", Name: "build"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "dangerous") {
		t.Fatalf("an ordinary mode must not carry the field at all: %s", raw)
	}
}

func TestSessionModeDangerousRoundTrips(t *testing.T) {
	in := event.SessionMode{
		ID: "auto", Name: "auto", Description: "Auto-approve", Dangerous: true,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"dangerous":true`) {
		t.Fatalf("dangerous mode did not serialize the flag: %s", raw)
	}
	var out event.SessionMode
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round-trip = %+v, want %+v", out, in)
	}
}

// A payload from a daemon that predates the field must decode to the safe
// default rather than failing or defaulting to "dangerous".
func TestSessionModeFromOlderDaemonDecodesSafely(t *testing.T) {
	var m event.SessionMode
	if err := json.Unmarshal([]byte(`{"id":"auto","name":"Auto"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.Dangerous {
		t.Fatal("an absent flag must mean not-dangerous; defaulting the other " +
			"way would alarm on goose's normal state")
	}
}

// An unknown future field must not break decoding — the same forward
// compatibility the field itself relies on.
func TestSessionModeIgnoresUnknownFields(t *testing.T) {
	var m event.SessionMode
	err := json.Unmarshal([]byte(`{"id":"auto","name":"Auto","future":"x"}`), &m)
	if err != nil {
		t.Fatalf("unknown field broke decoding: %v", err)
	}
	if m.ID != "auto" {
		t.Fatalf("id = %q", m.ID)
	}
}

// TestSessionModeDangerousIsDocumented mirrors the doc-coverage guards in this
// package: the existing ones cover event types and error codes, so a new
// *field* has nothing checking it. This closes that specific gap.
func TestSessionModeDangerousIsDocumented(t *testing.T) {
	doc := readProtocolDoc(t)
	if !strings.Contains(doc, "`dangerous`") {
		t.Error("SessionMode.dangerous is a wire field and must be described in " +
			"the protocol spec — clients implement against it")
	}
}
