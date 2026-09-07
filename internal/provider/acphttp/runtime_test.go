package acphttp

import (
	"context"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestGooseImplementsRuntimeSession is acceptance 4's precondition.
func TestGooseImplementsRuntimeSession(t *testing.T) {
	var s any = &session{}
	if _, ok := s.(provider.RuntimeSession); !ok {
		t.Fatal("the acphttp session does not implement provider.RuntimeSession")
	}
}

// TestGooseUsageNeverClaimsACost is Q6.
//
// goose sends `used` and `size` on the standard ACP usage update and nothing
// else — no cost, no per-model split, no turn count. A "$0.00" would read as a
// free session rather than as an unreported one, which is the same distinction
// Phase 10 drew for grok's absent costUsdTicks.
func TestGooseUsageNeverClaimsACost(t *testing.T) {
	s := &session{}
	s.usage.record(4096, 128000)

	got, err := s.RuntimeUsage(context.Background())
	if err != nil {
		t.Fatalf("RuntimeUsage: %v", err)
	}
	if strings.Contains(got, "$") {
		t.Errorf("usage = %q — goose reports no cost, so a figure here is invented", got)
	}
	if !strings.Contains(got, "reports no cost") {
		t.Errorf("usage = %q, want it to say the cost is unreported rather than omit it", got)
	}
	for _, want := range []string{"4096", "128000", "3%"} {
		if !strings.Contains(got, want) {
			t.Errorf("usage = %q, want %q", got, want)
		}
	}
}

// TestGooseUsageDistinguishesUnreportedFromZero.
//
// Before the first turn there is no usage update at all. Reporting "0 tokens"
// then would be a measurement nobody made.
func TestGooseUsageDistinguishesUnreportedFromZero(t *testing.T) {
	fresh := &session{}
	got, err := fresh.RuntimeUsage(context.Background())
	if err != nil {
		t.Fatalf("RuntimeUsage: %v", err)
	}
	if !strings.Contains(got, "has not reported") {
		t.Errorf("usage = %q, want it to say goose has reported nothing yet", got)
	}

	// A genuine zero is a different statement and must be shown as one.
	zero := &session{}
	zero.usage.record(0, 128000)
	got, _ = zero.RuntimeUsage(context.Background())
	if strings.Contains(got, "has not reported") {
		t.Errorf("usage = %q — a reported zero was shown as unreported", got)
	}
	if !strings.Contains(got, "0 tokens") {
		t.Errorf("usage = %q, want the reported zero", got)
	}
}

// TestGooseStatusReadsTheModelFromConfigOptions.
//
// goose's config options *are* its model catalog (MADR 0043 D6), so that is
// where the current model lives; there is no endpoint to ask.
func TestGooseStatusReadsTheModelFromConfigOptions(t *testing.T) {
	s := &session{agentID: "sess-1"}
	s.configOpts = []event.ConfigOption{
		{ID: "provider", Kind: "select", CurrentValue: "anthropic"},
		{ID: "model", Kind: "select", CurrentValue: "claude-sonnet-5"},
	}

	got, err := s.RuntimeStatus(context.Background())
	if err != nil {
		t.Fatalf("RuntimeStatus: %v", err)
	}
	for _, want := range []string{"Goose", "claude-sonnet-5", "sess-1", "no plan usage"} {
		if !strings.Contains(got, want) {
			t.Errorf("status = %q, want %q", got, want)
		}
	}

	// Before the agent sends its options there is no model to name, and the
	// line must still be useful rather than carrying an empty "model ".
	bare := &session{}
	got, _ = bare.RuntimeStatus(context.Background())
	if strings.Contains(got, "model ") {
		t.Errorf("status = %q — named a model it does not know", got)
	}
}

// TestGooseStatusAndUsageNeverError is the same claim Phase 10's P4 made for
// grok: cmdRuntime propagates an error to its caller instead of showing it, so
// an error here makes the slash command silently do nothing.
func TestGooseStatusAndUsageNeverError(t *testing.T) {
	s := &session{}
	if _, err := s.RuntimeStatus(context.Background()); err != nil {
		t.Errorf("RuntimeStatus: %v", err)
	}
	if _, err := s.RuntimeUsage(context.Background()); err != nil {
		t.Errorf("RuntimeUsage: %v", err)
	}
}
