package daemon

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestPrewarmPlanDefaultsEmpty(t *testing.T) {
	got := prewarmPlan(config.Defaults())
	if len(got) != 0 {
		t.Fatalf("Defaults() prewarmPlan = %v, want empty (MADR 0089 D5)", got)
	}
}

func TestPrewarmPlanHonoursExplicitTrue(t *testing.T) {
	cfg := config.Defaults()
	cfg.Providers.Kilo.Enabled = true
	cfg.Providers.Kilo.Prewarm = true
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
	cfg.Providers.Kilo.Enabled = false
	cfg.Providers.Kilo.Prewarm = true
	if prewarmWants(cfg, provider.IDKilo) {
		t.Fatal("disabled provider must not prewarm")
	}
}
