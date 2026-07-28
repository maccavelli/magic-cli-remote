package acphttp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// ACP session config option ids that carry the model catalog. Goose names them
// exactly this (probed against goose 1.44.0, MADR 0043 §2.3); an agent that
// names them otherwise simply reports no catalog, which is the pre-0043
// behaviour.
const (
	configOptionProvider = "provider"
	configOptionModel    = "model"
)

// The catalog surfaces this package implements. Compile-time assertions so a
// signature drift is a build failure rather than a silently missing picker.
var (
	_ provider.ModelCatalog         = (*Provider)(nil)
	_ provider.ModelProviderCatalog = (*Provider)(nil)
	_ provider.ModelCatalogSession  = (*session)(nil)
	_ provider.ModelSession         = (*session)(nil)
)

// Catalog cache keys, distinct from any model provider id.
const (
	cacheKeyModelProviders = "\x00providers"
	cacheKeyModels         = "\x00models"
)

// catalogProbeTimeout bounds the throwaway session used to harvest a catalog
// before any user session exists. Measured: goose session/new answers in ~2 s.
const catalogProbeTimeout = 30 * time.Second

// modelCatalogFromOption converts a `model` select into a picker catalog.
//
// Ids are provider-qualified ("openai/gpt-5.6-sol"). They must be: goose's own
// model ids are only meaningful inside a provider — `gpt-4o` exists under
// `openai` and not under `google` — so an unqualified id chosen at
// create-session time would be applied against whatever provider the engine
// happened to be on (MADR 0043 D6).
//
// Goose reports no release dates, so the engine's order is preserved exactly.
// Ranking it by a version-name heuristic would be a recency claim the data does
// not support.
func modelCatalogFromOption(opt event.ConfigOption, modelProvider string) picker.Catalog {
	opts := make([]picker.Option, 0, len(opt.Values))
	for _, v := range opt.Values {
		if v.ID == "" {
			continue
		}
		label := v.Name
		if label == "" {
			label = v.ID
		}
		opts = append(opts, picker.Option{
			ID:    qualifyModel(modelProvider, v.ID),
			Label: label,
			Group: modelProvider,
		})
	}
	def := ""
	if opt.CurrentValue != "" {
		def = qualifyModel(modelProvider, opt.CurrentValue)
	}
	return picker.SingleCatalog(picker.SourceLive, picker.OrderModels(opts, def), def, true)
}

// providerCatalogFromOption converts a `provider` select into a picker catalog.
//
// The engine reports every provider it can speak to, not the ones with
// credentials, and offers no way to tell them apart — so the current one is the
// only thing known to work and is the sole entry marked connected. Guessing
// wider would put a green light on providers that fail the moment they are
// selected (§2.3: switching to an unconfigured provider returns -32603).
func providerCatalogFromOption(opt event.ConfigOption) picker.Catalog {
	opts := make([]picker.Option, 0, len(opt.Values))
	for _, v := range opt.Values {
		if v.ID == "" {
			continue
		}
		label := v.Name
		if label == "" {
			label = v.ID
		}
		current := v.ID == opt.CurrentValue
		group := "All providers"
		if current {
			group = "Connected"
		}
		opts = append(opts, picker.Option{
			ID:    v.ID,
			Label: label,
			Group: group,
			Meta:  map[string]string{picker.MetaConnected: fmt.Sprint(current)},
		})
	}
	// Current first; the rest keep the engine's order, which is alphabetical by
	// id and is the right order for a list browsed by search.
	ordered := make([]picker.Option, 0, len(opts))
	for _, o := range opts {
		if o.Group == "Connected" {
			ordered = append(ordered, o)
		}
	}
	for _, o := range opts {
		if o.Group != "Connected" {
			ordered = append(ordered, o)
		}
	}
	return picker.SingleCatalog(picker.SourceLive, ordered, opt.CurrentValue, false)
}

// qualifyModel joins a model provider and model id. An empty provider leaves
// the id alone, which is what a session that reports no provider option needs.
func qualifyModel(modelProvider, modelID string) string {
	if modelProvider == "" {
		return modelID
	}
	return modelProvider + "/" + modelID
}

// splitQualifiedModel splits "provider/model". A value with no slash is a bare
// model id and keeps the pre-0043 meaning: set `model` and leave `provider`
// alone, so persisted sessions and hand-typed ids still work.
func splitQualifiedModel(v string) (modelProvider, modelID string) {
	mp, id, ok := strings.Cut(v, "/")
	if !ok {
		return "", v
	}
	return mp, id
}

// noteConfigOptions records the latest config options seen on any live session
// as the provider-level catalog. It is what makes the create-session picker
// free while a session is open.
func (p *Provider) noteConfigOptions(opts []event.ConfigOption) {
	if len(opts) == 0 {
		return
	}
	p.mu.Lock()
	p.configOpts = opts
	p.mu.Unlock()
}

// cachedConfigOptions returns the provider-level snapshot, if any.
func (p *Provider) cachedConfigOptions() []event.ConfigOption {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.configOpts
}

func findOption(opts []event.ConfigOption, id string) (event.ConfigOption, bool) {
	for _, o := range opts {
		if o.ID == id {
			return o, true
		}
	}
	return event.ConfigOption{}, false
}

