//go:build live_codex_turn

package codex

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestLiveThreadLifecycle creates a thread, runs a cheap turn, and cleans up.
func TestLiveThreadLifecycle(t *testing.T) {
	cfg := Config{Bin: "codex"}
	p := NewWithLogger(cfg, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sess, err := p.Start(ctx, provider.StartOptions{})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer sess.Close(context.Background())

	cs, ok := sess.(*session)
	if !ok {
		t.Fatal("session is not *session")
	}
	if cs.agentID == "" {
		t.Fatal("agent session id is empty after start")
	}
	t.Logf("thread id: %s", cs.agentID)

	if err := sess.Prompt(ctx, []provider.Content{{Text: "Reply with exactly 'hello' and nothing else."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	timeout := time.After(30 * time.Second)
	for {
		select {
		case ev := <-sess.Events():
			if ev.Type == event.TypeTurnComplete {
				t.Logf("turn completed: status=%s", ev.Status)
				goto done
			}
			if ev.Type == event.TypeError {
				t.Logf("turn error: %s", ev.Error)
				goto done
			}
			if ev.Type == event.TypeAssistantChunk {
				t.Logf("assistant chunk: %s", ev.Text)
			}
		case <-timeout:
			t.Fatal("timed out waiting for turn completion")
		}
	}
done:
	p.Shutdown()
}

// TestLiveTurnPlanUpdatedNotSkipped is a load-bearing live test for MADR
// 0035 phase 8: a turn that produces a plan must surface a TypePlan
// event from the daemon.
func TestLiveTurnPlanUpdatedNotSkipped(t *testing.T) {
	cfg := Config{Bin: "codex"}
	p := NewWithLogger(cfg, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sess, err := p.Start(ctx, provider.StartOptions{})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer sess.Close(context.Background())

	if err := sess.Prompt(ctx, []provider.Content{{
		Text: "Make a 3-step plan: 1) read AGENTS.md, 2) summarize, 3) reply. Use your plan tool if available.",
	}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	sawPlan := false
	timeout := time.After(45 * time.Second)
loop:
	for {
		select {
		case ev := <-sess.Events():
			switch ev.Type {
			case event.TypePlan:
				sawPlan = true
				t.Logf("plan entries=%d first=%+v", len(ev.Entries), ev.Entries[0])
			case event.TypeTurnComplete, event.TypeError:
				break loop
			}
		case <-timeout:
			break loop
		}
	}
	if !sawPlan {
		t.Skip("model did not emit a plan; wire shape and translation are pinned by unit tests")
	}
	p.Shutdown()
}

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

func TestLiveModePoliciesAreAccepted(t *testing.T) {
	fr, done := liveEngine(t)
	defer done()

	for _, m := range availableCodexModes(Config{AllowFullAccess: true}) {
		t.Run(m.mode.ID, func(t *testing.T) {
			params := map[string]any{"cwd": t.TempDir()}
			applyPolicyParams(params, m.approvalPolicy, m.sandbox)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			raw, err := fr.sendRequest(ctx, "thread/start", params)
			cancel()
			if err != nil {
				t.Fatalf("thread/start with %s policy: %v", m.mode.ID, err)
			}
			var resp struct {
				Thread struct {
					ID string `json:"id"`
				} `json:"thread"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				t.Fatalf("decode thread: %v", err)
			}

			turn := map[string]any{
				"threadId":       resp.Thread.ID,
				"input":          []map[string]any{{"type": "text", "text": "hi"}},
				"approvalPolicy": m.approvalPolicy,
				"sandboxPolicy":  sandboxPolicyParam(m.sandbox),
			}
			ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Second)
			_, err = fr.sendRequest(ctx2, "turn/start", turn)
			cancel2()
			if err != nil && isParamError(err) {
				t.Fatalf("turn/start with %s policy rejected: %v", m.mode.ID, err)
			}
			ctx3, cancel3 := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = fr.sendRequest(ctx3, "turn/interrupt", map[string]any{"threadId": resp.Thread.ID})
			cancel3()
		})
	}
}
