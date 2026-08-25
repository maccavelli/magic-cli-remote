package opencode

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// textParts is the ordinary one-text-part prompt these tests send.
func textParts(text string) []provider.Content {
	return []provider.Content{{Type: "text", Text: text}}
}

// isFixedErr reports the Grok-only "level is fixed at spawn" sentinel, which
// this provider must never return.
func isFixedErr(err error) bool {
	return errors.Is(err, provider.ErrThinkingLevelFixed)
}

// newThinkingSession builds a session whose active model advertises levels.
func newThinkingSession(t *testing.T, h *recorder, model string, levels ...string) *httpSession {
	t.Helper()
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	rungs := make([]picker.ThinkingLevel, 0, len(levels))
	for _, l := range levels {
		rungs = append(rungs, picker.ThinkingLevel{ID: l})
	}
	d.surfaces.replace(map[string]modelSurface{
		model: {Attachment: true, Levels: picker.NormalizeThinkingLevels(rungs)},
	})
	h.model = model
	return d.NewSession(h).(*httpSession)
}

// TestThinkingLevelRestingValueIsSentinel proves a fresh session reports the
// reserved sentinel, not the empty string, so /thinking can name a level.
func TestThinkingLevelRestingValueIsSentinel(t *testing.T) {
	s := newThinkingSession(t, newRecorder(), "opencode/m", "low", "high")
	if got := s.ThinkingLevel(); got != defaultVariant {
		t.Fatalf("resting level = %q, want %q", got, defaultVariant)
	}
	if v := s.requestVariant(); v != "" {
		t.Fatalf("resting session must omit variant, got %q", v)
	}
}

// TestSetThinkingLevelAcceptsOnlyAdvertisedRungs is the core A14 rule: the
// active model's advertised keys and the sentinel, nothing else.
func TestSetThinkingLevelAcceptsOnlyAdvertisedRungs(t *testing.T) {
	s := newThinkingSession(t, newRecorder(), "opencode/m", "low", "high")
	for _, ok := range []string{"low", "high", defaultVariant, "", "  high  "} {
		if err := s.SetThinkingLevel(context.Background(), ok); err != nil {
			t.Fatalf("SetThinkingLevel(%q) = %v, want nil", ok, err)
		}
	}
	if got := s.ThinkingLevel(); got != "high" {
		t.Fatalf("trimmed value not stored: %q", got)
	}
	for _, bad := range []string{"xhigh", "LOW", "medium", "ultra", "0"} {
		if err := s.SetThinkingLevel(context.Background(), bad); err == nil {
			t.Fatalf("SetThinkingLevel(%q) was accepted; only advertised rungs may pass", bad)
		}
	}
	// A rejected value must not disturb the stored one.
	if got := s.ThinkingLevel(); got != "high" {
		t.Fatalf("a rejected rung changed the stored level to %q", got)
	}
}

// TestSetThinkingLevelWithoutAdvertisedRungs proves a model that advertises no
// variants refuses every concrete rung but still accepts the sentinel.
func TestSetThinkingLevelWithoutAdvertisedRungs(t *testing.T) {
	s := newThinkingSession(t, newRecorder(), "opencode/big-pickle")
	if err := s.SetThinkingLevel(context.Background(), "high"); err == nil {
		t.Fatal("a model advertising no variants accepted a rung")
	}
	if err := s.SetThinkingLevel(context.Background(), defaultVariant); err != nil {
		t.Fatalf("sentinel refused: %v", err)
	}
}

// TestSetThinkingLevelNeverReturnsFixed proves ErrThinkingLevelFixed stays
// Grok-specific: OpenCode applies variant per request (A14).
func TestSetThinkingLevelNeverReturnsFixed(t *testing.T) {
	s := newThinkingSession(t, newRecorder(), "opencode/m", "low")
	for _, v := range []string{"low", "nope", defaultVariant, ""} {
		if err := s.SetThinkingLevel(context.Background(), v); err != nil {
			if got := err.Error(); got == "" {
				t.Fatal("empty error text")
			}
			if isFixedErr(err) {
				t.Fatalf("SetThinkingLevel(%q) returned ErrThinkingLevelFixed", v)
			}
		}
	}
}

// TestPromptSendsVariantOnlyWhenSet proves the wire carries `variant` exactly
// when a concrete rung is stored, and never the reserved sentinel.
func TestPromptSendsVariantOnlyWhenSet(t *testing.T) {
	h := newRecorder()
	s := newThinkingSession(t, h, "opencode/m", "low", "high")

	if err := s.Prompt(context.Background(), textParts("hi")); err != nil {
		t.Fatal(err)
	}
	if got := h.find(t, "POST", "/prompt_async"); got.body["variant"] != nil {
		t.Fatalf("default session sent variant=%v; the engine normalises it away itself", got.body["variant"])
	}

	h.calls = nil
	if err := s.SetThinkingLevel(context.Background(), "high"); err != nil {
		t.Fatal(err)
	}
	if err := s.Prompt(context.Background(), textParts("hi")); err != nil {
		t.Fatal(err)
	}
	if got := h.find(t, "POST", "/prompt_async"); got.body["variant"] != "high" {
		t.Fatalf("variant=%v, want \"high\"", got.body["variant"])
	}

	// Back to the sentinel: the field must disappear again.
	h.calls = nil
	if err := s.SetThinkingLevel(context.Background(), defaultVariant); err != nil {
		t.Fatal(err)
	}
	if err := s.Prompt(context.Background(), textParts("hi")); err != nil {
		t.Fatal(err)
	}
	if got := h.find(t, "POST", "/prompt_async"); got.body["variant"] != nil {
		t.Fatalf("returning to default still sent variant=%v", got.body["variant"])
	}
}

