// Package registered lists the providers this build carries, in registration
// order (0208 MADR D6). Adding an agent CLI is its package and one line in
// Descriptors.
package registered

import (
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
)

// Descriptors returns the registered providers, in registration order.
func Descriptors() []provider.Descriptor {
	return []provider.Descriptor{
		fake.Descriptor(),
	}
}

// Sections returns the registered providers' config sections, for
// config.LoadOptions.Providers.
func Sections() []config.ProviderSectionSpec {
	ds := Descriptors()
	out := make([]config.ProviderSectionSpec, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.SectionSpec())
	}
	return out
}
