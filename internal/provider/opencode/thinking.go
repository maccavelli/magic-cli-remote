package opencode

import (
	"context"
	"fmt"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// defaultVariant is OpenCode's reserved `Session.model.variant` sentinel.
//
// 1.18.21 treats it as "no variant override" and normalises it away itself, so
// the daemon stores it as the resting value but never puts it on the wire:
// sending it is redundant, and sending an unadvertised key is an upstream error
// (MADR 0112 A14).
const defaultVariant = "default"

// activeModelSurface returns the advertised surface of the session's active
// model. The second result is false when the model is unknown to the catalog —
// before AfterBoot lands, or after a switch to a model the engine dropped.
func (o *httpSession) activeModelSurface() (modelSurface, bool) {
	mp, mid := o.resolveModel()
	if mid == "" {
		return modelSurface{}, false
	}
	return o.d.surfaces.lookup(mp + "/" + mid)
}

// advertisedLevels returns the rungs the active model advertises.
func (o *httpSession) advertisedLevels() []picker.ThinkingLevel {
	s, ok := o.activeModelSurface()
	if !ok {
		return nil
	}
	return s.Levels
}

// advertisesLevel reports whether the active model advertised this exact rung.
// Matching is exact: the upstream key is the wire value, and a near-miss must
// fail here rather than reach the engine as a 400.
func (o *httpSession) advertisesLevel(level string) bool {
	for _, l := range o.advertisedLevels() {
		if l.ID == level {
			return true
		}
	}
	return false
}

// ThinkingLevel implements [provider.ThinkingSession]. The resting value is the
// reserved sentinel, never the empty string, so the command layer can report a
// concrete level without inventing one.
func (o *httpSession) ThinkingLevel() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.thinkingLevel == "" {
		return defaultVariant
	}
	return o.thinkingLevel
}

// SetThinkingLevel implements [provider.ThinkingSession].
//
// Only a rung the *active* model advertises is accepted, plus the reserved
// sentinel. Anything else is refused here rather than forwarded: OpenCode
// rejects an unadvertised variant, and turning that into a failed turn would
// surface as a broken prompt instead of a rejected setting.
//
// OpenCode applies `variant` per request, so this is settable mid-session and
// [provider.ErrThinkingLevelFixed] is never returned by this provider — that
// sentinel stays Grok-specific (MADR 0112 A14).
func (o *httpSession) SetThinkingLevel(_ context.Context, level string) error {
	lvl := strings.TrimSpace(level)
	if lvl == "" || lvl == defaultVariant {
		o.mu.Lock()
		o.thinkingLevel = defaultVariant
		o.mu.Unlock()
		return nil
	}
	if !o.advertisesLevel(lvl) {
		advertised := o.advertisedLevels()
		if len(advertised) == 0 {
			return fmt.Errorf("opencode: the active model advertises no thinking levels")
		}
		names := make([]string, 0, len(advertised))
		for _, l := range advertised {
			names = append(names, l.ID)
		}
		return fmt.Errorf("opencode: %q is not a thinking level this model advertises (have: %s)",
			lvl, strings.Join(names, ", "))
	}
	o.mu.Lock()
	o.thinkingLevel = lvl
	o.mu.Unlock()
	return nil
}

// requestVariant returns the value to send as the optional `variant` field, or
// "" when the field must be omitted entirely.
func (o *httpSession) requestVariant() string {
	if lvl := o.ThinkingLevel(); lvl != defaultVariant {
		return lvl
	}
	return ""
}

// applyStartThinkingLevel settles the create/resume precedence for the stored
// rung (MADR 0112 A14, PLAN P3 step 6).
//
// upstream is the engine's own Session.model.variant ("" when it has none);
// requested is provider.StartOptions.ThinkingLevel. On resume the engine is
// authoritative, because the OpenCode TUI or another client may have changed it
// since this daemon last persisted a choice. A stored rung is therefore a
// fallback only when upstream has no variant at all, and either source must
// still be advertised by the resolved model to survive.
func (o *httpSession) applyStartThinkingLevel(upstream, requested string) {
	resolved := defaultVariant
	switch {
	case strings.TrimSpace(upstream) != "" && strings.TrimSpace(upstream) != defaultVariant:
		if u := strings.TrimSpace(upstream); o.advertisesLevel(u) {
			resolved = u
		}
	case strings.TrimSpace(requested) != "" && strings.TrimSpace(requested) != defaultVariant:
		if r := strings.TrimSpace(requested); o.advertisesLevel(r) {
			resolved = r
		}
	}
	o.mu.Lock()
	o.thinkingLevel = resolved
	o.mu.Unlock()
}

// resetThinkingLevel returns the session to the engine default. A model change
// invalidates the stored rung outright: rungs are per-model, and carrying one
// across a switch would send the new model a key the old model advertised.
func (o *httpSession) resetThinkingLevel() {
	o.mu.Lock()
	o.thinkingLevel = defaultVariant
	o.mu.Unlock()
}

// Compile-time check: the dialect half satisfies the thinking contract
// httpagent forwards. The public [provider.ThinkingSession] is implemented by
// httpagent's session wrapper, not by this dialect session.
var _ interface {
	SetThinkingLevel(context.Context, string) error
	ThinkingLevel() string
} = (*httpSession)(nil)

// PromptCapabilities implements httpagent's capability hook: which prompt
// inputs the session's active model actually accepts (MADR 0112 A2).
//
// Both the coarse `attachment` flag and the specific input modality must be
// true. They can disagree in the live catalog, and advertising an input the
// engine will discard is the failure this gate exists to prevent. An unknown
// model reports neither, which is the conservative answer a session created
// before the catalog landed must give until AfterBootRefined corrects it.
func (o *httpSession) PromptCapabilities() (image, audio bool) {
	s, ok := o.activeModelSurface()
	if !ok || !s.Attachment {
		return false, false
	}
	return s.Inputs.Image, s.Inputs.Audio
}

// AfterBootRefined implements httpagent's post-catalog hook.
//
// A rung stored before the catalog resolved was validated against an empty
// surface, so it is re-checked here and dropped if the model turns out not to
// advertise it. Dropping is right rather than keeping: an unadvertised variant
// is an upstream error on the next prompt.
func (o *httpSession) AfterBootRefined() {
	if lvl := o.ThinkingLevel(); lvl != defaultVariant && !o.advertisesLevel(lvl) {
		o.resetThinkingLevel()
	}
}
