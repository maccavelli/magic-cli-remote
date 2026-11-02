package codex

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestCapabilitiesAreTheFixedSurfaceAndTheModels pins MADR 0208 D1 for codex:
// the app-server's fixed operations, plus the Fast tier and personality when
// the current model has them. Each declared capability has the interface that
// performs it.
func TestCapabilitiesAreTheFixedSurfaceAndTheModels(t *testing.T) {
	fixed := []provider.Capability{
		provider.CapCompact, provider.CapFork, provider.CapDiff, provider.CapRename,
		provider.CapStatus, provider.CapUsage, provider.CapSetModel, provider.CapSetThinkingLevel,
		provider.CapApprovalsReviewer, provider.CapGuardianApprove, provider.CapNativeThreadLifecycle,
		provider.CapExecution, provider.CapGoal, provider.CapReview,
	}
	s := seedFastSession(t, false)
	for _, tc := range []struct {
		model string
		want  []provider.Capability
	}{
		{"gpt-5.6-sol", append([]provider.Capability{provider.CapServiceTier}, fixed...)},
		{"gpt-5.5", append([]provider.Capability{provider.CapPersonality}, fixed...)},
	} {
		s.opts.Model = tc.model
		got := map[provider.Capability]bool{}
		for c, ok := range s.Capabilities() {
			if !ok {
				continue
			}
			if !provider.Implements(s, c) {
				t.Errorf("%s: declares %q without the interface that performs it", tc.model, c)
			}
			got[c] = true
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s: declared %v, want exactly %v", tc.model, got, tc.want)
		}
		for _, c := range tc.want {
			if !got[c] {
				t.Errorf("%s: declared %v, want %q among them", tc.model, got, c)
			}
		}
	}
}
