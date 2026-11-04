package fake

import (
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Section is providers.fake.
type Section struct {
	// Enabled registers the fake provider. Default false: it is for
	// development and smoke runs, and tests enable it (MADR 0055 R6).
	Enabled bool `mapstructure:"enabled"`
}

// IsEnabled reports providers.fake.enabled.
func (s *Section) IsEnabled() bool { return s.Enabled }

// Validate accepts every fake section.
func (s *Section) Validate() error { return nil }

// Descriptor registers the fake provider (0208 MADR D6).
func Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:      provider.IDFake,
		Name:    "fake",
		DevOnly: true,
		Section: func() config.ProviderSection { return &Section{} },
		New:     func(provider.Build) provider.Provider { return New() },
	}
}
