package protocol_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// The wire-shape guard for MADR 0074 D4. The auth block is additive: a daemon
// with nothing to report — or a connection that never negotiated the
// provider_auth capability — must not grow an `"auth":{...}` object on every
// listing. A non-pointer field or a missing omitempty would break v1 clients.
//
// `prewarm` is the one deliberate addition to the baseline (MADR 0089 D7): it
// is encoded unconditionally, precisely so a client can tell a daemon that
// reports `false` from an older one that omits the key entirely. Old clients
// ignore the unknown field. Adding any *other* always-on field here needs the
// same explicit justification — that is what this byte-exact assertion is for.
func TestProviderInfoPayloadV1WireCompat(t *testing.T) {
	b, err := json.Marshal(protocol.ProviderInfoPayload{ID: "kilo", Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"id":"kilo","ready":true,"prewarm":false}`
	if string(b) != want {
		t.Fatalf("v1 provider entry changed shape:\n got %s\nwant %s", b, want)
	}
}

// With the block present the field appears — proving omitempty is not hiding a
// populated value.
func TestProviderInfoPayloadWithAuth(t *testing.T) {
	b, err := json.Marshal(protocol.ProviderInfoPayload{
		ID:    "kilo",
		Ready: true,
		Auth: &protocol.ProviderAuthPayload{
			Status:         protocol.AuthStatusConfigured,
			ActiveUpstream: "kilo",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"auth":{"status":"configured","active_upstream":"kilo"}`)) {
		t.Fatalf("auth block missing or reshaped: %s", b)
	}
}

// MADR 0074 D2/D11: a credential must never be renderable. This covers every
// path a secret could plausibly escape through — structured logging, the fmt
// verbs, and the error-formatting habit of printing a whole payload.
func TestSetCredentialPayloadNeverRendersSecret(t *testing.T) {
	const secret = "sk-live-DO-NOT-LEAK-abcdef0123456789"
	p := protocol.SetCredentialPayload{
		ProviderID: "opencode",
		UpstreamID: "opencode-go",
		MethodID:   "api",
		Secret:     secret,
		Inputs:     map[string]string{"accountId": "acct-1"},
	}

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("probe", "payload", p)

	renders := map[string]string{
		"slog":    buf.String(),
		"fmt %v":  fmt.Sprintf("%v", p),
		"fmt %s":  string(fmt.Appendf(nil, "%s", p)),
		"fmt %+v": fmt.Sprintf("%+v", p),
		"fmt ptr": fmt.Sprintf("%v", &p),
		"errorf":  fmt.Errorf("set credential failed: %v", p).Error(),
	}
	for name, out := range renders {
		if strings.Contains(out, secret) {
			t.Errorf("%s leaked the secret: %s", name, out)
		}
		if !strings.Contains(out, "[redacted]") {
			t.Errorf("%s did not mark the secret redacted: %s", name, out)
		}
	}
}

// The secret still has to survive the wire — redaction is for rendering, not
// for transport. If this ever stops holding, credential injection silently
// sends an empty key.
func TestSetCredentialPayloadJSONRoundTrip(t *testing.T) {
	const secret = "sk-live-abc"
	b, err := json.Marshal(protocol.SetCredentialPayload{ProviderID: "kilo", Secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.SetCredentialPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Secret != secret {
		t.Fatalf("secret did not survive the round trip: %q", got.Secret)
	}
}

// MADR 0074 D6: the capability is additive in exactly the way Receipts is, so
// a daemon without the feature emits no key at all.
func TestCapsProviderAuthOmitEmpty(t *testing.T) {
	off, err := json.Marshal(protocol.Caps{Protocol: protocol.V2})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(off, []byte("provider_auth")) {
		t.Fatalf("capability advertised while disabled: %s", off)
	}
	on, err := json.Marshal(protocol.Caps{Protocol: protocol.V2, ProviderAuth: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(on, []byte(`"provider_auth":true`)) {
		t.Fatalf("capability missing while enabled: %s", on)
	}
}

// Inputs are the part of D5 most likely to be dropped by a well-meaning
// simplification, so pin that they survive JSON in both directions.
func TestAuthMethodInputsRoundTrip(t *testing.T) {
	in := protocol.AuthMethodPayload{
		ID:    "github-copilot:0",
		Type:  protocol.AuthMethodOAuthDevice,
		Label: "Login with GitHub Copilot",
		Inputs: []protocol.AuthInputPayload{
			{Key: "deploymentType", Type: protocol.AuthInputSelect, Options: []protocol.AuthInputOptionPayload{{Value: "github.com"}, {Value: "enterprise"}}},
			{Key: "enterpriseUrl", Type: protocol.AuthInputText, Placeholder: "https://…"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.AuthMethodPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Inputs) != 2 {
		t.Fatalf("inputs lost in transit: %+v", got)
	}
	if len(got.Inputs[0].Options) != 2 {
		t.Fatalf("select options lost: %+v", got.Inputs[0])
	}
}
