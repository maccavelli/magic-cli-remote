package relay

import (
	"os"
	"runtime/debug"
	"testing"
)

func TestMemoryLimitPlanDefault(t *testing.T) {
	if _, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		t.Skip("GOMEMLIMIT already set in environment")
	}
	lim, src := memoryLimitPlan()
	if lim != defaultRelayMemoryLimit || src != "default" {
		t.Fatalf("got %d %q, want %d default", lim, src, defaultRelayMemoryLimit)
	}
}

func TestMemoryLimitPlanEnv(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "256MiB")
	_, src := memoryLimitPlan()
	if src != "GOMEMLIMIT" {
		t.Fatalf("src=%q, want GOMEMLIMIT", src)
	}
}

func TestApplyMemoryLimitDefault(t *testing.T) {
	if _, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		t.Skip("GOMEMLIMIT already set in environment")
	}
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	lim, src := applyMemoryLimit()
	if src != "default" {
		t.Fatalf("src=%q, want default", src)
	}
	if lim != defaultRelayMemoryLimit {
		t.Fatalf("lim=%d, want %d", lim, defaultRelayMemoryLimit)
	}
}
