package grok

import (
	"fmt"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// Section is providers.grok (0208 MADR D6): the shared ACP keys, and grok's
// own launch flags.
//
// allowed_tools, disallowed_tools, allow_rules and deny_rules are gone. Each
// was measured with no effect on a session driven over ACP, on grok 0.2.114
// and again on 1.0.70 (0208 PLAN P8, grok commit); live_toolpolicy_test.go
// fails if grok starts honouring one.
type Section struct {
	config.ACPProviderConfig `mapstructure:",squash"`
	// ReasoningEffort is still passed as --reasoning-effort (an ACP no-op on
	// grok 1.0.5) and applied through session/new|load `_meta.reasoningEffort`
	// (MADR 0106).
	ReasoningEffort string `mapstructure:"reasoning_effort"`
	// PermissionMode sets grok's permission mode (--permission-mode <MODE>).
	PermissionMode string `mapstructure:"permission_mode"`
	// Sandbox selects grok's OS-level sandbox profile (--sandbox <PROFILE>):
	// off, workspace, devbox, read-only, strict, or a custom profile name
	// resolved from ~/.grok/sandbox.toml or .grok/sandbox.toml. Empty leaves
	// grok's own default. Deliberately not enum-validated beyond the built-ins
	// being documented: custom profiles are a supported grok feature and a hard
	// enum would break the day grok adds one (MADR 0050 D4).
	Sandbox string `mapstructure:"sandbox"`
	// NoSubagents disables subagent spawning (--no-subagents).
	NoSubagents bool `mapstructure:"no_subagents"`
	// DisableWebSearch disables built-in web search (--disable-web-search).
	DisableWebSearch bool `mapstructure:"disable_web_search"`
	// StreamCoalesceMs is how long assistant/thought text is held so it can be
	// emitted as one event instead of one per model token (MADR 0024 / 0057 H-1).
	// The first chunk of a run and the tail before any control event are never
	// delayed. 0 disables coalescing. Default 80. Max config.MaxStreamCoalesceMs.
	StreamCoalesceMs int `mapstructure:"stream_coalesce_ms"`
}

// defaultSection is providers.grok holding its defaults.
func defaultSection() *Section {
	return &Section{
		ACPProviderConfig: config.ACPProviderConfig{AgentCommon: config.AgentCommon{
			Enabled:                  true,
			Bin:                      "grok",
			AlwaysApprove:            false,
			PermissionTimeoutSeconds: 120,
			// Off (MADR 0089 D5): engine starts on first session.
			// Turn on from the phone or set true here if RAM allows.
			Prewarm:                false,
			TurnStallNoticeSeconds: 120,
		}},
		// Pinned rather than left empty. Empty means "whatever this host's
		// grok resolves to" — ~/.grok/config.toml, project config, or (since
		// grok 0.2.102) fleet-wide remote config — which the daemon cannot
		// see. It advertises session modes and a `dangerous` flag on the
		// assumption it knows the approval posture, so an unknown one makes
		// the mode chip describe a policy nobody set. `default` makes grok
		// ask, which is what gives the modes (and MADR 0049's `auto`)
		// meaning. Opt out with `bypassPermissions` (MADR 0050 D3).
		PermissionMode: "default",
		// ~12 mid-stream updates/sec instead of one per token
		// (MADR 0057 H-1). Matches opencode/codex defaults.
		StreamCoalesceMs: 80,
	}
}

// validPermissionMode returns true for recognized grok permission modes.
// Empty means "inherit grok's own configuration" — legal, but see
// defaultSection for why the daemon pins it (MADR 0050 D3).
func validPermissionMode(s string) bool {
	switch s {
	case "", "default", "acceptEdits", "auto", "dontAsk", "bypassPermissions", "plan":
		return true
	}
	return false
}

// Validate checks providers.grok.
func (s *Section) Validate() error {
	if err := s.ValidateAgent("grok"); err != nil {
		return err
	}
	if v := s.StreamCoalesceMs; v < 0 || v > config.MaxStreamCoalesceMs {
		return fmt.Errorf("providers.grok.stream_coalesce_ms must be 0..%d, got %d",
			config.MaxStreamCoalesceMs, v)
	}
	if s.ReasoningEffort != "" && strings.TrimSpace(s.ReasoningEffort) == "" {
		return fmt.Errorf("providers.grok.reasoning_effort must not be whitespace-only")
	}
	if err := config.ValidateACPProvider("grok", s.ACPProviderConfig); err != nil {
		return err
	}
	// Rejected at load rather than at session start: grok exits with
	// `error: unexpected argument` for an unknown value, which surfaces as a
	// provider that never becomes ready (MADR 0050).
	if !validPermissionMode(s.PermissionMode) {
		return fmt.Errorf("providers.grok.permission_mode must be empty, default, acceptEdits, auto, dontAsk, bypassPermissions, or plan, got %q",
			s.PermissionMode)
	}
	return nil
}

// providerConfig maps the section onto the provider's config. version is
// mcremote's own, sent to grok as its client's.
func (s *Section) providerConfig(version string) Config {
	c := acpagent.ConfigFrom(s.ACPProviderConfig)
	c.ReasoningEffort = s.ReasoningEffort
	c.PermissionMode = s.PermissionMode
	c.Sandbox = s.Sandbox
	c.NoSubagents = s.NoSubagents
	c.DisableWebSearch = s.DisableWebSearch
	c.ClientVersion = version
	// Explicit pointer so 0 means "one event per token" (pre-0057), not
	// "use the transport default".
	streamCoalesce := time.Duration(s.StreamCoalesceMs) * time.Millisecond
	c.StreamCoalesce = &streamCoalesce
	return c
}

// Descriptor registers grok (0208 MADR D6). Its credentials are transactional
// (MADR 0074 D21): with a coordinator, the provider is the coordinated one.
func Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:      provider.IDGrok,
		Name:    "grok",
		Section: func() config.ProviderSection { return defaultSection() },
		Credentials: func(config.ProviderSection) providerauth.Adapter {
			return NewCredentialAdapter("grok")
		},
		New: func(b provider.Build) provider.Provider {
			s := b.Section.(*Section)
			cfg := s.providerConfig(b.Version)
			cfg.SavedCatalogs = b.SavedCatalogs
			gp := NewWithLogger(cfg, b.Log)
			if b.Credentials == nil {
				return gp
			}
			return NewCoordinated(gp, s.Bin, b.Log, b.Credentials, b.Busy)
		},
	}
}
