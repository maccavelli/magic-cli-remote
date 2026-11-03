package acpagent

import (
	"context"
	"fmt"

	acp "github.com/maccavelli/acp-go-sdk2"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// The model picker's vendor step for an ACP agent (MADR 0079; 0214 PLAN P6c
// Deviation 1). Spec.ModelProviders builds the vendors; one vendor's models,
// and a session's own, are the models whose Group is that vendor. A Spec
// without the hook has one implicit vendor, its own ID, which is what the
// daemon answered for every ACP agent before this.

// modelOptions are the values of the agent's `model` config option as picker
// options, and its current value.
func modelOptions(opts []acp.SessionConfigOption) (options []picker.Option, current string, ok bool) {
	sel, ok := acp.ConfigOptionByCategory(opts, acp.SessionConfigOptionCategoryModel)
	if !ok {
		return nil, "", false
	}
	if sel.Options.Ungrouped != nil {
		for _, v := range *sel.Options.Ungrouped {
			options = append(options, picker.Option{ID: string(v.Value), Label: v.Name})
		}
	}
	if sel.Options.Grouped != nil {
		for _, g := range *sel.Options.Grouped {
			for _, v := range g.Options {
				options = append(options, picker.Option{ID: string(v.Value), Label: v.Name, Group: string(g.Group)})
			}
		}
	}
	return options, string(sel.CurrentValue), true
}

// noteModelOption keeps the session's model option as the provider's catalog
// when the Spec asks for it. Caller holds s.mu.
func (s *session) noteModelOption(opts []acp.SessionConfigOption) {
	if s.provider == nil || s.provider.spec.ModelsFromOption == nil {
		return
	}
	models, current, ok := modelOptions(opts)
	if !ok || len(models) == 0 {
		return
	}
	s.SetModelCatalog(picker.SingleCatalog(picker.SourceLive, s.provider.spec.ModelsFromOption(models), current, true))
}

// ListModelProviders implements [provider.ModelProviderCatalog].
func (p *Provider) ListModelProviders(ctx context.Context) (picker.Catalog, error) {
	if p.spec.ModelProviders == nil {
		id := string(p.spec.ID)
		return picker.SingleCatalog(picker.SourceStatic, []picker.Option{{
			ID:    id,
			Label: id,
			Meta:  map[string]string{picker.MetaConnected: "true"},
		}}, id, false), nil
	}
	models, err := p.ListModels(ctx)
	if err != nil {
		return picker.Catalog{}, err
	}
	return p.spec.ModelProviders(ctx, models.Options)
}

// ListModelsFor implements [provider.ModelProviderCatalog]. An unknown vendor
// has no models, and free text stays possible.
func (p *Provider) ListModelsFor(ctx context.Context, modelProvider string) (picker.Catalog, error) {
	models, err := p.ListModels(ctx)
	if err != nil {
		return picker.Catalog{}, err
	}
	if p.spec.ModelProviders == nil {
		if modelProvider == string(p.spec.ID) {
			return models, nil
		}
		return picker.SingleCatalog(picker.SourceStatic, nil, "", true), nil
	}
	return modelsIn(models, modelProvider), nil
}

// modelsIn is the part of c whose Group is vendor, keeping c's default when it
// is among them.
func modelsIn(c picker.Catalog, vendor string) picker.Catalog {
	var in []picker.Option
	for _, o := range c.Options {
		if o.Group == vendor {
			in = append(in, o)
		}
	}
	def := ""
	for _, id := range c.DefaultIDs {
		for _, o := range in {
			if o.ID == id {
				def = id
			}
		}
	}
	return picker.SingleCatalog(c.Source, in, def, c.AllowCustom)
}

// ModelCatalog implements [provider.ModelCatalogSession]: the vendor step, or
// this session's own models of its current model's vendor, from its own model
// option. A Spec without ModelProviders reports none, and the daemon answers
// with the provider's list, as it did before.
func (s *session) ModelCatalog(ctx context.Context, scope string) (picker.Catalog, error) {
	if s.provider == nil || s.provider.spec.ModelProviders == nil {
		return picker.Catalog{}, fmt.Errorf("%w: no session-scoped model catalog", provider.ErrNotImplemented)
	}
	if scope == provider.CatalogScopeProviders {
		return s.provider.ListModelProviders(ctx)
	}
	s.mu.Lock()
	models, current, ok := modelOptions(s.configOptions)
	s.mu.Unlock()
	if !ok || len(models) == 0 {
		return s.provider.ListModels(ctx)
	}
	if hook := s.provider.spec.ModelsFromOption; hook != nil {
		models = hook(models)
	}
	all := picker.SingleCatalog(picker.SourceLive, models, current, true)
	for _, o := range models {
		if o.ID == current && o.Group != "" {
			return modelsIn(all, o.Group), nil
		}
	}
	return all, nil
}
