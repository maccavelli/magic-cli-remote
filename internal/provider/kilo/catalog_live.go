package kilo

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// ListAgentsLive implements [httpagent.AgentLister] via GET /agent.
func (d *httpDialect) ListAgentsLive(ctx context.Context, api httpagent.API) (picker.Catalog, error) {
	agents, err := fetchAgents(ctx, api, "")
	if err != nil {
		return picker.Catalog{}, err
	}
	opts := make([]picker.Option, 0, len(agents))
	for _, a := range agents {
		// Hidden agents (compaction, summary, title) are engine internals
		// reported as primary; they are not selectable work.
		if !a.startable() {
			continue
		}
		name := a.Name
		mode := a.Mode
		if mode == "" {
			mode = "primary"
		}
		desc := strings.TrimSpace(a.Description)
		if desc == "" {
			desc = mode
		}
		opts = append(opts, picker.Option{
			ID:          name,
			Label:       name,
			Description: desc,
			Group:       mode,
			Meta:        map[string]string{"mode": mode},
		})
	}
	slices.SortFunc(opts, func(a, b picker.Option) int {
		// Primary/all agents first, then by id. Subagents are deliberately not
		// in this catalog: they cannot accept a top-level user turn.
		rank := func(g string) int {
			switch g {
			case "primary":
				return 0
			case "all":
				return 1
			default:
				return 2
			}
		}
		if ra, rb := rank(a.Group), rank(b.Group); ra != rb {
			return ra - rb
		}
		return strings.Compare(a.ID, b.ID)
	})
	def := "code"
	hasDefault := false
	for _, o := range opts {
		if o.ID == "code" {
			hasDefault = true
			break
		}
	}
	if !hasDefault && len(opts) > 0 {
		// Prefer first primary-mode option.
		def = opts[0].ID
		for _, o := range opts {
			if o.Group == "primary" {
				def = o.ID
				break
			}
		}
	}
	return picker.SingleCatalog(picker.SourceLive, opts, def, false), nil
}

// catalogFetchTimeout bounds one engine catalog fetch. /provider is ~4.7 MB on kilo 7.4.20 and
// takes ~0.9 s on a warm engine; 15 s covers a cold one with room to spare.
const catalogFetchTimeout = 15 * time.Second

// providerModel is the per-model shape of both /provider and /config/providers.
// Only the fields the picker shows are declared, which is also the mechanism
// that keeps secrets out: see [connectedProvidersResponse].
type providerModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ReleaseDate string `json:"release_date"`
	Status      string `json:"status"`
	Limit       struct {
		Context int `json:"context"`
	} `json:"limit"`
}

// providerEntry is one model provider in /provider's `all` array.
type providerEntry struct {
	ID     string                   `json:"id"`
	Name   string                   `json:"name"`
	Models map[string]providerModel `json:"models"`
}

// fullProvidersResponse is GET /provider: every provider models.dev knows,
// 4.7 MB and 179 providers on the kilo 7.4.20 spike host. `connected` names the ones the user
// actually has credentials for.
type fullProvidersResponse struct {
	All       []providerEntry   `json:"all"`
	Default   map[string]string `json:"default"`
	Connected []string          `json:"connected"`
}

// connectedProvidersResponse is GET /config/providers: the configured
// providers only — small where /provider is multi-MB.
//
// SECURITY: the engine also returns a plaintext `key` field per provider — the
// user's API key. This struct deliberately has no such field, so encoding/json
// discards it during decode and it can never reach a catalog option, a log line
// or the wire. Do not "complete" this struct against the engine's response
// shape (MADR 0043 D4); TestConnectedCatalogDropsAPIKey guards it.
type connectedProvidersResponse struct {
	Providers []providerEntry   `json:"providers"`
	Default   map[string]string `json:"default"`
}

// modelOption converts one engine model into a picker row, carrying the
// metadata picker.OrderModels and the client badges read.
func modelOption(providerID string, m providerModel, modelID string) picker.Option {
	if m.ID != "" {
		modelID = m.ID
	}
	label := m.Name
	if label == "" {
		label = modelID
	}
	meta := map[string]string{}
	if m.ReleaseDate != "" {
		meta[picker.MetaReleaseDate] = m.ReleaseDate
	}
	if m.Status != "" {
		meta[picker.MetaStatus] = m.Status
	}
	desc := ""
	if m.Limit.Context > 0 {
		desc = humanContext(m.Limit.Context) + " context"
		meta[picker.MetaContext] = humanContext(m.Limit.Context)
	}
	if len(meta) == 0 {
		meta = nil
	}
	return picker.Option{
		ID:          providerID + "/" + modelID,
		Label:       label,
		Description: desc,
		Group:       providerID,
		Meta:        meta,
	}
}

