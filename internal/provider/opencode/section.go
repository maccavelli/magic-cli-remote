package opencode

import (
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// Section is providers.opencode (0208 MADR D6): the shared ACP keys, and the
// operator's shell policy (0214 MADR).
//
// The 1.x dialect's `pure`, `session_tree` and `stream_coalesce_ms`, the
// share policy `allow_remote_share`, and the pre-0019 `transport` are not
// keys: a config that still sets one fails to load and names it (0208 MADR
// D6, amendment of 2026-11-04).
type Section struct {
	config.ACPProviderConfig `mapstructure:",squash"`

	// AllowRemoteShell permits a phone to run a command in the session's
	// working directory (MADR 0112 A9). Default false.
	//
	// OpenCode's shell runs outside the model's tool-permission evaluation, so
	// this is remote command execution on the daemon host and is treated as
	// such: disabled by default, and per-command confirmation on top even once
	// enabled.
	AllowRemoteShell bool `mapstructure:"allow_remote_shell"`
}

// defaultSection is providers.opencode holding its defaults.
//
// OpenCode is enabled by default and selectable from the phone's new-session
// provider menu. Registration is harmless when the binary is absent: the
// provider lists as not ready, with a startup warning.
func defaultSection() *Section {
	return &Section{ACPProviderConfig: config.ACPProviderConfig{AgentCommon: config.AgentCommon{
		Enabled:                  true,
		Bin:                      "opencode",
		AlwaysApprove:            false,
		PermissionTimeoutSeconds: 120,
		// Off (MADR 0089 D5): the first session pays the cold start
		// instead of holding an idle engine.
		Prewarm:                false,
		TurnStallNoticeSeconds: 120,
	}}}
}

// Validate checks providers.opencode.
func (s *Section) Validate() error {
	if err := s.ValidateAgent("opencode"); err != nil {
		return err
	}
	return config.ValidateACPProvider("opencode", s.ACPProviderConfig)
}

// providerConfig maps the section onto the provider's config. A policy flag
// that is settable, documented and tested at the config layer but never copied
// here would be a default in name only (MADR 0112 A9).
func (s *Section) providerConfig() acpagent.Config {
	c := acpagent.ConfigFrom(s.ACPProviderConfig)
	// Off unless an operator turned it on for this host (MADR 0112 A9).
	c.AllowRemoteShell = s.AllowRemoteShell
	return c
}

// Descriptor registers OpenCode (0208 MADR D6). Each session is its own
// `opencode acp`; one lease-bound control plane serves what ACP does not carry
// (0214 MADR). doctor refuses an engine older than MinVersion by name.
func Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:      provider.IDOpencode,
		Name:    "opencode",
		Section: func() config.ProviderSection { return defaultSection() },
		Floor:   &provider.VersionFloor{Min: MinVersion, UpgradeURL: UpgradeURL},
		New: func(b provider.Build) provider.Provider {
			cfg := b.Section.(*Section).providerConfig()
			cfg.SavedCatalogs = b.SavedCatalogs
			return NewACPWithLogger(cfg, b.Log)
		},
	}
}
