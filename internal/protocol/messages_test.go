package protocol_test

import (
	"encoding/json"
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
