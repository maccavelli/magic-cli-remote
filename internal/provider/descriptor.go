package provider

import (
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/picker/saved"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
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
	// Floor, when set, is the oldest engine release the provider drives.
	// doctor asks the binary its version and reports one below it.
	Floor *VersionFloor
	// Credentials, when set, returns the provider's credential adapter, so
	// the daemon coordinates its credential transactions (MADR 0074 D21).
	Credentials func(config.ProviderSection) providerauth.Adapter
	// New builds the provider from its section. Building starts nothing.
	New func(Build) Provider
}

// VersionFloor is the oldest release of an engine its provider drives, and
// where the refusal sends the operator.
type VersionFloor struct {
	Min        string
	UpgradeURL string
}

// Build is what a descriptor's New is given.
type Build struct {
	// Section is the provider's own section, as Section returned it.
	Section config.ProviderSection
	// Version is mcremote's own version.
	Version string
	// RuntimeDir is the daemon's runtime directory.
	RuntimeDir string
	Log        *slog.Logger
	// Credentials is the provider's credential coordinator, nil when it has
	// none or the daemon is only inspecting (mcremote doctor).
	Credentials *providerauth.Coordinator
	// Busy reports how many of the provider's sessions are live, for a
	// credential activation to ask; nil with no Credentials.
	Busy func() int
	// SavedCatalogs holds each provider's last live model catalog. It is nil
	// when the daemon is only inspecting (mcremote doctor), so no saved
	// catalog is read or written (MADR 0235 D1).
	SavedCatalogs *saved.Store
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
