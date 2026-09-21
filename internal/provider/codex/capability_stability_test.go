package codex

import (
	"strings"
	"testing"
)

// TestStableMethodsAreNotGatedAsExperimental guards the invariant behind PLAN
// 0163's 2026-09-21 deviation: capability stability must follow the engine's own
// schema, not a hand-maintained list.
//
// The failure it prevents is quiet. An experimental capability is denied outright
// when the engine does not accept `experimental` (capabilities.go:195), and each
// carries a fallback to an older method — so a method that upstream has promoted
// to stable, but that we still label experimental, keeps silently routing to its
// fallback in exactly the configuration nobody tests. That is how
// thread/items/list and thread/turns/list came to point at the deprecated
// thread/read long after #40673 promoted them.
//
// Asserting over the whole manifest rather than those two methods is the point:
// the next promotion is caught without anyone remembering to add it here.
func TestStableMethodsAreNotGatedAsExperimental(t *testing.T) {
	manifest, err := loadEmbeddedContractManifest()
	if err != nil {
		t.Fatalf("loading embedded contract: %v", err)
	}

	stableMethods := make(map[string]struct{}, len(manifest.Stable.ClientRequests))
	for _, request := range manifest.Stable.ClientRequests {
		stableMethods[request.Method] = struct{}{}
	}

	var mislabelled, withFallback []string
	for _, capability := range manifest.Capabilities {
		// Only "rpc:" capabilities are the method itself. A "field:" capability
		// gates an experimental FIELD on a method that may well be stable —
		// field:thread/fork.deferGoalContinuation is experimental while thread/fork
		// is stable, and both facts are correct. Checking those here would demand
		// the manifest lie about one of them.
		if !strings.HasPrefix(string(capability.ID), "rpc:") {
			continue
		}
		method := ""
		for _, requirement := range capability.Requires {
			if requirement.Kind == WireClientRequest {
				method = requirement.Method
				break
			}
		}
		// A capability spanning several requirements (thread/start + thread/fork)
		// is a composite we assemble by hand, not a single promoted method.
		if method == "" || len(capability.Requires) != 1 {
			continue
		}
		if _, isStable := stableMethods[method]; !isStable {
			continue
		}
		if capability.Stability != StabilityStable {
			mislabelled = append(mislabelled, string(capability.ID))
		}
		if capability.Fallback != "" {
			withFallback = append(withFallback, string(capability.ID))
		}
	}

	if len(mislabelled) > 0 {
		t.Errorf("capabilities marked %q although the stable bundle declares their method: %v\n"+
			"Stability is derived in generatedCapabilities from which schema bundle declares the "+
			"method; regenerate the contract rather than editing the manifest.",
			StabilityExperimental, mislabelled)
	}
	// A stable capability cannot be taken away by negotiation, so a fallback on one
	// is unreachable code that reads like a supported degradation path.
	if len(withFallback) > 0 {
		t.Errorf("stable capabilities declare a fallback, which can never be reached: %v", withFallback)
	}
}
