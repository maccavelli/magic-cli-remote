package gobble

import (
	"fmt"
	"slices"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// Section is providers.gobble (MADR 0179 D11): the shared ACP keys and
// nothing of gobble's own. gobble reads its own settings as the service
// account; the daemon passes it no --set and no GOBBLE_* variable (D1).
type Section struct {
	config.ACPProviderConfig `mapstructure:",squash"`
}

// defaultSection is providers.gobble holding its defaults.
func defaultSection() *Section {
	return &Section{ACPProviderConfig: config.ACPProviderConfig{AgentCommon: config.AgentCommon{
		Enabled:                  true,
		Bin:                      "gobble",
		AlwaysApprove:            false,
		PermissionTimeoutSeconds: 120,
		// Off (MADR 0089 D5): the first session pays the cold start.
		Prewarm:                false,
		TurnStallNoticeSeconds: 120,
	}}}
}

// Validate checks providers.gobble. auth_method_id, when set, must be one of
// gobble's agent auth methods, the ids acpext.AuthProviders names (D4).
func (s *Section) Validate() error {
	if err := s.ValidateAgent("gobble"); err != nil {
		return err
	}
	if err := config.ValidateACPProvider("gobble", s.ACPProviderConfig); err != nil {
		return err
	}
	if id := s.AuthMethodID; id != "" {
		ids := safeAuthMethodIDs()
		if !slices.Contains(ids, id) {
			return fmt.Errorf("providers.gobble.auth_method_id must be empty or one of %s, got %q",
				strings.Join(ids, ", "), id)
		}
	}
	return nil
}

// providerConfig maps the section onto the provider's config.
func (s *Section) providerConfig() acpagent.Config {
	return acpagent.ConfigFrom(s.ACPProviderConfig)
}

// Descriptor is gobble's registration (D11). No Floor: gobble's supported
// minimum is stated, not enforced (D5, A2). No Credentials: gobble's store is
// written by gobble's own CLI, outside the credential coordinator (D3). P4
// lists it in registered.Descriptors.
func Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:      provider.IDGobble,
		Name:    "gobble",
		Section: func() config.ProviderSection { return defaultSection() },
		New: func(b provider.Build) provider.Provider {
			cfg := b.Section.(*Section).providerConfig()
			cfg.SavedCatalogs = b.SavedCatalogs
			return New(cfg, b.Log)
		},
	}
}
