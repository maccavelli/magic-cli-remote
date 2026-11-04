package provider

import (
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// Descriptor is how a provider registers (0208 MADR D6). Each provider's
// package exports one; the daemon, doctor and the config loader iterate the
// registered list instead of naming providers.
type Descriptor struct {
	// ID is the provider's id, and the name of its providers.<id> section.
	ID ID
	// Name is how the daemon and doctor name the provider to an operator.
	Name string
	// DevOnly marks a provider for development and smoke runs only; the
	// daemon warns when one is enabled.
	DevOnly bool
	// Section returns a fresh providers.<id> section holding its defaults.
	Section func() config.ProviderSection
	// New builds the provider from its section. Building starts nothing.
	New func(Build) Provider
}

// Build is what a descriptor's New is given.
type Build struct {
	// Section is the provider's own section, as Section returned it.
	Section config.ProviderSection
	Log     *slog.Logger
}

// SectionSpec is d's section in the form config.Load takes.
func (d Descriptor) SectionSpec() config.ProviderSectionSpec {
	return config.ProviderSectionSpec{ID: string(d.ID), New: d.Section}
}

// SectionIn returns d's section from cfg, or a section holding its defaults
// when cfg has none, as a Config not built by config.Load has none.
func (d Descriptor) SectionIn(cfg config.Config) config.ProviderSection {
	if sec, ok := cfg.Providers.Sections[string(d.ID)]; ok {
		return sec
	}
	return d.Section()
}
