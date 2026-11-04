package kilo

import (
	"fmt"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Section is providers.kilo (0208 MADR D6): the Kilo CLI provider, a shared
// `kilo serve` engine over httpagent (MADR 0075). Kilo is a fork of
// OpenCode's 1.x serve architecture, so its keys follow that dialect's: no
// `transport` (retired in MADR 0019 before kilo existed), no `args`, and no
// auth keys (the engine spawns un-gated on loopback, MADR 0075 D5 as amended /
// plan PD1).
//
// Of the keys every agent has: `model` is a providerID/modelID pair split on
// the FIRST slash only (plan PD3), since Kilo model ids may themselves contain
// slashes ("kilo/~anthropic/x" → provider "kilo", model "~anthropic/x"). Empty
// uses the engine default, which is auth-state-dependent (kilo-auto/free vs
// kilo-auto/balanced — MADR 0075 §2.6), so no default is hard-coded. `prewarm`
// boots the shared `kilo serve` engine at daemon start so the first session
// create skips the Bun-class cold start.
type Section struct {
	config.AgentCommon `mapstructure:",squash"`
	// SessionTree enables multi-agent session-tree demux. Default FALSE for
	// kilo (unlike OpenCode): the routes exist but child SSE events are
	// unproven — flip only after the MADR 0075 Q7 fixtures land (plan PD2).
	SessionTree bool `mapstructure:"session_tree"`
	// StreamCoalesceMs is how long assistant/thought text is held so it can be
	// sent as one event instead of one per model token (MADR 0024). 0 disables
	// coalescing. Default 80.
	StreamCoalesceMs int `mapstructure:"stream_coalesce_ms"`
	// Pure runs kilo serve without loading external third-party plugins
	// (--pure, live-confirmed on 7.4.20). Default false (match OpenCode).
	Pure bool `mapstructure:"pure"`
}

// defaultSection is providers.kilo holding its defaults.
func defaultSection() *Section {
	return &Section{
		AgentCommon: config.AgentCommon{
			// Default-on since MADR 0075 acceptance flip (2026-08-10).
			Enabled:                  true,
			Bin:                      "kilo",
			AlwaysApprove:            false,
			PermissionTimeoutSeconds: 120,
			// Off (MADR 0089 D5): first session pays the Bun-class
			// cold start instead of holding an idle engine.
			Prewarm:                false,
			TurnStallNoticeSeconds: 120,
		},
		// Off until child-SSE fixtures prove tree demux (plan PD2).
		SessionTree:      false,
		StreamCoalesceMs: 80,
	}
}

// Validate checks providers.kilo.
func (s *Section) Validate() error {
	if err := s.ValidateAgent("kilo"); err != nil {
		return err
	}
	if v := s.StreamCoalesceMs; v < 0 || v > config.MaxStreamCoalesceMs {
		return fmt.Errorf("providers.kilo.stream_coalesce_ms must be 0..%d, got %d",
			config.MaxStreamCoalesceMs, v)
	}
	return nil
}

// providerConfig maps the section onto the provider's config.
func (s *Section) providerConfig() Config {
	sessionTree := s.SessionTree
	streamCoalesce := time.Duration(s.StreamCoalesceMs) * time.Millisecond
	return Config{
		Bin:               s.Bin,
		AlwaysApprove:     s.AlwaysApprove,
		DefaultCWD:        s.DefaultCWD,
		Model:             s.Model,
		PermissionTimeout: time.Duration(s.PermissionTimeoutSeconds) * time.Second,
		TurnStallNotice:   time.Duration(s.TurnStallNoticeSeconds) * time.Second,
		// Explicit pointer so the false default (plan PD2) is distinct
		// from zero Config; kilo flips this only after MADR 0075 Q7.
		SessionTree: &sessionTree,
		// Likewise explicit: 0 means "stream one event per token".
		StreamCoalesce: &streamCoalesce,
		Pure:           s.Pure,
	}
}

// Descriptor registers Kilo (0208 MADR D6). One shared long-lived `kilo
// serve` engine (HTTP + SSE) drives every Kilo session, same architecture as
// OpenCode 1.x (MADR 0075 D2/D4); it is spawned un-gated on loopback (D5 as
// amended, plan PD1).
func Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:      provider.IDKilo,
		Name:    "kilo",
		Section: func() config.ProviderSection { return defaultSection() },
		New: func(b provider.Build) provider.Provider {
			return NewHTTPWithLogger(b.Section.(*Section).providerConfig(), b.Log)
		},
	}
}
