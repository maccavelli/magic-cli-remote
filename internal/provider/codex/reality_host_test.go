//go:build live_codex

package codex

import (
	"context"
	"testing"
)

// TestLiveHostReality reports what this host's Codex credential situation
// actually is. It is diagnostic: it asserts only that the probe reaches a
// conclusion, and prints the conclusion for the operator.
func TestLiveHostReality(t *testing.T) {
	got, err := ObserveCredentialStore(context.Background(), "codex")
	t.Logf("observed codex credential reality on this host: %s (err=%v)", got, err)
	if got == RealityUnknown {
		t.Fatal("the probe could not reach a conclusion with a runnable codex binary")
	}
	if d := describeReality(got); d != "" {
		t.Logf("operator explanation: %s", d)
	}
}
