package opencode

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Diagnostics section bounds (MADR 0112 A6).
const (
	maxDiagnosticSkills     = 64
	maxDiagnosticLSP        = 32
	maxDiagnosticFormatters = 32
	maxSkillNameLen         = 128
	maxSkillDescriptionLen  = 512
)

// normalizeMCPState is the total mapping of the closed 1.18.21 MCPStatus union.
//
// Every member is named explicitly so a future upstream addition falls to
// "unknown" rather than reaching the phone as a raw string. That matters more
// than it looks: two of the upstream members carry a required `error` field
// containing URLs and bearer tokens, and a pass-through default would be the
// path by which such a value escaped (PLAN P7 step 9).
func normalizeMCPState(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "connected":
		return provider.MCPStateConnected
	case "disabled":
		return provider.MCPStateDisabled
	case "failed":
		return provider.MCPStateFailed
	case "needs_auth":
		return provider.MCPStateNeedsAuth
	case "needs_client_registration":
		return provider.MCPStateNeedsRegistration
	default:
		return provider.MCPStateUnknown
	}
}

// skillsFor reads discovered skills, keeping only name and description.
//
// The upstream body also carries the skill's location and its full instruction
// text. Neither is decoded: a field this struct does not declare cannot leak,
// which is the same mechanism that keeps API keys out of the model catalog.
func (o *httpSession) skillsFor(ctx context.Context) ([]provider.SkillInfo, bool) {
	var upstream []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := o.h.API()(ctx, "GET", "/skill"+o.dir(), nil, &upstream); err != nil {
		o.h.Log().Debug("opencode diagnostics skills failed", "err", err)
		return nil, false
	}
	out := make([]provider.SkillInfo, 0, len(upstream))
	for _, u := range upstream {
		name := clip(strings.TrimSpace(u.Name), maxSkillNameLen)
		if name == "" {
			continue
		}
		out = append(out, provider.SkillInfo{
			Name:        name,
			Description: clip(strings.TrimSpace(u.Description), maxSkillDescriptionLen),
		})
	}
	slices.SortFunc(out, func(a, b provider.SkillInfo) int {
		return strings.Compare(a.Name, b.Name)
	})
	if len(out) > maxDiagnosticSkills {
		out = out[:maxDiagnosticSkills]
	}
	return out, true
}

// lspFor reads language-server state, keeping only name and status. Roots and
// executable paths would disclose the daemon host's layout.
func (o *httpSession) lspFor(ctx context.Context) ([]provider.LSPStatus, bool) {
	var upstream []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := o.h.API()(ctx, "GET", "/lsp"+o.dir(), nil, &upstream); err != nil {
		o.h.Log().Debug("opencode diagnostics lsp failed", "err", err)
		return nil, false
	}
	out := make([]provider.LSPStatus, 0, len(upstream))
	for _, u := range upstream {
		name := clip(strings.TrimSpace(u.Name), maxDiagnosticTextLen)
		if name == "" {
			continue
		}
		out = append(out, provider.LSPStatus{
			Name:   name,
			Status: clip(strings.TrimSpace(u.Status), maxDiagnosticTextLen),
		})
	}
	slices.SortFunc(out, func(a, b provider.LSPStatus) int {
		return strings.Compare(a.Name, b.Name)
	})
	if len(out) > maxDiagnosticLSP {
		out = out[:maxDiagnosticLSP]
	}
	return out, true
}

// formattersFor reads formatter availability.
//
// Extensions are counted rather than listed: the count answers "does this cover
// my files", while the list is configuration the phone does not act on.
func (o *httpSession) formattersFor(ctx context.Context) ([]provider.FormatterInfo, bool) {
	var upstream []struct {
		Name       string   `json:"name"`
		Enabled    bool     `json:"enabled"`
		Extensions []string `json:"extensions"`
	}
	if err := o.h.API()(ctx, "GET", "/formatter"+o.dir(), nil, &upstream); err != nil {
		o.h.Log().Debug("opencode diagnostics formatter failed", "err", err)
		return nil, false
	}
	out := make([]provider.FormatterInfo, 0, len(upstream))
	for _, u := range upstream {
		name := clip(strings.TrimSpace(u.Name), maxDiagnosticTextLen)
		if name == "" {
			continue
		}
		out = append(out, provider.FormatterInfo{
			Name:       name,
			Enabled:    u.Enabled,
			Extensions: len(u.Extensions),
		})
	}
	slices.SortFunc(out, func(a, b provider.FormatterInfo) int {
		return strings.Compare(a.Name, b.Name)
	})
	if len(out) > maxDiagnosticFormatters {
		out = out[:maxDiagnosticFormatters]
	}
	return out, true
}

// DisposeInstance recycles the OpenCode instance for this session's project.
//
// It calls documented POST /instance/dispose exactly once with the session's
// own working directory. Instance state is keyed by normalized directory and
// disposal invalidates only that cache: persisted sessions, messages and tool
// history survive, which is what makes this an acceptable way to pick up a
// newly written skill (MADR 0112 A10).
func (o *httpSession) DisposeInstance(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// The directory is supplied by the daemon, never by the phone.
	return o.h.API()(callCtx, "POST", "/instance/dispose"+o.dir(), map[string]any{}, nil)
}

// ReloadSkillCatalogs re-reads the discovery surfaces the disposal invalidated.
//
// Both are read again because a skill appears in two places: /skill is the
// diagnostics view, and /command is where a skill-backed command shows up in
// the composer. Reloading one and not the other leaves the phone half-updated.
func (o *httpSession) ReloadSkillCatalogs(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, ok := o.skillsFor(callCtx); !ok {
		return fmt.Errorf("opencode skill reload failed")
	}
	// Re-advertise commands so a skill-backed command becomes selectable
	// without another round trip from the phone.
	o.advertiseCommands(callCtx)
	return nil
}
