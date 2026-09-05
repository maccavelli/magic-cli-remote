package relay

import (
	"os"
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