// ListModels implements [provider.ModelCatalog]: the current provider's models.
//
// Before MADR 0043 this returned spec.StaticModels, and goose's spec sets none
// — so goose's model picker was empty. The catalog was on the wire the whole
// time as ACP session config options; nothing read it back.
func (p *Provider) ListModels(ctx context.Context) (picker.Catalog, error) {
	static := picker.SingleCatalog(picker.SourceStatic, p.spec.StaticModels, p.cfg.Model, true)
	opts, err := p.catalogOptions(ctx)
	if err != nil {
		p.log.Debug("list models: no catalog available; static",
			slog.String("err", err.Error()))
		return static, nil
	}
	modelOpt, ok := findOption(opts, configOptionModel)
	if !ok {
		return static, nil
	}
	mp := ""
	if pr, ok := findOption(opts, configOptionProvider); ok {
		mp = pr.CurrentValue
	}
	cat := modelCatalogFromOption(modelOpt, mp)
	if p.cfg.Model != "" {
		// Config is the operator's pre-session policy; the picker must not
		// claim a model Start will not use.
		cat.DefaultIDs = []string{p.cfg.Model}
	}
	return cat, nil
}

// ListModelProviders implements [provider.ModelProviderCatalog].
func (p *Provider) ListModelProviders(ctx context.Context) (picker.Catalog, error) {
	opts, err := p.catalogOptions(ctx)
	if err != nil {
		return picker.SingleCatalog(picker.SourceStatic, nil, "", false), nil
	}
	providerOpt, ok := findOption(opts, configOptionProvider)
	if !ok {
		return picker.SingleCatalog(picker.SourceStatic, nil, "", false), nil
	}
	return providerCatalogFromOption(providerOpt), nil
}

// ListModelsFor implements [provider.ModelProviderCatalog].
//
// The catalog is only ever scoped to the provider the engine is *currently*
// on: goose re-scopes its model list when the provider changes, and switching
// it to enumerate another provider's models would change the engine's own
// state as a side effect of opening a picker. A different provider therefore
// answers empty-with-free-text rather than lying or mutating state.
func (p *Provider) ListModelsFor(ctx context.Context, modelProvider string) (picker.Catalog, error) {
	opts, err := p.catalogOptions(ctx)
	if err != nil {
		return picker.SingleCatalog(picker.SourceStatic, nil, "", true), nil
	}
	modelOpt, ok := findOption(opts, configOptionModel)
	if !ok {
		return picker.SingleCatalog(picker.SourceStatic, nil, "", true), nil
	}
	current := ""
	if pr, ok := findOption(opts, configOptionProvider); ok {
		current = pr.CurrentValue
	}
	if modelProvider != "" && modelProvider != current {
		p.log.Debug("model list requested for a provider the engine is not on",
			slog.String("requested", modelProvider), slog.String("current", current))
		return picker.SingleCatalog(picker.SourceStatic, nil, "", true), nil
	}
	return modelCatalogFromOption(modelOpt, current), nil
}

// catalogOptions returns the config options that carry the catalog, from the
// provider cache, or by opening a short-lived probe session when nothing has
// populated it yet.
func (p *Provider) catalogOptions(ctx context.Context) ([]event.ConfigOption, error) {
	if opts := p.cachedConfigOptions(); len(opts) > 0 {
		return opts, nil
	}
	if !p.Ready() {
		return nil, fmt.Errorf("%s not installed", p.cfg.Bin)
	}
	// Single-flighted and TTL'd through the same cache the model catalogs use,
	// so N picker opens against a cold cache mean one probe session.
	if _, err := p.catalogs.Get(ctx, cacheKeyModelProviders, func(ctx context.Context) (picker.Catalog, error) {
		if err := p.probeCatalog(ctx); err != nil {
			return picker.Catalog{}, err
		}
		return picker.Catalog{}, nil
	}); err != nil {
		return nil, err
	}
	opts := p.cachedConfigOptions()
	if len(opts) == 0 {
		return nil, fmt.Errorf("agent reported no session config options")
	}
	return opts, nil
}

// probeCatalog opens a throwaway session purely to read its config options.
//
// There is no pre-session endpoint: goose's catalog arrives on session/new, and
// `goose serve` exposes no HTTP models/providers route (probed: /models,
// /providers, /api/models, /config, /openapi.json all 404). Logged at Info
// because a picker that starts an agent session should be visible in the log.
func (p *Provider) probeCatalog(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, catalogProbeTimeout)
	defer cancel()
	start := time.Now()
	p.log.Info("opening a short-lived session to read the model catalog",
		slog.String("bin", p.cfg.Bin))
	sess, err := p.Start(ctx, provider.StartOptions{Name: "mcremote-catalog-probe"})
	if err != nil {
		return fmt.Errorf("catalog probe session: %w", err)
	}
	defer func() {
		// context.WithoutCancel: the probe's own deadline must not abort the
		// close, or the throwaway session outlives the probe on the engine.
		if cerr := sess.Close(context.WithoutCancel(ctx)); cerr != nil {
			p.log.Debug("catalog probe close", slog.String("err", cerr.Error()))
		}
	}()
	// Config options are emitted synchronously during create, so they are
	// already retained by the time Start returns.
	if len(p.cachedConfigOptions()) == 0 {
		return fmt.Errorf("catalog probe session reported no config options")
	}
	p.log.Info("model catalog harvested",
		slog.Duration("took", time.Since(start).Round(time.Millisecond)))
	return nil
}
