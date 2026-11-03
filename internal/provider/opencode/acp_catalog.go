package opencode

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// The model catalog for OpenCode v2 (0214 MADR D7, amended at P6c). The model
// ids are the ACP `model` option's, which lists exactly the usable vendors'
// models; acpagent keeps the latest session's as the provider's catalog. The
// vendor step groups them: a vendor whose models the option lists is
// connected, and every other integration is not configured.
//
// GET /api/provider and /api/model are never read: they return a key set in
// opencode.json in plain text, and the control plane refuses them.

// acpModelsFromOption groups the option's values by vendor. A value is
// "<vendor>/<model>", and its name repeats the vendor ("anthropic/Claude
// Fable 5"), which the label drops.
func acpModelsFromOption(opts []picker.Option) []picker.Option {
	out := make([]picker.Option, 0, len(opts))
	for _, o := range opts {
		if vendor, _, ok := strings.Cut(o.ID, "/"); ok && vendor != "" {
			o.Group = vendor
			o.Label = strings.TrimPrefix(o.Label, vendor+"/")
		}
		out = append(out, o)
	}
	return out
}

// acpNoHarvest is the Spec's ListModels: opencode names no model at
// initialize, so before any session has reported its model option the picker
// shows the static list, and no agent is spawned to learn nothing.
func acpNoHarvest(context.Context, acpagent.Config) (picker.Catalog, error) {
	return picker.Catalog{}, nil
}

// modelProviders is the Spec's ModelProviders: the connected vendors, in the
// order the models list them, then every other integration, by name. Names
// come from the integration list; without it, the connected vendors are listed
// by id.
func (a *acpAuth) modelProviders(ctx context.Context, models []picker.Option) (picker.Catalog, error) {
	counts := map[string]int{}
	var connected []string
	for _, m := range models {
		if m.Group == "" {
			continue
		}
		if counts[m.Group] == 0 {
			connected = append(connected, m.Group)
		}
		counts[m.Group]++
	}
	ctx, cancel := context.WithTimeout(ctx, acpAuthReadTimeout)
	defer cancel()
	list, err := a.list(ctx)
	if err != nil {
		a.log.Debug("opencode integrations unavailable; connected vendors only", slog.String("err", err.Error()))
	}
	names := map[string]string{}
	var rest []picker.Option
	for _, in := range list {
		if in.isMCP() {
			continue
		}
		names[in.ID] = in.Name
		if counts[in.ID] == 0 {
			rest = append(rest, vendorOption(in.ID, in.Name, 0, false))
		}
	}
	opts := make([]picker.Option, 0, len(connected)+len(rest))
	for _, id := range connected {
		opts = append(opts, vendorOption(id, names[id], counts[id], true))
	}
	slices.SortStableFunc(rest, func(x, y picker.Option) int {
		return strings.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	opts = append(opts, rest...)
	def := ""
	if len(connected) > 0 {
		def = connected[0]
	}
	return picker.SingleCatalog(picker.SourceLive, opts, def, false), nil
}

// vendorOption is one vendor in the vendor step.
func vendorOption(id, name string, models int, connected bool) picker.Option {
	if name == "" {
		name = id
	}
	o := picker.Option{ID: id, Label: name, Group: "Not configured", Description: "not configured",
		Meta: map[string]string{picker.MetaConnected: fmt.Sprint(connected)}}
	if connected {
		o.Group = "Connected"
		o.Description = fmt.Sprintf("%d models", models)
		if models == 1 {
			o.Description = "1 model"
		}
		o.Meta[picker.MetaModelCount] = fmt.Sprint(models)
	}
	return o
}
