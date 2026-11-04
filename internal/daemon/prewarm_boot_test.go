package daemon

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
)

func TestPrewarmPlanDefaultsEmpty(t *testing.T) {
	got := prewarmPlan(config.Defaults())
	if len(got) != 0 {
		t.Fatalf("Defaults() prewarmPlan = %v, want empty (MADR 0089 D5)", got)
	}
}

func TestPrewarmPlanHonoursExplicitTrue(t *testing.T) {
	cfg := config.Defaults()
	sec := &kilo.Section{}
	sec.Enabled = true
	sec.Prewarm = true
	cfg.Providers.Sections = config.ProviderSections{"kilo": sec}
	got := prewarmPlan(cfg)
	if len(got) != 1 || got[0] != provider.IDKilo {
		t.Fatalf("prewarmPlan = %v, want [kilo]", got)
	}
	if !prewarmWants(cfg, provider.IDKilo) {
		t.Fatal("prewarmWants(kilo) = false")
	}
	if prewarmWants(cfg, provider.IDOpencode) {
		t.Fatal("prewarmWants(opencode) = true, want false")
	}
}

func TestPrewarmPlanSkipsDisabled(t *testing.T) {
	cfg := config.Defaults()
	sec := &kilo.Section{}
	sec.Prewarm = true
	cfg.Providers.Sections = config.ProviderSections{"kilo": sec}
	if prewarmWants(cfg, provider.IDKilo) {
		t.Fatal("disabled provider must not prewarm")
	}
}

// TestPrewarmPlanReadsADescribedAgentsSection is 0208 PLAN P8: a described
// agent prewarms from its own section, and only while it is enabled.
func TestPrewarmPlanReadsADescribedAgentsSection(t *testing.T) {
	cfg := config.Defaults()
	sec := &grok.Section{}
	sec.Prewarm = true
	cfg.Providers.Sections = config.ProviderSections{"grok": sec}
	if prewarmWants(cfg, provider.IDGrok) {
		t.Fatal("a disabled grok must not prewarm")
	}
	sec.Enabled = true
	if !prewarmWants(cfg, provider.IDGrok) {
		t.Fatalf("prewarmPlan = %v, want grok", prewarmPlan(cfg))
	}
}
