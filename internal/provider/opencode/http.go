// Package opencode implements the OpenCode agent provider for mcremote.
//
// OpenCode is driven through its native HTTP + SSE server (`opencode serve`)
// over the shared internal/provider/httpagent transport: ONE long-lived engine
// process per daemon, with every session a cheap server-side object
// multiplexed over a single SSE stream.
//
// Why not per-session `opencode acp` subprocesses (removed in MADR 0019):
// every such process is a full Bun engine (~3s cold start, measured) and N of
// them contend on OpenCode's single global SQLite DB — both upstream
// WONTFIXes. The HTTP server is also the surface OpenCode itself recommends
// for programmatic clients (its own TUI is one), and it supports /undo and
// /redo, which its ACP surface does not. See
// docs/0011-MADR-opencode-provider-plan.md ("Performance addendum") and
// docs/0019-MADR-opencode-process-management-plan.md.
package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/agenterr"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// zenDefaultModel is the OpenCode Zen model seeded before the multi-MB
// /provider catalog returns. It is the engine's own default on the assessed
// release (MADR 0112 D6): isolated 1.18.21 reports default["opencode"] ==
// "big-pickle" at zero cost on every axis.
//
// This deliberately replaces a hand-maintained latency ranking. The previous
// seed and its runner-up — deepseek-v4-flash-free and north-mini-code-free —
// were both absent from the 1.18.21 Zen catalog, so the first session started
// from two model IDs that no longer existed and silently fell through. Ranking
// mutable catalog entries by measured latency does not survive the catalog
// changing underneath it; asking the engine what its default is does.
const zenDefaultModel = "big-pickle"

// staticModelIDs are the OpenCode Zen entries offered by the offline catalog.
// They are a picker convenience for a daemon whose engine is not up yet, not a
// preference order: AfterBoot replaces the default with whatever the live
// engine reports (MADR 0112 D6).
var staticModelIDs = []string{
	"big-pickle",
}

// staticModelOptions is the offline catalog used when the engine is not up (or
// its multi-MB /provider catalog has not returned yet). Live ListModelsLive
// merges the engine's real list over this.
func staticModelOptions() []picker.Option {
	opts := make([]picker.Option, 0, len(staticModelIDs))
	for _, id := range staticModelIDs {
		opts = append(opts, picker.Option{
			ID:    "opencode/" + id,
			Label: id,
			Group: "opencode",
		})
	}
	// Common third-party ids users pin in config (still AllowCustom for the rest).
	extras := []picker.Option{
		{ID: "anthropic/claude-sonnet-4-5", Label: "Claude Sonnet 4.5", Group: "anthropic"},
		{ID: "anthropic/claude-haiku-4-5", Label: "Claude Haiku 4.5", Group: "anthropic"},
		{ID: "openai/gpt-5", Label: "GPT-5", Group: "openai"},
	}
	return append(opts, extras...)
}

// Config configures the OpenCode provider (shared agent config shape).
type Config = httpagent.Config

// NewHTTP creates the OpenCode HTTP-transport provider.
func NewHTTP(cfg Config) *httpagent.Provider {
	return NewHTTPWithLogger(cfg, nil)
}

// NewHTTPWithLogger is like NewHTTP but sets a logger.
func NewHTTPWithLogger(cfg Config, log *slog.Logger) *httpagent.Provider {
	return NewHTTPWithToolFrameHook(cfg, log, nil)
}

// RawToolPartFrame captures a message.part.updated tool frame for live probing (MADR 0034 Phase 0).
type RawToolPartFrame struct {
	CallID string
	PartID string
	Tool   string
	Status string
	Title  string
	Input  string
	Output string
	Error  string
}

// NewHTTPWithToolFrameHook creates a Provider with a callback for raw tool frames (MADR 0034 Phase 0).
func NewHTTPWithToolFrameHook(cfg Config, log *slog.Logger, hook func(RawToolPartFrame)) *httpagent.Provider {
	l := slog.Default()
	if log != nil {
		l = log
	}
	d := &httpDialect{
		log:                  l,
		defaultModelProvider: "opencode",
		defaultModelID:       zenDefaultModel,
		onToolPartUpdated:    hook,
		pure:                 cfg.Pure,
		// Immutable for the life of the provider: policy is an operator
		// decision made at daemon start, never something a session can change.
		allowRemoteShare: cfg.AllowRemoteShare,
		allowRemoteShell: cfg.AllowRemoteShell,
	}
	return httpagent.NewWithLogger(d, cfg, log)
}

// httpDialect is the engine-level half: launch/health/SSE conventions plus
// the catalog-resolved default model shared by every session.
type httpDialect struct {
	log *slog.Logger
	// pure runs the serve process without external plugins (--pure).
	pure bool
	// allowRemoteShare/allowRemoteShell are the operator's remote-mutation
	// policy (MADR 0112 A8/A9). They are read-only after construction, so a
	// session can consult them without a lock and cannot widen them.
	//
	// Deliberately never logged next to a command, share URL or provider
	// response body: a log line pairing "shell allowed" with the command that
	// ran is a more useful artefact to an attacker than either alone.
	allowRemoteShare bool
	allowRemoteShell bool

	mu sync.Mutex
	// defaultModelProvider/ID is the engine-catalog fallback applied to
	// prompts when neither session nor config names a model. Seeded with
	// zenDefaultModel at construction; AfterBoot may refine it.
	defaultModelProvider string
	defaultModelID       string
	// engineVersion is the last /global/health version string (MADR 0020 KD10).
	engineVersion string
	// contextLimits is "providerID/modelID" → context-window size, harvested
	// from the same /provider catalog AfterBoot already fetches. It turns the
	// token counts on assistant messages into a usable "used of window" report
	// for /context and the client's context indicator.
	contextLimits map[string]int
	// onToolPartUpdated is an optional callback for live probing tool frames (MADR 0034 Phase 0).
	onToolPartUpdated func(RawToolPartFrame)

	// surfaces caches each model's advertised input modalities and reasoning
	// rungs by full "provider/model" ID. It is refreshed by AfterBoot from the
	// same /provider catalog that supplies contextLimits, so a session created
	// before that multi-MB fetch lands starts conservative and becomes truthful
	// without a restart (MADR 0112 A2/A14).
	surfaces modelSurfaceCache
}

var (
	_ httpagent.Dialect             = (*httpDialect)(nil)
	_ httpagent.ModelProviderLister = (*httpDialect)(nil)
	_ httpagent.ModelLister         = (*httpDialect)(nil)
	_ httpagent.AgentLister         = (*httpDialect)(nil)
	_ httpagent.CommandLister       = (*httpDialect)(nil)
	_ httpagent.CommandTabler       = (*httpDialect)(nil)
	_ httpagent.HealthyHook         = (*httpDialect)(nil)
	_ httpagent.VersionGate         = (*httpDialect)(nil)
	_ httpagent.EngineEventDialect  = (*httpDialect)(nil)
)

