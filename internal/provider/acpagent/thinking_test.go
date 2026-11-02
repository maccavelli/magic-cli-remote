package acpagent

import (
	"context"
	"errors"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestSetThinkingLevelRequiresIdentity(t *testing.T) {
	s := &session{}
	err := s.SetThinkingLevel(context.Background(), "low")
	if err == nil {
		t.Fatal("empty session: want error")
	}
	if errors.Is(err, provider.ErrThinkingLevelFixed) {
		t.Fatalf("err = %v, must not be ErrThinkingLevelFixed", err)
	}
	if s.ThinkingLevel() != "" {
		t.Errorf("failed SetThinkingLevel must not mutate state, got %q", s.ThinkingLevel())
	}

	s.closed = true
	s.agentID = "sid"
	s.currentModelID = "model-a"
	err = s.SetThinkingLevel(context.Background(), "low")
	if err == nil || err.Error() != "session closed" {
		t.Fatalf("closed session err = %v, want session closed", err)
	}
}

func TestSpawnArgsIgnoresPerSessionModelAndThinking(t *testing.T) {
	baked := []string{"--reasoning-effort", "medium", "agent", "--no-leader", "stdio"}
	p := &Provider{
		cfg: Config{
			Args:            baked,
			ReasoningEffort: "medium",
		},
	}
	got := p.spawnArgs(provider.StartOptions{})
	if !equalStr(got, baked) {
		t.Errorf("no override: got %v, want %v", got, baked)
	}
	got = p.spawnArgs(provider.StartOptions{ThinkingLevel: "low", Model: "model-b"})
	if !equalStr(got, baked) {
		t.Errorf("per-session model/thinking must not rebuild argv: got %v, want %v", got, baked)
	}
}

func equalStr(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
