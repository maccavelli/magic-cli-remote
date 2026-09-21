package codex

import (
	"slices"
	"testing"
	"time"
)

// negotiatedProvider returns a provider whose engine negotiated `experimental`
// the way the argument says, with the real embedded manifest behind it.
func negotiatedProvider(t *testing.T, experimental bool) (*Provider, *capabilityState) {
	t.Helper()
	m, err := loadEmbeddedContractManifest()
	if err != nil {
		t.Fatalf("loading embedded contract: %v", err)
	}
	snapshot, err := buildCapabilitySnapshot(m, BinaryIdentity{}, 1, experimental, time.Now())
	if err != nil {
		t.Fatalf("building snapshot: %v", err)
	}
	state := newCapabilityState(snapshot)
	p := New(Config{})
	p.eng = &engine{capabilities: state, experimental: experimental, ready: true}
	return p, state
}

// TestAdvertisedSurfaceFollowsTheNegotiatedEngine is acceptance criterion A23 of
// PLAN 0163.
//
// The manifest says what the pinned binary can do. It is not a statement about
// the engine we are actually talking to, and advertising it unconditionally
// promises the phone operations the daemon will then refuse — discoverable only
// by trying and failing.
func TestAdvertisedSurfaceFollowsTheNegotiatedEngine(t *testing.T) {
	manifestStable, manifestExperimental := SurfaceCapabilityIDs()
	if len(manifestStable) == 0 || len(manifestExperimental) == 0 {
		t.Fatalf("manifest surface looks empty (%d stable, %d experimental); the rest of this test proves nothing",
			len(manifestStable), len(manifestExperimental))
	}

	t.Run("no engine falls back to the manifest", func(t *testing.T) {
		if _, _, negotiated := New(Config{}).NegotiatedSurfaceCapabilityIDs(); negotiated {
			t.Error("a provider with no engine claimed a negotiated surface; " +
				"the caller must fall back to the manifest instead of advertising nothing")
		}
	})

	t.Run("experimental accepted advertises both halves", func(t *testing.T) {
		p, _ := negotiatedProvider(t, true)
		stable, experimental, negotiated := p.NegotiatedSurfaceCapabilityIDs()
		if !negotiated {
			t.Fatal("a running engine must report a negotiated surface")
		}
		if !slices.Equal(stable, manifestStable) {
			t.Errorf("stable surface = %d ids, want the manifest's %d", len(stable), len(manifestStable))
		}
		if !slices.Equal(experimental, manifestExperimental) {
			t.Errorf("experimental surface = %d ids, want the manifest's %d", len(experimental), len(manifestExperimental))
		}
	})

	t.Run("experimental refused advertises no experimental capability", func(t *testing.T) {
		p, _ := negotiatedProvider(t, false)
		stable, experimental, negotiated := p.NegotiatedSurfaceCapabilityIDs()
		if !negotiated {
			t.Fatal("a running engine must report a negotiated surface")
		}
		if len(experimental) != 0 {
			t.Errorf("advertised %d experimental capabilities to an engine that refused experimental: %v",
				len(experimental), experimental)
		}
		// The stable half must survive: shrinking it too would be a different bug,
		// and would make the phone think the daemon can do almost nothing.
		if !slices.Equal(stable, manifestStable) {
			t.Errorf("stable surface = %d ids, want the manifest's %d; refusing experimental must not shrink the stable half",
				len(stable), len(manifestStable))
		}
	})

	t.Run("a disabled capability disappears and comes back", func(t *testing.T) {
		p, state := negotiatedProvider(t, true)
		now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
		state.now = func() time.Time { return now }

		const victim = CapabilityThreadSettings
		before, _, _ := p.NegotiatedSurfaceCapabilityIDs()
		beforeAll := append(append([]string{}, before...), secondHalf(t, p)...)
		if !slices.Contains(beforeAll, string(victim)) {
			t.Fatalf("%s is not advertised to begin with; nothing to disable", victim)
		}

		state.Disable(victim, DenialMethodNotFound)
		stable, experimental, _ := p.NegotiatedSurfaceCapabilityIDs()
		if slices.Contains(stable, string(victim)) || slices.Contains(experimental, string(victim)) {
			t.Errorf("%s is still advertised after being disabled", victim)
		}

		// After the retry window a real use re-probes and re-enables it, and the
		// advertisement follows (MADR 0163 D10).
		now = now.Add(capabilityRetryAfter + time.Second)
		if !state.Supports(victim) {
			t.Fatalf("%s did not come back after the retry window", victim)
		}
		stable, experimental, _ = p.NegotiatedSurfaceCapabilityIDs()
		if !slices.Contains(stable, string(victim)) && !slices.Contains(experimental, string(victim)) {
			t.Errorf("%s did not reappear in the advertisement after re-probing", victim)
		}
	})
}

// secondHalf returns the experimental half, so a caller can search both lists.
func secondHalf(t *testing.T, p *Provider) []string {
	t.Helper()
	_, experimental, _ := p.NegotiatedSurfaceCapabilityIDs()
	return experimental
}