// humanContext renders a context window the way a picker row should read.
func humanContext(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%dM", n/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	default:
		return fmt.Sprint(n)
	}
}

// modelsOf turns one provider entry into an ordered option list.
func (d *httpDialect) modelsOf(p providerEntry, defaults map[string]string) picker.Catalog {
	opts := make([]picker.Option, 0, len(p.Models))
	for modelID, m := range p.Models {
		if modelID == "" && m.ID == "" {
			continue
		}
		opts = append(opts, modelOption(p.ID, m, modelID))
	}
	def := ""
	if m := defaults[p.ID]; m != "" {
		def = p.ID + "/" + m
	}
	return picker.SingleCatalog(picker.SourceLive, picker.OrderModels(opts, def), def, true)
}

// maxDefaultCatalogModels bounds the connected-set default models.list reply.
// Kilo's own connected set runs larger than opencode's: the 7.4.20 spike's
// three default-connected providers alone (openrouter, huggingface, kilo)
// summed to 677 models (docs/kilo-spike-7.4.20/provider-summary.json) — the
// 200 cap opencode uses still serializes over the 32 KB default-catalog
// budget at that scale (MADR 0076 M4 #3: opencode's cap was never validated
// against Kilo's real, larger connected-set count). 150 is the largest count
// that stays under budget with headroom for longer Kilo model ids (e.g.
// `~vendor/model` aliases). Full per-provider lists stay available via
// ListModelsForLive (MADR 0043 D2/D8).
const maxDefaultCatalogModels = 150

// ListModelsLive implements [httpagent.ModelLister]: the **connected**
// providers' models, not every model the engine has heard of.
//
// The unscoped version of this call flattened 172 providers × 5,788 models into
// one alphabetically-sorted list — a 532 KB frame, over half the relay's 1 MiB
// message cap, whose first rows were a provider called `302ai`. Everything else
// is still reachable through ListModelProvidersLive + ListModelsForLive
// (MADR 0043 D2/D8).
func (d *httpDialect) ListModelsLive(ctx context.Context, api httpagent.API) (picker.Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogFetchTimeout)
	defer cancel()
	conn, err := d.connectedProviders(ctx, api)
	if err != nil {
		return picker.Catalog{}, err
	}
	opts := make([]picker.Option, 0, 128)
	// Preserve the engine's own provider order rather than sorting: it lists
	// the user's configured providers, and the first is the one they use.
	//
	// The result is a concatenation of per-provider lists, each newest-first
	// behind that provider's own default. Ordering is a within-group property,
	// not a global one — the picker renders a header per provider, so a global
	// sort would interleave providers under their own headings.
	for _, p := range conn.Providers {
		if p.ID == "" {
			continue
		}
		opts = append(opts, d.modelsOf(p, conn.Default).Options...)
	}
	def := ""
	if m := conn.Default["kilo"]; m != "" {
		def = "kilo/" + m
	}
	d.mu.Lock()
	if def == "" && d.defaultModelID != "" {
		def = d.defaultModelProvider + "/" + d.defaultModelID
	}
	d.mu.Unlock()
	opts = capDefaultCatalogModels(opts, def, maxDefaultCatalogModels)
	return picker.SingleCatalog(picker.SourceLive, opts, def, true), nil
}

// capDefaultCatalogModels keeps at most max options, ensuring def stays in
// the list when it would otherwise fall off the end. max <= 0 leaves opts
// unchanged.
func capDefaultCatalogModels(opts []picker.Option, def string, max int) []picker.Option {
	if max <= 0 || len(opts) <= max {
		return opts
	}
	out := make([]picker.Option, max)
	copy(out, opts[:max])
	if def == "" {
		return out
	}
	for _, o := range out {
		if o.ID == def {
			return out
		}
	}
	for _, o := range opts[max:] {
		if o.ID != def {
			continue
		}
		// Drop the last capped row so the default stays choosable.
		out[max-1] = o
		return out
	}
	return out
}