// EngineEventNeedsDiagnostics implements [httpagent.EngineEventDialect].
//
// Exactly two canonical events invalidate the sanitized diagnostics report. The
// 1.18.21 Event union has no formatter, config or skill update event, so those
// sections refresh explicitly or on open rather than being inferred from an
// event that does not exist (MADR 0112 A6, PLAN P7 step 10).
func (d *httpDialect) EngineEventNeedsDiagnostics(typ string) bool {
	switch typ {
	case "mcp.tools.changed", "lsp.updated":
		return true
	default:
		return false
	}
}

func (d *httpDialect) ID() provider.ID    { return provider.IDOpencode }
func (d *httpDialect) DefaultBin() string { return "opencode" }

// StaticModels implements [httpagent.ModelLister].
func (d *httpDialect) StaticModels(cfg httpagent.Config) picker.Catalog {
	def := cfg.Model
	if def == "" {
		def = "opencode/" + zenDefaultModel
	}
	return picker.SingleCatalog(picker.SourceStatic, staticModelOptions(), def, true)
}

// StaticAgents implements [httpagent.AgentLister]. Offline fallback of common
// primary agents; live GET /agent replaces this when the engine is up.
func (d *httpDialect) StaticAgents(cfg httpagent.Config) picker.Catalog {
	_ = cfg
	opts := []picker.Option{
		{ID: "build", Label: "build", Description: "Default agent", Group: "primary"},
		{ID: "plan", Label: "plan", Description: "Plan mode (no edits)", Group: "primary"},
	}
	return picker.SingleCatalog(picker.SourceStatic, opts, "build", false)
}

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
	def := "build"
	hasBuild := false
	for _, o := range opts {
		if o.ID == "build" {
			hasBuild = true
			break
		}
	}
	if !hasBuild && len(opts) > 0 {
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

// catalogFetchTimeout bounds one engine catalog fetch. /provider is ~4.3 MB and
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
	// Capabilities and Variants are the model's advertised input modalities and
	// reasoning rungs. Both are optional upstream: a model that omits them keeps
	// an unknown surface rather than a fabricated one (MADR 0112 A2/A14).
	Capabilities *modelCapabilities         `json:"capabilities"`
	Variants     map[string]json.RawMessage `json:"variants"`
}

// providerEntry is one model provider in /provider's `all` array.
type providerEntry struct {
	ID     string                   `json:"id"`
	Name   string                   `json:"name"`
	Models map[string]providerModel `json:"models"`
}

// fullProvidersResponse is GET /provider: every provider models.dev knows,
// 4.3 MB and 5,788 models on a stock host. `connected` names the ones the user
// actually has credentials for.
type fullProvidersResponse struct {
	All       []providerEntry   `json:"all"`
	Default   map[string]string `json:"default"`
	Connected []string          `json:"connected"`
}

