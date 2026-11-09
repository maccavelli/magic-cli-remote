package gobble

import (
	"context"
	"slices"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// noHarvest is the Spec's ListModels. gobble's initialize carries no model
// state, so starting a gobble process to read a catalog would learn nothing
// and start gobble's model listings for nothing (MADR 0179 G10, D7). Before a
// host's first session the picker takes free text; after it, the saved
// catalog serves the last one (0235).
func noHarvest(context.Context, acpagent.Config) (picker.Catalog, error) {
	return picker.Catalog{}, nil
}

// modelsFromOption keeps every group gobble's model option sends, in gobble's
// order: its recent picks first, then one group per provider. A model in the
// recent group repeats one in its provider's group, with the same id, so
// either row selects it (PLAN P2 step 7, the owner's word of 2026-11-07).
func modelsFromOption(opts []picker.Option) []picker.Option {
	return slices.Clone(opts)
}