// ListModelProvidersLive implements [httpagent.ModelProviderLister]: connected
// providers first, then every other provider the engine knows, each labelled
// with the engine's own display name ("Kilo Gateway", "OpenRouter").
func (d *httpDialect) ListModelProvidersLive(ctx context.Context, api httpagent.API) (picker.Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogFetchTimeout)
	defer cancel()

	connected := map[string]struct{}{}
	opts := make([]picker.Option, 0, 192)
	def := ""

	// The connected set is cheap and is what the user almost always wants, so
	// fetch it first and let a failure of the expensive full list be survivable.
	if conn, err := d.connectedProviders(ctx, api); err == nil {
		for _, p := range conn.Providers {
			if p.ID == "" {
				continue
			}
			connected[p.ID] = struct{}{}
			if def == "" {
				def = p.ID
			}
			opts = append(opts, providerOption(p, true, conn.Default[p.ID]))
		}
	} else {
		d.log.Debug("kilo connected providers unavailable", slog.String("err", err.Error()))
	}

	var full fullProvidersResponse
	if err := api(ctx, "GET", "/provider", nil, &full); err != nil {
		if len(opts) == 0 {
			return picker.Catalog{}, err
		}
		// The connected set alone is a usable answer; say so rather than
		// failing the picker outright.
		d.log.Debug("kilo full provider list unavailable; connected only",
			slog.String("err", err.Error()))
		return picker.SingleCatalog(picker.SourceLive, opts, def, false), nil
	}
	// `connected` on the full response is authoritative when /config/providers
	// was unavailable; it is the same set by a cheaper name.
	for _, id := range full.Connected {
		connected[id] = struct{}{}
	}
	for _, p := range full.All {
		if p.ID == "" {
			continue
		}
		if _, seen := connected[p.ID]; seen {
			// Already listed from the connected set (or newly known to be
			// connected but not listed — cover that second case).
			if !hasOptionID(opts, p.ID) {
				opts = append(opts, providerOption(p, true, full.Default[p.ID]))
				if def == "" {
					def = p.ID
				}
			}
			continue
		}
		opts = append(opts, providerOption(p, false, full.Default[p.ID]))
	}
	// Connected first (stable within each band), then the rest by display name:
	// the long tail is browsed by search, so alphabetical is the right order
	// there and exactly the wrong one for the handful the user actually has.
	slices.SortStableFunc(opts, func(a, b picker.Option) int {
		ac, bc := a.Meta[picker.MetaConnected] == "true", b.Meta[picker.MetaConnected] == "true"
		if ac != bc {
			if ac {
				return -1
			}
			return 1
		}
		if ac {
			return 0
		}
		return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})
	return picker.SingleCatalog(picker.SourceLive, opts, def, false), nil
}

// ListModelsForLive implements [httpagent.ModelProviderLister].
func (d *httpDialect) ListModelsForLive(ctx context.Context, api httpagent.API, modelProvider string) (picker.Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogFetchTimeout)
	defer cancel()
	if modelProvider == "" {
		return picker.SingleCatalog(picker.SourceLive, nil, "", true), nil
	}
	// The connected set covers the common case at 99 KB; only fall through to
	// the 4.3 MB list for a provider the user has not configured.
	if conn, err := d.connectedProviders(ctx, api); err == nil {
		for _, p := range conn.Providers {
			if p.ID == modelProvider {
				return d.modelsOf(p, conn.Default), nil
			}
		}
	}
	var full fullProvidersResponse
	if err := api(ctx, "GET", "/provider", nil, &full); err != nil {
		return picker.Catalog{}, err
	}
	for _, p := range full.All {
		if p.ID == modelProvider {
			return d.modelsOf(p, full.Default), nil
		}
	}
	// Unknown provider: empty, not an error. The client may be asking about one
	// that has since left the engine's list.
	return picker.SingleCatalog(picker.SourceLive, nil, "", true), nil
}

// connectedProviders fetches the configured-providers endpoint.
func (d *httpDialect) connectedProviders(ctx context.Context, api httpagent.API) (connectedProvidersResponse, error) {
	var out connectedProvidersResponse
	if err := api(ctx, "GET", "/config/providers", nil, &out); err != nil {
		return connectedProvidersResponse{}, err
	}
	return out, nil
}

func providerOption(p providerEntry, connected bool, defaultModel string) picker.Option {
	label := p.Name
	if label == "" {
		label = p.ID
	}
	n := len(p.Models)
	desc := fmt.Sprintf("%d models", n)
	if n == 1 {
		desc = "1 model"
	}
	group := "All providers"
	if connected {
		group = "Connected"
	} else {
		desc += " · not configured"
	}
	meta := map[string]string{
		picker.MetaConnected:  fmt.Sprint(connected),
		picker.MetaModelCount: fmt.Sprint(n),
	}
	if defaultModel != "" {
		meta[picker.MetaDefaultModel] = p.ID + "/" + defaultModel
	}
	return picker.Option{ID: p.ID, Label: label, Description: desc, Group: group, Meta: meta}
}

func hasOptionID(opts []picker.Option, id string) bool {
	for _, o := range opts {
		if o.ID == id {
			return true
		}
	}
	return false
}
