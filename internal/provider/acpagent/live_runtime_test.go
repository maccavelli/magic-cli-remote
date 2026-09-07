//go:build live_grok

package acpagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// MADR 0138 Phase 10, acceptances 5 and 6.
//
// Two tests and not one, because their costs are not comparable:
//
//	TestLiveGrokSessionUsage… reads an in-process ledger. No network, no auth,
//	                          no model tokens.
//	TestLiveGrokBilling…      leaves the machine: grok fetches it over HTTP from
//	                          its CLI chat proxy using the operator's own
//	                          credentials.
//
// Bundling them would make the free check hostage to the costly one.
//
// Both decode strictly. An ordinary decode of a renamed or restructured
// response yields zero values and no error, which is the exact failure these
// acceptances exist to rule out.

// startLiveGrok brings up a real grok in a temp directory, without prompting.
func startLiveGrok(t *testing.T) *session {
	t.Helper()

	p := New(Spec{
		ID:         provider.IDGrok,
		DefaultBin: "grok",
		DefaultArgs: func(Config) []string {
			return []string{"--no-auto-update", "agent", "--no-leader", "stdio"}
		},
	}, Config{})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	sess, err := p.Start(ctx, provider.StartOptions{Name: "runtime-probe", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start grok: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close(context.Background()) })

	s, ok := sess.(*session)
	if !ok {
		t.Fatalf("grok session is %T, not *session", sess)
	}
	if _, ok := sess.(provider.RuntimeSession); !ok {
		t.Fatal("the live grok session does not satisfy provider.RuntimeSession")
	}
	return s
}

// strictDecode fails the test when raw carries a field the struct does not know.
func strictDecode(t *testing.T, raw json.RawMessage, into any, what string) error {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	t.Logf("%s decoded strictly", what)
	return nil
}

// TestLiveGrokSessionUsageDecodesStrictly is acceptance 5.
//
// Run with: go test -tags live_grok ./internal/provider/acpagent/ -run LiveGrokSessionUsage -count=1
//
// Spends no model tokens and makes no network call: x.ai/session/usage reads
// the agent's own in-memory UsageLedger.
func TestLiveGrokSessionUsageDecodesStrictly(t *testing.T) {
	s := startLiveGrok(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var raw json.RawMessage
	if err := callAgentExtension(ctx, s, "x.ai/session/usage",
		sessionUsageRequest{SessionID: s.AgentSessionID()}, &raw); err != nil {
		if errors.Is(err, provider.ErrNotImplemented) {
			t.Fatalf("this grok build has no _x.ai/session/usage; /usage cannot work on it: %v", err)
		}
		t.Fatalf("_x.ai/session/usage: %v", err)
	}
	t.Logf("_x.ai/session/usage returned: %s", raw)

	var res sessionUsageResponse
	if err := strictDecode(t, raw, &res, "session usage"); err != nil {
		t.Fatalf("the response does not match sessionUsageResponse — the transcribed shape has "+
			"drifted from the installed binary: %v", err)
	}

	// A session that has never prompted has an empty ledger, which is the
	// expected result here and is not a failure. What is asserted is that the
	// shape decodes and that RuntimeUsage renders it without claiming a cost
	// it was not given.
	msg, err := s.RuntimeUsage(ctx)
	if err != nil {
		t.Fatalf("RuntimeUsage: %v", err)
	}
	t.Logf("/usage would show: %s", msg)
	if res.Usage.CostUsdTicks == nil && !bytes.Contains([]byte(msg), []byte("cost unavailable")) {
		t.Errorf("the ledger reported no cost but /usage said %q", msg)
	}
}

// TestLiveGrokBillingDecodesStrictly is acceptance 6.
//
// Run with: go test -tags live_grok ./internal/provider/acpagent/ -run LiveGrokBilling -count=1
//
// **This contacts xAI's backend with the operator's credentials.** It spends no
// model tokens, but it is not a local read, and it runs only deliberately.
//
// Two outcomes pass, and which one occurred is what the run records:
//
//   - a strict decode, on an install authenticated with grok.com; or
//   - ACP -32000, on an API-key install, which cannot satisfy grok's
//     require_xai_auth gate and is a supported configuration here.
//
// A third outcome — a decode error — is the failure this exists to catch.
func TestLiveGrokBillingDecodesStrictly(t *testing.T) {
	s := startLiveGrok(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var raw json.RawMessage
	err := callAgentExtension(ctx, s, "x.ai/billing", map[string]any{}, &raw)
	switch {
	case err == nil:
		t.Logf("_x.ai/billing returned: %s", raw)
		var res billingResponse
		if decErr := strictDecode(t, raw, &res, "billing"); decErr != nil {
			t.Fatalf("the response does not match billingResponse — the transcribed shape has "+
				"drifted from the installed binary: %v", decErr)
		}
		t.Logf("OUTCOME: authenticated. /status would show: %s", formatBilling(res))
	case isAuthRequired(err):
		t.Logf("OUTCOME: this install cannot read billing (ACP -32000): %v", err)
		msg, statusErr := s.RuntimeStatus(ctx)
		if statusErr != nil {
			t.Fatalf("RuntimeStatus turned an auth failure into an error: %v", statusErr)
		}
		t.Logf("/status would show: %s", msg)
	case errors.Is(err, provider.ErrNotImplemented):
		t.Fatalf("this grok build has no _x.ai/billing: %v", err)
	default:
		t.Fatalf("_x.ai/billing: %v", err)
	}
}
