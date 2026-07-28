//go:build live_codex

package codex

import (
	"context"
	"encoding/json"
	"strings"
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

func liveEngine(t *testing.T) (*conn, func()) {
	t.Helper()
	p := NewWithLogger(Config{Bin: "codex"}, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fr, err := p.ensureEngine(ctx)
	if err != nil {
		t.Fatalf("engine start: %v", err)
	}
	return fr, p.Shutdown
}

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

// TestLiveTurnStartSandboxPolicyShape: turn/start's sandboxPolicy is the *other*
// shape — an object with a camelCase type tag. Pinned here so a future change
// cannot cross-wire the two (MADR 0044 D7).
func TestLiveTurnStartSandboxPolicyShape(t *testing.T) {
	fr, done := liveEngine(t)
	defer done()

	start := func(t *testing.T) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		raw, err := fr.sendRequest(ctx, "thread/start", map[string]any{
			"cwd":            t.TempDir(),
			"sandbox":        "read-only",
			"approvalPolicy": "never",
		})
		if err != nil {
			t.Fatalf("thread/start: %v", err)
		}
		var resp struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("decode thread: %v", err)
		}
		return resp.Thread.ID
	}

	t.Run("object_accepted", func(t *testing.T) {
		id := start(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := fr.sendRequest(ctx, "turn/start", map[string]any{
			"threadId":       id,
			"input":          []map[string]any{{"type": "text", "text": "hi"}},
			"approvalPolicy": "never",
			"sandboxPolicy": map[string]any{
				"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{},
			},
		})
		// A model/auth/network failure is fine — we are asserting the params
		// were accepted, not that the turn produced a reply.
		if err != nil && isParamError(err) {
			t.Errorf("object sandboxPolicy rejected: %v", err)
		}
		ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel2()
		_, _ = fr.sendRequest(ctx2, "turn/interrupt", map[string]any{"threadId": id})
	})

	t.Run("string_rejected", func(t *testing.T) {
		id := start(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := fr.sendRequest(ctx, "turn/start", map[string]any{
			"threadId":      id,
			"input":         []map[string]any{{"type": "text", "text": "hi"}},
			"sandboxPolicy": "workspace-write",
		})
		if err == nil || !isParamError(err) {
			t.Fatalf("string sandboxPolicy should be an invalid-params error, got %v", err)
		}
	})
}

// isParamError reports whether err is JSON-RPC -32600 (invalid request), i.e. a
// wire-shape rejection rather than a model or auth failure.
func isParamError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "-32600") || strings.Contains(s, "Invalid request")
}
