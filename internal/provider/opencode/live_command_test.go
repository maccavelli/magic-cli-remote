//go:build live_opencode

package opencode_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// Run with: go test -tags live_opencode ./internal/provider/opencode/ -run Command -count=1
//
// Pins the engine contracts the canonical commands rest on (MADR 0023), each of
// which was established by probing rather than from docs: summarize is the
// compaction route (the v2 compact route answers 503), the model route takes a
// ModelRef and switches in place, and assistant token counts are what /context
// reports. One real turn is needed — usage and undo have nothing to work with
// otherwise.
func TestLiveCanonicalCommandCapabilities(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "cmd-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	// Every canonical op the table claims must be implemented by the session the
	// transport actually built.
	if _, ok := s.(provider.CompactSession); !ok {
		t.Fatal("session does not implement CompactSession")
	}
	if _, ok := s.(provider.ModelSession); !ok {
		t.Fatal("session does not implement ModelSession")
	}
	if _, ok := s.(provider.UndoSession); !ok {
		t.Fatal("session does not implement UndoSession")
	}

	// One cheap turn, so there is something to compact, undo and count.
	if err := s.Prompt(ctx, []provider.Content{{
		Type: "text",
		Text: "Reply with the single word: ok",
	}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	usage := waitForUsage(t, s, 3*time.Minute)
	if usage == nil {
		t.Fatal("no usage_update after a turn — /context has nothing to report")
	}
	t.Logf("usage: %d of %d tokens", usage.Used, usage.Size)
	if usage.Used <= 0 {
		t.Errorf("usage.Used = %d, want the turn's tokens", usage.Used)
	}
	if usage.Size <= 0 {
		t.Error("usage.Size = 0: the context window was not resolved from the " +
			"provider catalog, so /context can only show a bare count")
	}

	// The compaction route the table names must accept the call.
	if err := s.(provider.CompactSession).Compact(ctx); err != nil {
		t.Errorf("Compact: %v — the summarize route may have changed", err)
	}

	// An in-place model switch keeps the session; the metadata must follow it.
	if err := s.(provider.ModelSession).SetModel(ctx, "opencode/grok-code"); err != nil {
		t.Errorf("SetModel: %v — the ModelRef shape or route may have changed", err)
	}

	// Undo resolves the last user message itself.
	summary, err := s.(provider.UndoSession).UndoLast(ctx)
	if err != nil {
		t.Errorf("UndoLast: %v", err)
	} else if strings.TrimSpace(summary) == "" {
		t.Error("UndoLast returned no summary to show the user")
	}
	if rs, ok := s.(provider.RevertSession); ok {
		if err := rs.Unrevert(ctx); err != nil {
			t.Errorf("Unrevert (/redo): %v", err)
		}
	} else {
		t.Error("session does not implement RevertSession, which /redo needs")
	}
}

func waitForUsage(t *testing.T, s provider.Session, within time.Duration) *event.Usage {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				return nil
			}
			if ev.Type == event.TypeUsage && ev.Usage != nil {
				return ev.Usage
			}
		case <-deadline:
			return nil
		}
	}
}
