package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	env, err := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: "mcr_x"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.Envelope
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != protocol.TypeAuth || got.ID != "1" || got.V != protocol.Version {
		t.Fatalf("got=%+v", got)
	}
	var p protocol.AuthPayload
	if err := protocol.DecodePayload(got, &p); err != nil {
		t.Fatal(err)
	}
	if p.Token != "mcr_x" {
		t.Fatalf("token=%q", p.Token)
	}
}

func TestSessionForkPayloadLastTurnID(t *testing.T) {
	raw := []byte(`{"session_id":"s1","last_turn_id":"turn-9"}`)
	var p protocol.SessionForkPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if p.SessionID != "s1" || p.LastTurnID != "turn-9" || p.MessageID != "" {
		t.Fatalf("%+v", p)
	}
}

func TestSessionDiffResultEnvelopeUnder1MiB(t *testing.T) {
	// 256 KiB clipped patch plus metadata must stay well under the 1 MiB
	// outbound WebSocket cap (MADR 0080 D15).
	const maxOutbound = 1 << 20
	body := strings.Repeat("diff --git a/x b/x\n", 20000)
	env, err := protocol.NewEnvelope(protocol.TypeSessionDiffResult, "d1", protocol.SessionDiffResultPayload{
		SessionID: "s1",
		Summary:   body,
		BaseSHA:   strings.Repeat("a", 64),
		Scope:     "working_tree",
		Truncated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) >= maxOutbound {
		t.Fatalf("envelope %d bytes exceeds %d", len(b), maxOutbound)
	}
}
