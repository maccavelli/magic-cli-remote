//go:build live_codex

package codex

import (
	"context"
	"testing"
	"time"
)

// The sandbox/approval overrides are the one place where the fake and the real
// engine drifted (MADR 0044 Finding 5): the fixture asserted an object-shaped
// `sandbox` that codex rejects outright, so the unit tests were green while
// every session with providers.codex.sandbox_mode set failed to start.
//
// These tests pin both shapes against a real engine, in both directions —
// asserting that the wrong shape is *rejected* is what stops the fake from
// silently drifting back.

// TestLiveThreadStartSandboxShape: thread/start takes SandboxMode as a plain
// kebab-case string. The object form must fail — that is the bug this guards.
func TestLiveThreadStartSandboxShape(t *testing.T) {
	fr, done := liveEngine(t)
	defer done()

	t.Run("string_accepted", func(t *testing.T) {
		for _, mode := range []string{"read-only", "workspace-write", "danger-full-access"} {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			_, err := fr.sendRequest(ctx, "thread/start", map[string]any{
				"cwd":            t.TempDir(),
				"sandbox":        mode,
				"approvalPolicy": "never",
			})
			cancel()
			if err != nil {
				t.Errorf("sandbox=%q as string: %v", mode, err)
			}
		}
	})

	t.Run("object_rejected", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := fr.sendRequest(ctx, "thread/start", map[string]any{
			"cwd":     t.TempDir(),
			"sandbox": map[string]any{"type": "read-only", "networkAccess": false},
		})
		if err == nil {
			t.Fatal("object-shaped sandbox was accepted; thread/start may now take " +
				"both shapes — re-check applyPolicyParams and MADR 0044 Finding 5")
		}
	})

	t.Run("unknown_variant_rejected", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := fr.sendRequest(ctx, "thread/start", map[string]any{
			"cwd":     t.TempDir(),
			"sandbox": "totally-bogus",
		})
		if err == nil {
			t.Fatal("bogus sandbox mode was accepted; the server is not validating")
		}
	})
}