// TestCommandSendsVariantOnlyWhenSet mirrors the prompt rule on POST …/command.
func TestCommandSendsVariantOnlyWhenSet(t *testing.T) {
	h := newRecorder()
	s := newThinkingSession(t, h, "opencode/m", "high")
	if err := s.submitCommand(context.Background(), "init", ""); err != nil {
		t.Fatal(err)
	}
	if got := h.find(t, "POST", "/command"); got.body["variant"] != nil {
		t.Fatalf("default command sent variant=%v", got.body["variant"])
	}
	h.calls = nil
	if err := s.SetThinkingLevel(context.Background(), "high"); err != nil {
		t.Fatal(err)
	}
	if err := s.submitCommand(context.Background(), "init", ""); err != nil {
		t.Fatal(err)
	}
	if got := h.find(t, "POST", "/command"); got.body["variant"] != "high" {
		t.Fatalf("command variant=%v, want \"high\"", got.body["variant"])
	}
}

// TestStartThinkingPrecedence pins all six create/resume combinations from
// PLAN P3 step 6. The engine is authoritative on resume: a stale stored rung
// never overrides a live upstream variant.
func TestStartThinkingPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name                string
		upstream, requested string
		want                string
	}{
		{"create with no request keeps the sentinel", "", "", defaultVariant},
		{"create applies an advertised request", "", "high", "high"},
		{"create drops an unadvertised request", "", "ultra", defaultVariant},
		{"resume takes the upstream variant", "low", "", "low"},
		{"resume prefers upstream over a stale stored rung", "low", "high", "low"},
		{"resume falls back to the request only when upstream has none", "", "high", "high"},
		{"resume drops an upstream variant the model no longer advertises", "ultra", "high", defaultVariant},
		{"the sentinel upstream is treated as no variant", defaultVariant, "high", "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newThinkingSession(t, newRecorder(), "opencode/m", "low", "high")
			s.applyStartThinkingLevel(tc.upstream, tc.requested)
			if got := s.ThinkingLevel(); got != tc.want {
				t.Fatalf("level = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestModelChangeResetsThinkingLevel proves a switch clears the rung: rungs are
// per-model and the new model may not advertise the old key.
func TestModelChangeResetsThinkingLevel(t *testing.T) {
	h := newRecorder()
	s := newThinkingSession(t, h, "opencode/m", "low", "high")
	if err := s.SetThinkingLevel(context.Background(), "high"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetModel(context.Background(), "opencode/other"); err != nil {
		t.Fatal(err)
	}
	if got := s.ThinkingLevel(); got != defaultVariant {
		t.Fatalf("after a model switch the level is %q, want %q", got, defaultVariant)
	}
	h.calls = nil
	if err := s.Prompt(context.Background(), textParts("hi")); err != nil {
		t.Fatal(err)
	}
	if got := h.find(t, "POST", "/prompt_async"); got.body["variant"] != nil {
		t.Fatalf("a stale rung survived the model switch onto the wire: %v", got.body["variant"])
	}
}

// TestThinkingCommandIsAdvertised proves the canonical /thinking gate is really
// flipped, and that the old refusal note is gone (A14).
func TestThinkingCommandIsAdvertised(t *testing.T) {
	m, ok := (&httpDialect{}).CommandTable()["thinking"]
	if !ok {
		t.Fatal("/thinking is absent from the OpenCode command table")
	}
	if m.Kind != command.KindOp || m.Op != command.OpSetThinkingLevel {
		t.Fatalf("/thinking = %+v, want KindOp/%s", m, command.OpSetThinkingLevel)
	}
	if m.Note != "" {
		t.Fatalf("/thinking still carries the stale refusal note %q", m.Note)
	}
}

// TestSessionConfigHasNoVariantOption proves A14's "one control" rule: the
// thinking picker is the only effort selector, so no session_config option may
// duplicate it.
func TestSessionConfigHasNoVariantOption(t *testing.T) {
	for name := range (&httpDialect{}).CommandTable() {
		if name == "variant" {
			t.Fatal("a /variant command duplicates the thinking control")
		}
	}
}

// TestConcurrentThinkingAccess proves the stored rung is race-free: prompts
// read it while /thinking writes it.
func TestConcurrentThinkingAccess(t *testing.T) {
	s := newThinkingSession(t, newRecorder(), "opencode/m", "low", "high")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			_ = s.SetThinkingLevel(context.Background(), "high")
			_ = s.SetThinkingLevel(context.Background(), defaultVariant)
		}
	}()
	for i := 0; i < 300; i++ {
		_ = s.ThinkingLevel()
		_ = s.requestVariant()
	}
	<-done
}