// connectedProvidersResponse is GET /config/providers: the configured
// providers only — 99 KB and 113 models where /provider is 4.3 MB and 5,788.
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
	return m.surface().apply(picker.Option{
		ID:          providerID + "/" + modelID,
		Label:       label,
		Description: desc,
		Group:       providerID,
		Meta:        meta,
	})
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
// Measured 2026-07-29: four connected providers can yield ~450 models / ~85 KB,
// which still fits the 1 MiB relay frame but blows the 64 KB acceptance budget
// and makes the create-session picker unusable. Full per-provider lists stay
// available via ListModelsForLive (MADR 0043 D2/D8).
const maxDefaultCatalogModels = 200

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
	if m := conn.Default["opencode"]; m != "" {
		def = "opencode/" + m
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
// with the engine's own display name ("OpenCode Zen", "OpenCode Go").
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
		d.log.Debug("opencode connected providers unavailable", slog.String("err", err.Error()))
	}

	var full fullProvidersResponse
	if err := api(ctx, "GET", "/provider", nil, &full); err != nil {
		if len(opts) == 0 {
			return picker.Catalog{}, err
		}
		// The connected set alone is a usable answer; say so rather than
		// failing the picker outright.
		d.log.Debug("opencode full provider list unavailable; connected only",
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

func (d *httpDialect) ServeArgs(port int) []string {
	args := []string{"serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port)}
	if d.pure {
		args = append(args, "--pure")
	}
	return args
}

func (d *httpDialect) HealthPath() string { return "/global/health" }
func (d *httpDialect) EventsPath() string { return "/global/event" }

// OnHealthy implements [httpagent.HealthyHook]: records the engine version from
// GET /global/health. Does not refuse here — [CheckMinVersion] gates Start when
// session_tree is on so kill-switch configs can still run older engines.
func (d *httpDialect) OnHealthy(body []byte) error {
	var h struct {
		Healthy bool   `json:"healthy"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &h); err != nil {
		// Non-JSON health is unexpected but not fatal: keep boot path open.
		d.log.Debug("opencode health body not JSON", slog.String("err", err.Error()))
		return nil
	}
	v := strings.TrimSpace(h.Version)
	d.mu.Lock()
	d.engineVersion = v
	d.mu.Unlock()
	if v == "" {
		return nil
	}
	switch {
	case !VersionMeetsMin(v):
		// The hard floor. Still only a warning here: CheckMinVersion decides
		// whether to refuse Start, so a session_tree=false config can run an
		// older engine deliberately.
		d.log.Warn("opencode engine below minimum version for session-tree features",
			slog.String("version", v),
			slog.String("min", MinVersion),
			slog.String("hint", "upgrade opencode, or set providers.opencode.session_tree=false"),
		)
	case VersionIsKnownGood(v):
		d.log.Info("opencode engine version",
			slog.String("version", v),
			slog.String("min", MinVersion),
			slog.String("known_good", KnownGoodVersion),
		)
	default:
		// Above the floor but not the assessed release. httpagent calls this
		// hook exactly once per engine boot — inside ensureServer's startup
		// probe loop — so this is naturally one warning per boot, and a restart
		// onto a still-drifted engine warns again, which is what an operator
		// wants (MADR 0112 D1).
		d.log.Warn("opencode engine differs from the known-good release",
			slog.String("version", v),
			slog.String("known_good", KnownGoodVersion),
			slog.String("min", MinVersion),
			slog.String("hint", "supported, but this release has not been assessed against mcremote"),
		)
	}
	return nil
}

// CheckMinVersion implements [httpagent.VersionGate] (MADR 0020 KD10).
func (d *httpDialect) CheckMinVersion(cfg httpagent.Config) error {
	if !cfg.TreeEnabled() {
		return nil
	}
	d.mu.Lock()
	v := d.engineVersion
	d.mu.Unlock()
	if v == "" {
		// Health probe has not reported yet (or body had no version).
		return nil
	}
	if !VersionMeetsMin(v) {
		return VersionPinError(v)
	}
	return nil
}

// EngineVersion returns the last health-reported version, or "".
func (d *httpDialect) EngineVersion() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.engineVersion
}

// AfterBoot refines the fallback model from the engine catalog. It must be
// fast-path safe to skip: the dialect already seeds zenDefaultModel, and the
// transport runs this asynchronously so a 4MB /provider download never
// blocks session create.
//
// Preference order (MADR 0112 D6):
//  1. Engine-reported default["opencode"], when the catalog still lists it
//  2. The seed, when the catalog still lists it
//  3. Any OpenCode Zen model the catalog does list
//  4. The seed, unchanged
//
// The engine's own default comes first on purpose. mcremote used to keep a
// latency-ranked list of specific free-tier IDs and prefer those; two of the
// three had vanished from the catalog by 1.18.21. Whatever the engine reports
// is by construction current, and an explicit operator or session model still
// wins over all of this — this only decides what an unspecified model becomes.
func (d *httpDialect) AfterBoot(ctx context.Context, api httpagent.API) {
	// Bound catalog fetch — a hung provider list must not pin a goroutine forever.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var out struct {
		Default map[string]string `json:"default"`
		All     []struct {
			ID     string                     `json:"id"`
			Models map[string]json.RawMessage `json:"models"`
		} `json:"all"`
	}
	if err := api(ctx, "GET", "/provider", nil, &out); err != nil {
		d.log.Warn("opencode default-model resolve failed; using seeded fallback",
			slog.String("fallback", "opencode/"+zenDefaultModel),
			slog.String("err", err.Error()))
		return
	}
	available := map[string]struct{}{}
	limits := map[string]int{}
	surfaces := map[string]modelSurface{}
	for _, p := range out.All {
		for id, raw := range p.Models {
			if p.ID == "opencode" {
				available[id] = struct{}{}
			}
			var m providerModel
			if json.Unmarshal(raw, &m) != nil {
				continue
			}
			full := p.ID + "/" + id
			if m.Limit.Context > 0 {
				limits[full] = m.Limit.Context
			}
			surfaces[full] = m.surface()
		}
	}
	// The engine's default may name a model the catalog no longer carries, so
	// every candidate is checked against `available` before it is taken.
	chosen := ""
	for _, cand := range []string{strings.TrimSpace(out.Default["opencode"]), zenDefaultModel} {
		if cand == "" {
			continue
		}
		if _, ok := available[cand]; ok {
			chosen = cand
			break
		}
	}
	if chosen == "" && len(available) > 0 {
		// Nothing expected is present. Rather than prompt with an ID the engine
		// will reject, take a real catalog entry — lowest ID for determinism.
		ids := make([]string, 0, len(available))
		for id := range available {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		chosen = ids[0]
		d.log.Warn("opencode default and seed model both absent from the catalog",
			slog.String("engine_default", out.Default["opencode"]),
			slog.String("seed", zenDefaultModel),
			slog.String("using", chosen))
	}
	if chosen == "" {
		chosen = zenDefaultModel
	}
	d.mu.Lock()
	d.defaultModelProvider, d.defaultModelID = "opencode", chosen
	d.contextLimits = limits
	d.mu.Unlock()
	d.surfaces.replace(surfaces)
	d.log.Info("opencode default model resolved",
		slog.String("model", "opencode/"+chosen),
		slog.Int("catalog_models", len(available)),
		slog.Int("model_surfaces", d.surfaces.len()))
}

// fallbackModel returns the catalog default for prompts with no model.
func (d *httpDialect) fallbackModel() (string, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.defaultModelProvider, d.defaultModelID
}

// DecodeFrame accepts both OpenCode SSE envelope forms: /global/event wraps
// each event as {directory, project, payload:{type, properties}}; the
// per-directory /event stream sends the bare {type, properties} form.
func (d *httpDialect) DecodeFrame(data []byte) (string, json.RawMessage, string, bool) {
	var frame struct {
		Payload struct {
			Type       string          `json:"type"`
			Properties json.RawMessage `json:"properties"`
		} `json:"payload"`
		Type       string          `json:"type"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return "", nil, "", false
	}
	typ, props := frame.Type, frame.Properties
	if frame.Payload.Type != "" {
		typ, props = frame.Payload.Type, frame.Payload.Properties
	}
	return typ, props, sessionIDOf(props), true
}

// sessionIDOf pulls properties.sessionID (or nested part/info sessionID / Session.id).
// Lifecycle frames (session.created) use info.id without a top-level sessionID
// (MADR 0020); without info.id they demux as empty sid and are dropped.
func sessionIDOf(props json.RawMessage) string {
	var probe struct {
		SessionID string `json:"sessionID"`
		Part      struct {
			SessionID string `json:"sessionID"`
		} `json:"part"`
		Info struct {
			SessionID string `json:"sessionID"`
			ID        string `json:"id"`
		} `json:"info"`
	}
	if json.Unmarshal(props, &probe) != nil {
		return ""
	}
	if probe.SessionID != "" {
		return probe.SessionID
	}
	if probe.Part.SessionID != "" {
		return probe.Part.SessionID
	}
	if probe.Info.SessionID != "" {
		return probe.Info.SessionID
	}
	return probe.Info.ID
}

// ParentIDFromProps implements [httpagent.ChildFrame] for session.created/updated
// bootstrap demux when the child sid is not yet aliased (MADR 0020).
func (d *httpDialect) ParentIDFromProps(props json.RawMessage) string {
	return parentIDOf(props)
}

func parentIDOf(props json.RawMessage) string {
	var probe struct {
		Info struct {
			ParentID string `json:"parentID"`
		} `json:"info"`
		ParentID string `json:"parentID"`
	}
	if json.Unmarshal(props, &probe) != nil {
		return ""
	}
	if probe.Info.ParentID != "" {
		return probe.Info.ParentID
	}
	return probe.ParentID
}

func (d *httpDialect) NewSession(h httpagent.Host) httpagent.DialectSession {
	return &httpSession{
		d:         d,
		h:         h,
		partText:  make(map[string]string),
		partType:  make(map[string]string),
		msgRole:   make(map[string]string),
		subagents: make(map[string]subagentState),
	}
}

type toolEmit struct{ status, name, kind, text string }

// httpSession is the per-session half: OpenCode REST shapes and SSE event
// translation.
type httpSession struct {
	d *httpDialect
	h httpagent.Host

	mu sync.Mutex
	// partText tracks the actual accumulated text per part id (NOT a byte count):
	// SSE part.updated frames carry the FULL text each time, so we emit only the
	// suffix beyond what we already streamed. Storing the real text (rather than a
	// cursor) lets a snapshot re-align after a dropped/reordered delta instead of
	// slicing at a stale offset — which dropped, duplicated, or split runes.
	partText map[string]string
	// partType remembers part type (text/reasoning/tool/…) from part.updated
	// so subsequent part.delta frames can be classified without a full snapshot.
	partType map[string]string
	// msgRole records message role by id; user-authored parts echo back over
	// SSE and must not become assistant chunks.
	msgRole map[string]string
	// thinkingLevel is the OpenCode `variant` this session sends on prompt and
	// command. The resting value is defaultVariant, which is never put on the
	// wire (MADR 0112 A14).
	thinkingLevel string
	// seenTools distinguishes the first sighting of a tool call (tool_call)
	// from updates (tool_call_update).
	seenTools map[string]struct{}
	// autoHandled remembers permission ids this session already auto-approved.
	//
	// Host.TakePending alone is not enough to dedupe: the engine sends both
	// permission.asked and permission.updated for one id, and if the second
	// arrives after the first has already been claimed and answered, its
	// TrackPermission *resurrects* the id and a second goroutine claims it
	// again — two replies for one permission. Bounded by maxAutoHandled
	// (MADR 0044).
	autoHandled map[string]struct{}
	autoOrder   []string
	// approvalMu serialises the auto-approval snapshot with its emit, and is
	// deliberately NOT o.mu: autoApprove runs one goroutine per permission
	// (permission.go), so two concurrent emits could deliver snapshots out of
	// order and a stale, shorter list would replace a longer one — losing an
	// approval for good under the event's replace semantics. o.mu is unsuitable
	// because it is already held across non-blocking chunk emits and must not
	// gain a blocking-emit path (MADR 0051 §4.3).
	approvalMu    sync.Mutex
	autoApprovals []event.ApprovalItem
	// lastToolEmit holds the last payload actually emitted per tool id, so the
	// engine's repeated part.updated frames do not each cost a frame (MADR
	// 0034 D1). Cleared by turnCleanup alongside seenTools.
	lastToolEmit map[string]toolEmit
	// subagents tracks this turn's child agent sessions: agent session id →
	// name, task and status. Completed ids are retained (not deleted) so a
	// post-completion session.updated cannot reopen one; the whole map is
	// dropped by turnCleanup. Published as event.TypeSubagents — the phone
	// renders a panel, never transcript items (MADR 0051 D8).
	subagents map[string]subagentState
	// subagentsPublished latches that a non-empty set went out this turn, so
	// the clear at turn end is only sent to sessions that actually had one.
	subagentsPublished bool
	// runningSent latches that "running" has already been announced, so the
	// engine's repeated session.status busy frames do not each cost a frame
	// (MADR 0024). Cleared by any other status and by turnCleanup.
	runningSent bool
	// lastUsed/lastSize/usageSent hold the last usage report actually emitted,
	// so an unchanged token count is not re-sent (MADR 0024). usageSent is
	// cleared by turnCleanup so every turn reports at least once.
	lastUsage usageKey
	lastUsed  int
	lastSize  int
	usageSent bool
}

var _ httpagent.DialectSession = (*httpSession)(nil)
var _ httpagent.TreeIdleConfirmer = (*httpSession)(nil)

// dir is the ?directory= query OpenCode uses to scope requests to a project.
func (o *httpSession) dir() string {
	return "?directory=" + url.QueryEscape(o.h.CWD())
}

func (o *httpSession) Create(ctx context.Context, opts provider.StartOptions) (string, error) {
	body := map[string]any{}
	if opts.Name != "" {
		body["title"] = opts.Name
	}
	// Always pin a model at create time so the server never falls through to
	// its broken zen/… default (UnknownError: Model not found).
	if mp, mid := o.resolveModel(); mid != "" {
		// Create expects {providerID, id}. Prompt uses {providerID, modelID}
		// — different keys on the same engine (OpenCode 1.18 OpenAPI).
		body["model"] = map[string]string{"providerID": mp, "id": mid}
	}
	start := time.Now()
	var created struct {
		ID string `json:"id"`
	}
	if err := o.h.API()(ctx, "POST", "/session"+o.dir(), body, &created); err != nil {
		return "", err
	}
	o.h.Log().Info("opencode session create",
		slog.String("agent_session_id", created.ID),
		slog.Duration("ms", time.Since(start)),
		slog.String("model", o.h.Model()),
	)
	// Advertise slash commands so manager/mobile treat /init etc. as agent
	// commands (MADR 0020 Sprint 5). Best-effort; static fallback inside.
	o.advertiseCommands(ctx)
	// Advertise the primary agents as session modes so the switcher and /plan
	// work (MADR 0022). Best-effort; static fallback inside.
	o.advertiseModes(ctx)
	// A new session has no upstream variant, so the caller's requested rung is
	// the only candidate and still has to be advertised (PLAN P3 step 6).
	o.applyStartThinkingLevel("", opts.ThinkingLevel)
	return created.ID, nil
}

// resolveModel returns the effective provider/model for this session: explicit
// session/config model first, then the dialect fallback (seeded zen default).
func (o *httpSession) resolveModel() (providerID, modelID string) {
	mp, mid := splitModel(o.h.Model())
	if mid == "" {
		mp, mid = o.d.fallbackModel()
	}
	return mp, mid
}

func (o *httpSession) Resume(ctx context.Context, agentSessionID string) (string, error) {
	// 1.18.21 persists the session's model and its reasoning variant, so both
	// are read from authoritative session state rather than inferred from
	// message history (PLAN P3 step 4).
	var info struct {
		ID    string `json:"id"`
		Model struct {
			ProviderID string `json:"providerID"`
			ID         string `json:"id"`
			Variant    string `json:"variant"`
		} `json:"model"`
	}
	if err := o.h.API()(ctx, "GET", "/session/"+agentSessionID+o.dir(), nil, &info); err != nil {
		return "", err
	}
	// Adopt the upstream model when it is a complete ref the catalog still
	// carries; otherwise keep the active model rather than pinning the session
	// to an id the engine would reject on the next prompt.
	if mp, mid := strings.TrimSpace(info.Model.ProviderID), strings.TrimSpace(info.Model.ID); mp != "" && mid != "" {
		full := mp + "/" + mid
		if _, known := o.d.surfaces.lookup(full); known || o.d.surfaces.len() == 0 {
			o.h.RecordModel(full)
		}
	}
	// The engine is authoritative for the rung on resume; a stored one is only
	// a fallback when upstream has none at all (PLAN P3 step 6).
	o.applyStartThinkingLevel(info.Model.Variant, o.h.StartThinkingLevel())
	// Command and mode catalogs are session-agnostic; re-advertise both so
	// autocomplete and the mode strip survive a resume.
	o.advertiseCommands(ctx)
	o.advertiseModes(ctx)
	return info.ID, nil
}

// Replay rebuilds the daemon-side history ring for a resumed session from the
// server's message log (best-effort).
func (o *httpSession) Replay(ctx context.Context) {
	var msgs []struct {
		Info struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"info"`
		Parts []nativePart `json:"parts"`
	}
	if err := o.h.API()(ctx, "GET", "/session/"+o.h.AgentSessionID()+"/message"+o.dir(), nil, &msgs); err != nil {
		o.h.Log().Warn("resume message replay failed", slog.String("err", err.Error()))
		return
	}
	for _, m := range msgs {
		for _, part := range m.Parts {
			// Replayed parts are authoritative snapshots by definition: they
			// are the engine's current truth, not increments on top of what a
			// previous connection streamed.
			if part.MessageID == "" {
				part.MessageID = m.Info.ID
			}
			ev, ok := mapPart(m.Info.Role, part, true, o.h.Log())
			if ok {
				blank := strings.TrimSpace(ev.Text) == ""
				if !blank || ev.Type == event.TypeToolCall || ev.Type == event.TypeArtifact {
					// A terminal failure must not come back as a completed
					// call: the status is carried from the replayed state.
					o.h.EmitReplay(ev)
				}
			}
			// A completed tool's attachments are artifacts in their own right,
			// identified by the tool part plus their index so replay and live
			// streaming produce the same rows (PLAN P5 step 1).
			for _, art := range toolArtifacts(part, true) {
				o.h.EmitReplay(art)
			}
		}
	}
}

// NewPromptMessageID implements [httpagent.IdentifiedPromptDialectSession].
//
// The 1.18.21 MessageID schema requires only a "msg" prefix and persists the
// exact value, so a caller-supplied id needs no upstream API extension
// (MADR 0112 A3).
func (o *httpSession) NewPromptMessageID() string { return "msg_" + uuid.NewString() }

// PromptWithMessageID implements [httpagent.IdentifiedPromptDialectSession].
func (o *httpSession) PromptWithMessageID(ctx context.Context, messageID string, parts []provider.Content) error {
	return o.prompt(ctx, messageID, parts)
}

func (o *httpSession) Prompt(ctx context.Context, parts []provider.Content) error {
	return o.prompt(ctx, "", parts)
}

// prompt submits one turn. messageID is the preassigned native identity, or ""
// to let the engine mint one.
func (o *httpSession) prompt(ctx context.Context, messageID string, parts []provider.Content) error {
	// OpenCode custom slash commands → POST …/command (Sprint 5). Manager only
	// forwards names advertised via available_commands.
	if name, args, ok := soleSlashCommand(parts); ok {
		return o.submitCommand(ctx, messageID, name, args)
	}
	// Non-text blocks become stable FilePartInput data URLs in composed order.
	// Validation happens here, before any request exists, so an oversized or
	// malformed attachment fails as a rejected prompt rather than a failed turn.
	apiParts, err := o.promptParts(parts)
	if err != nil {
		return err
	}
	body := map[string]any{"parts": apiParts}
	if messageID != "" {
		body["messageID"] = messageID
	}
	mp, mid := o.resolveModel()
	if mid != "" {
		// Prompt uses modelID (not id). OpenCode 1.18's prompt_async schema
		// requires {providerID, modelID}; create uses {providerID, id}.
		// Unifying them to "id" breaks every prompt with HTTP 400.
		body["model"] = map[string]string{"providerID": mp, "modelID": mid}
	}
	// Optional agent name (MADR 0020 Sprint 3): "build", "plan", …
	if agent := strings.TrimSpace(o.h.Agent()); agent != "" {
		body["agent"] = agent
	}
	// Optional reasoning-effort variant. Omitted at the reserved default, which
	// the engine normalises away itself (MADR 0112 A14).
	if v := o.requestVariant(); v != "" {
		body["variant"] = v
	}
	start := time.Now()
	// prompt_async returns immediately; the turn streams over SSE and ends
	// with session.idle.
	err = o.h.API()(ctx, "POST", "/session/"+o.h.AgentSessionID()+"/prompt_async"+o.dir(), body, nil)
	o.h.Log().Info("opencode prompt_async",
		slog.String("agent_session_id", o.h.AgentSessionID()),
		slog.Duration("enqueue_ms", time.Since(start)),
		slog.String("model", mp+"/"+mid),
		slog.String("agent", o.h.Agent()),
		slog.Bool("ok", err == nil),
	)
	return err
}

// Abort cancels the parent session and best-effort aborts known children (A7).
func (o *httpSession) Abort(ctx context.Context) error {
	parent := o.h.AgentSessionID()
	if parent == "" {
		return nil
	}
	err := o.h.API()(ctx, "POST", "/session/"+parent+"/abort"+o.dir(), nil, nil)
	// Best-effort child abort cascade (MADR 0020 §6.9).
	for _, id := range o.discoverTreeChildren(ctx, parent) {
		_ = o.h.API()(ctx, "POST", "/session/"+id+"/abort"+o.dir(), nil, nil)
	}
	return err
}

// Delete purges the server-side session (session.delete / hard purge).
func (o *httpSession) Delete(ctx context.Context) error {
	id := o.h.AgentSessionID()
	if id == "" {
		return nil
	}
	return o.h.API()(ctx, "DELETE", "/session/"+id+o.dir(), nil, nil)
}

func (o *httpSession) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error {
	response := "reject"
	if !cancelled {
		switch optionID {
		case "once", "allow", "allow_once":
			response = "once"
		case "always", "allow_always":
			response = "always"
		}
	}
	return o.respondPermissionEngine(ctx, permissionID, response)
}

// fromChild reports whether the SSE frame being handled originated in a child
// (sub-agent) session rather than this one.
//
// Child *content* never reaches the transcript: the sub-agent reports to the
// main agent over the engine's own channel, and the parent's own reply carries
// the conclusion. Measured on opencode 1.18.7, a single sub-agent doing a
// two-file read produced 43–81% of a turn's streamed text (MADR 0051 D6).
//
// Only valid inside HandleEvent: httpagent clears eventAgentID once dispatch
// returns, so a goroutine that outlives the handler sees "" and must capture
// the answer before it starts.
func (o *httpSession) fromChild() bool {
	ev := o.h.EventAgentSessionID()
	return ev != "" && ev != o.h.AgentSessionID()
}

// HandleEvent translates one SSE event into daemon events.
func (o *httpSession) HandleEvent(typ string, props json.RawMessage) {
	switch typ {
	case "message.updated":
		var p struct {
			Info struct {
				ID   string `json:"id"`
				Role string `json:"role"`
				// Token counts and the model that produced them: an assistant
				// message names its model flat (modelID/providerID), unlike the
				// nested ModelRef the REST bodies take.
				Tokens     *msgTokens `json:"tokens"`
				Cost       *float64   `json:"cost"`
				ModelID    string     `json:"modelID"`
				ProviderID string     `json:"providerID"`
				Model      *msgModel  `json:"model"`
			} `json:"info"`
		}
		if json.Unmarshal(props, &p) == nil && p.Info.ID != "" {
			o.mu.Lock()
			// Keyed by message id, so recording a child's role is harmless and
			// keeps the part handlers below able to classify what they skip.
			o.msgRole[p.Info.ID] = p.Info.Role
			o.mu.Unlock()
			// A child's token counts are not this session's context usage: the
			// indicator reports the conversation the user is having, not the
			// sub-agent's private window (MADR 0051 D7).
			if o.fromChild() {
				return
			}
			model := p.Info.Model
			if model == nil && p.Info.ModelID != "" {
				model = &msgModel{ProviderID: p.Info.ProviderID, ModelID: p.Info.ModelID}
			}
			o.emitUsage(p.Info.Role, p.Info.Tokens, model, p.Info.Cost)
		}

	// OpenCode 1.18 streams assistant text primarily via part.delta (token
	// fragments). part.updated carries full snapshots / tool state. Handling
	// only updated made the phone wait until the turn finished (~seconds)
	// before any assistant text appeared.
	case "message.part.delta":
		var p struct {
			MessageID string `json:"messageID"`
			PartID    string `json:"partID"`
			Field     string `json:"field"`
			Delta     string `json:"delta"`
		}
		if json.Unmarshal(props, &p) != nil || p.Delta == "" {
			return
		}
		if p.Field != "" && p.Field != "text" {
			return
		}
		// Before the partText bookkeeping below, so a child's part ids never
		// enter the per-turn maps that only turnCleanup clears.
		if o.fromChild() {
			return
		}
		o.mu.Lock()
		role := o.msgRole[p.MessageID]
		ptype := o.partType[p.PartID]
		// M9: stream text only once we KNOW the message is the assistant's. A
		// missed message.updated (role) frame leaves role "" — defaulting that to
		// assistant echoed the user's own prompt back as an assistant bubble. The
		// authoritative part.updated snapshot re-delivers the text once role is
		// known, so skipping here loses nothing.
		if role != "assistant" {
			o.mu.Unlock()
			return
		}
		if p.PartID != "" {
			// M8: accumulate the real text (not a byte count) so the part.updated
			// catch-up compares against exactly what we streamed.
			o.partText[p.PartID] += p.Delta
		}
		o.mu.Unlock()
		// Unknown part type defaults to assistant text; reasoning parts are
		// classified once part.updated announces type=reasoning.
		t := event.TypeAssistantChunk
		if ptype == "reasoning" {
			t = event.TypeThoughtChunk
		}
		// Append delta, carrying the identity the later authoritative snapshot
		// will replace (MADR 0112 A3).
		o.h.Emit(event.Event{
			Type:            t,
			Text:            p.Delta,
			NativeMessageID: p.MessageID,
			NativePartID:    p.PartID,
		})

	case "message.part.updated":
		var p struct {
			Part nativePart `json:"part"`
		}
		if json.Unmarshal(props, &p) != nil {
			return
		}
		// Child text, reasoning and tool calls all arrive here; none of them is
		// this conversation. Returning before the partType write keeps the
		// per-turn maps parent-only (MADR 0051 D6).
		if o.fromChild() {
			return
		}
		part := p.Part
		o.mu.Lock()
		role := o.msgRole[part.MessageID]
		if part.ID != "" && part.Type != "" {
			o.partType[part.ID] = part.Type
		}
		o.mu.Unlock()
		if role == "user" {
			return
		}
		switch part.Type {
		case "text", "reasoning":
			o.emitTextSnapshot(part.MessageID, part.ID, part.Type, part.Text)
		case "file":
			if art, ok := mapPart(role, part, true, o.h.Log()); ok {
				o.h.Emit(art)
			}
		case "tool":
			id := part.CallID
			if id == "" {
				id = part.ID
			}
			if o.d != nil && o.d.onToolPartUpdated != nil {
				o.d.onToolPartUpdated(RawToolPartFrame{
					CallID: id,
					PartID: part.ID,
					Tool:   part.Tool,
					Status: part.State.Status,
					Title:  part.State.Title,
					Input:  string(part.State.Input),
					Output: part.State.Output,
					Error:  part.State.Error,
				})
			}
			status := mapToolStatus(part.State.Status)
			detail := strings.TrimSpace(part.State.Title)
			if detail == "" {
				detail = shortJSON(part.State.Input, 300)
			}
			if out := strings.TrimRight(part.State.Output, " \t\n"); out != "" {
				detail = clipBlock(out, maxToolOutputChars)
			}
			if part.State.Error != "" {
				detail = clip(part.State.Error, 300)
			}
			name := firstNonEmpty(part.State.Title, part.Tool, "tool")
			kind := kindForTool(part.Tool)

			e := toolEmit{status, name, kind, detail}
			isNew := o.noteTool(id)
			changed := o.noteToolEmit(id, e)
			if !isNew && !changed {
				return
			}
			o.h.Emit(event.Event{
				Type:            toolEventType(status, isNew),
				ToolID:          id,
				ToolName:        name,
				ToolKind:        kind,
				Status:          status,
				Text:            detail,
				NativeMessageID: part.MessageID,
				NativePartID:    part.ID,
			})
			// Attachments ride the same identity, so a re-sent tool snapshot
			// replaces its artifacts rather than duplicating them.
			for _, art := range toolArtifacts(part, true) {
				o.h.Emit(art)
			}
		}

	case "permission.asked", "permission.updated":
		if p, ok := normalizePermissionAsk(props); ok {
			o.emitPermissionAsk(p)
		}

	case "permission.v2.asked":
		if p, ok := normalizePermissionV2Ask(props); ok {
			o.emitPermissionAsk(p)
		}

	case "permission.replied", "permission.v2.replied":
		var p struct {
			RequestID    string `json:"requestID"`
			PermissionID string `json:"permissionID"`
			ID           string `json:"id"`
		}
		_ = json.Unmarshal(props, &p)
		id := firstNonEmpty(p.RequestID, p.PermissionID, p.ID)
		if id == "" {
			return
		}
		if o.h.TakePending(id) {
			o.h.Emit(event.Event{
				Type:         event.TypePermissionResolved,
				PermissionID: id,
				Status:       event.PermissionStatusResolved,
			})
		}

	case "question.asked", "question.v2.asked":
		o.handleQuestionAsked(props)

	case "question.replied", "question.v2.replied":
		o.handleQuestionResolved(props, false)

	case "question.rejected", "question.v2.rejected":
		o.handleQuestionResolved(props, true)

	case "session.diff":
		o.handleSessionDiff(props)

	case "command.executed":
		o.handleCommandExecuted(props)

	case "todo.updated":
		o.handleTodoUpdated(props)

	case "session.created":
		o.handleSessionLifecycle(props, true)

	case "session.updated":
		o.handleSessionLifecycle(props, false)

	case "session.deleted":
		o.handleSessionDeleted(props)

	case "session.status":
		o.handleSessionStatus(props)

	// The agent can retract content it already streamed. Both removals are
	// tombstones keyed on native identity; unknown ids are idempotent no-ops at
	// the consumer, so no local bookkeeping is needed here (MADR 0112 A3).
	case "message.removed":
		var p struct {
			MessageID string `json:"messageID"`
		}
		if json.Unmarshal(props, &p) != nil || p.MessageID == "" {
			return
		}
		if o.fromChild() {
			return
		}
		o.h.Emit(event.Event{
			Type:            event.TypeTranscriptRemove,
			NativeMessageID: p.MessageID,
		})

	case "message.part.removed":
		var p struct {
			MessageID string `json:"messageID"`
			PartID    string `json:"partID"`
		}
		if json.Unmarshal(props, &p) != nil || p.MessageID == "" || p.PartID == "" {
			return
		}
		if o.fromChild() {
			return
		}
		// Forget any accumulated text for the part so a later snapshot for a
		// reused id cannot be compared against text that no longer exists.
		o.mu.Lock()
		delete(o.partText, p.PartID)
		delete(o.partType, p.PartID)
		o.mu.Unlock()
		o.h.Emit(event.Event{
			Type:            event.TypeTranscriptRemove,
			NativeMessageID: p.MessageID,
			NativePartID:    p.PartID,
		})

	// Compaction replaces history upstream. It emits one bounded notice and
	// deliberately performs no replay: re-fetching the message log here would
	// append the whole conversation again, which is exactly the append-only
	// growth MADR 0112 A3 exists to prevent. A later full update for a
	// compacted tool part replaces it by native id through the ordinary
	// snapshot path.
	case "session.compacted":
		if o.fromChild() {
			return
		}
		o.h.Emit(event.Event{
			Type: event.TypeNotice,
			Text: "Conversation compacted — earlier turns were summarised to free context.",
		})

	case "session.idle":
		// Tree-aware EndTurn (MADR 0020): mark this agent node idle and only
		// complete the phone turn when every known tree node is idle.
		sid := firstNonEmpty(sessionIDOf(props), o.h.EventAgentSessionID(), o.h.AgentSessionID())
		o.noteNodeIdle(sid)
		o.tryTreeEndTurn()

	case "session.error":
		var p struct {
			Error struct {
				Name string `json:"name"`
				Data struct {
					Message string `json:"message"`
				} `json:"data"`
			} `json:"error"`
		}
		_ = json.Unmarshal(props, &p)
		sid := firstNonEmpty(sessionIDOf(props), o.h.EventAgentSessionID(), o.h.AgentSessionID())
		// Parent error is terminal for the turn; child error isolates (Q3).
		if sid == "" || sid == o.h.AgentSessionID() {
			o.h.NoteNodeStatus(o.h.AgentSessionID(), httpagent.NodeIdle)
			active := o.h.EndTurn()
			if active {
				o.finishAllSubagents()
				// An errored turn is still a finished turn: without this the
				// per-turn maps (notably seenTools) leaked into the next one,
				// so its first tool re-used call id emitted tool_call_update
				// with no preceding tool_call.
				o.turnCleanup()
				// turnCleanup dropped the map; tell the phone to drop the panel.
				o.clearSubagents()
				// Same reasoning for the approval audit: an errored turn still
				// approved whatever it approved, and the card must be closed
				// rather than left running into the next turn.
				o.finishApprovals()
			}
			if p.Error.Name == "MessageAbortedError" {
				if active {
					o.h.Emit(event.Event{Type: event.TypeTurnComplete, Status: "cancelled", StopReason: "cancelled"})
					o.emitStatus("idle")
				}
				return
			}
			msg := firstNonEmpty(p.Error.Data.Message, p.Error.Name, "agent error")
			// Present classifies and rewrites 429/529/quota dumps; fall back
			// to a clipped raw message for unclassified failures.
			cls := agenterr.Present(msg, time.Now())
			out := cls.Message
			if out == "" {
				out = clip(msg, 400)
			}
			o.h.Emit(event.Event{
				Type:      event.TypeError,
				Error:     out,
				ErrorKind: string(cls.Kind),
				RetryAt:   cls.ResetAt,
			})
			o.emitStatus("error")
			return
		}
		o.h.NoteNodeStatus(sid, httpagent.NodeIdle)
		// A sub-agent that errored is not one that finished: the panel says so,
		// and the notice below stays because a failed sub-agent is worth a
		// transcript line even though its output never is.
		o.finishSubagent(sid, event.SubagentStatusFailed)
		msg := firstNonEmpty(p.Error.Data.Message, p.Error.Name, "subagent error")
		o.h.Emit(event.Event{
			Type: event.TypeNotice,
			Text: clip("Subagent error: "+msg, 400),
		})
		// Child finished (with error); parent may still be busy.
		o.tryTreeEndTurn()
	}
}

// emitTextCatchUp reconciles the authoritative full text of a part against
// what was already streamed and emits only the missing tail. part.updated
// carries the FULL text of the part; so does the message log used by Resync.
// Comparing the real accumulated text (M8) means a dropped/reordered delta no
// longer corrupts output: on a clean prefix we emit the tail; when the
// snapshot lags the deltas we already streamed we emit nothing; on divergence
// we re-align to the authoritative snapshot at a rune-safe boundary instead of
// slicing at a stale byte cursor (which dropped text, duplicated it, or split
// a multi-byte rune into replacement characters).
//
// The o.mu hold spans the Emit so concurrent catch-ups (SSE pump vs resync)
// cannot interleave their tails out of order; chunk emits never block
// (drop-on-full), so the hold is bounded.
// emitTextSnapshot delivers a full `message.part.updated` body as an
// authoritative replacement for that native part.
//
// This supersedes the old delta-diffing catch-up. Diffing was only ever a way
// to avoid duplicating text on an append-only client; now that a snapshot
// carries its identity and Replace, the consumer can simply take it, and live
// streaming and replay agree by construction instead of by two implementations
// of the same arithmetic (MADR 0112 A3, PLAN P4 step 4).
//
// partText is still tracked so a stale snapshot lagging the deltas already
// streamed does not shorten the visible text.
func (o *httpSession) emitTextSnapshot(messageID, partID, partType, full string) {
	o.mu.Lock()
	prev := o.partText[partID]
	if full == prev {
		o.mu.Unlock()
		return
	}
	if strings.HasPrefix(prev, full) && prev != full {
		// Snapshot is behind what we already streamed; keep the longer text.
		o.mu.Unlock()
		return
	}
	if partID != "" {
		o.partText[partID] = full
	}
	o.mu.Unlock()
	if strings.TrimSpace(full) == "" {
		return
	}
	t := event.TypeAssistantChunk
	if partType == "reasoning" {
		t = event.TypeThoughtChunk
	}
	o.h.Emit(event.Event{
		Type:            t,
		Text:            full,
		NativeMessageID: messageID,
		NativePartID:    partID,
		Replace:         true,
	})
}

// turnCleanup resets per-turn state once a turn ends (session.idle or a
// resync-recovered turn-end). All per-turn maps are cleared unconditionally
// so they never grow stale over a session's lifetime.
func (o *httpSession) turnCleanup() {
	o.mu.Lock()
	o.partText = make(map[string]string)
	o.partType = make(map[string]string)
	o.msgRole = make(map[string]string)
	o.seenTools = nil
	o.lastToolEmit = nil
	o.subagents = nil
	// A new turn must re-announce "running" and re-report usage even if the
	// numbers are unchanged, so the phone never sits on stale turn state
	// (MADR 0024). emitStatus clears runningSent on any non-running status
	// too; this covers a turn that ends without emitting one at all.
	o.runningSent = false
	o.usageSent = false
	o.mu.Unlock()
}

// emitStatus emits a session_status, suppressing a repeated "running".
//
// OpenCode re-sends session.status busy for every step of a turn. Each repeat
// costs a WebSocket frame and — because session_status is not batchable on the
// phone — forces an immediate transcript commit, defeating the client's own
// 32ms coalescing window (MADR 0024 §1.1). Any other status clears the latch
// and is always delivered, so a status the client must not miss (idle, error,
// and the MADR 0014 resync corrections that emit them) is never suppressed.
func (o *httpSession) emitStatus(status string) {
	o.mu.Lock()
	if status == "running" {
		if o.runningSent {
			o.mu.Unlock()
			return
		}
		o.runningSent = true
	} else {
		o.runningSent = false
	}
	o.mu.Unlock()
	o.h.Emit(event.Event{Type: event.TypeSessionStatus, Status: status})
}

// Resync is implemented in resync.go (MADR 0020 PR5 tree-aware extension of H4).

// noteTool records that a tool id has been seen; returns true if it is new.
func (o *httpSession) noteTool(id string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seenTools == nil {
		o.seenTools = make(map[string]struct{})
	}
	_, seen := o.seenTools[id]
	o.seenTools[id] = struct{}{}
	return !seen
}

// noteToolEmit records the last emitted payload for a tool id; returns true if changed.
func (o *httpSession) noteToolEmit(id string, e toolEmit) bool {
	if id == "" {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.lastToolEmit == nil {
		o.lastToolEmit = make(map[string]toolEmit)
	}
	last, ok := o.lastToolEmit[id]
	o.lastToolEmit[id] = e
	return !ok || last != e
}

func toolEventType(status string, isNew bool) event.Type {
	if isNew {
		return event.TypeToolCall
	}
	return event.TypeToolUpdate
}

// kindForTool maps an OpenCode tool name onto the ACP tool-kind vocabulary
// carried in event.Event.ToolKind, so clients classify actions uniformly
// across transports ("Ran N commands", "Edited N files", …).
func kindForTool(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash":
		return "execute"
	// apply_patch is one of the 14 built-in tool IDs on 1.18.21 and edits files
	// exactly as edit/write do; it was previously falling through to "other",
	// so a patch-based turn rendered as an unclassified action (MADR 0112 D7).
	case "edit", "write", "patch", "apply_patch", "multiedit":
		return "edit"
	case "read":
		return "read"
	case "grep", "glob", "list", "ls":
		return "search"
	case "webfetch", "websearch":
		return "fetch"
	case "todowrite", "todoread":
		return "think"
	case "":
		return ""
	default:
		return "other"
	}
}

func mapToolStatus(s string) string {
	switch s {
	case "completed":
		return "completed"
	case "error":
		return "failed"
	case "running":
		return "running"
	case "pending":
		return "pending"
	default:
		return s
	}
}

// splitModel turns "provider/model" into its OpenCode parts. A bare name is
// treated as an opencode Zen model.
func splitModel(m string) (providerID, modelID string) {
	m = strings.TrimSpace(m)
	if m == "" {
		return "", ""
	}
	if i := strings.IndexByte(m, '/'); i > 0 {
		return m[:i], m[i+1:]
	}
	return "opencode", m
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// commonPrefixLen returns the byte length of the longest common prefix of a and
// b that ends on a UTF-8 rune boundary, so slicing b at the result never splits
// a multi-byte rune. Used to re-align part text after a dropped/reordered delta.
func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	// Back off to the start of the rune straddling the divergence point.
	for i > 0 && i < len(b) && !utf8.RuneStart(b[i]) {
		i--
	}
	return i
}

func clip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func shortJSON(raw json.RawMessage, max int) string {
	if len(raw) == 0 {
		return ""
	}
	return clip(string(raw), max)
}

// maxToolOutputChars caps emitted tool stdout/stderr detail. Aligned with the client's
// kMaxExpandedDetailChars (chat_models.dart:47). Not set to kMaxItemTextChars (100,000)
// because shipping 100 KB the mobile UI will never render is waste (MADR 0034 D2).
const maxToolOutputChars = 8000

// clipBlock truncates multi-line tool output for transport. Unlike clip it
// preserves line structure — a directory listing or grep result is
// unreadable once newlines are collapsed — and never cuts mid-rune.
func clipBlock(s string, max int) string {
	s = strings.TrimRight(s, " \t\n")
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n…[truncated]"
}
