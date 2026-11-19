package kilo

import (
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// defaultModelID is the offline seed: Kilo Gateway's free auto-router,
// available without any account state. The engine's own default is
// auth-state-dependent (kilo-auto/free unauthenticated, kilo-auto/balanced
// with a Gateway OAuth session — MADR 0075 §2.6), so this seed is only the
// static fallback; P3's AfterBoot resolve replaces it with the engine's
// answer (plan PD4).
const defaultModelID = "kilo-auto/free"

// staticModelOptions is the offline catalog used when the engine is not up.
// Ids are providerID/modelID with the model id verbatim after the first
// slash (plan PD3): Kilo model ids contain slashes and `~vendor` aliases, so
// they must come from a catalog, never string concatenation (MADR 0075 Q11).
func staticModelOptions() []picker.Option {
	return []picker.Option{
		{ID: "kilo/" + defaultModelID, Label: "Kilo Auto (free)", Group: providerKilo},
		{ID: "openrouter/openrouter/free", Label: "OpenRouter free", Group: "openrouter"},
	}
}

// StaticModels implements [httpagent.ModelLister].
func (d *httpDialect) StaticModels(cfg httpagent.Config) picker.Catalog {
	def := cfg.Model
	if def == "" {
		def = "kilo/" + defaultModelID
	}
	return picker.SingleCatalog(picker.SourceStatic, staticModelOptions(), def, true)
}

// StaticAgents implements [httpagent.AgentLister]: the live-probed primary
// agents of kilo 7.4.20 (MADR 0075 §2.8). Default is `code` — Kilo has no
// `build`. Subagents (explore, general) and hidden internals (compaction,
// summary, title) are not selectable work and stay out.
func (d *httpDialect) StaticAgents(cfg httpagent.Config) picker.Catalog {
	_ = cfg
	opts := []picker.Option{
		{ID: modeCode, Label: modeCode, Description: "Default full agent", Group: modeKindPrimary},
		{ID: modeAsk, Label: modeAsk, Description: "Q&A, no edits", Group: modeKindPrimary},
		{ID: modeDebug, Label: modeDebug, Description: "Diagnostics", Group: modeKindPrimary},
		{ID: modePlan, Label: modePlan, Description: "Plan files only", Group: modeKindPrimary},
		{ID: modeOrchestrator, Label: modeOrchestrator, Description: "Multi-agent coordination", Group: modeKindPrimary},
	}
	return picker.SingleCatalog(picker.SourceStatic, opts, modeCode, false)
}
