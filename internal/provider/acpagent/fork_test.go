package acpagent

import (
	"context"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// forkable gives s a Spec fork operation that succeeds, so a failure is the
// session's own check.
func forkable(s *session) *session {
	s.ops.Fork = func(context.Context, Host, provider.ForkOptions) (provider.ForkResult, error) {
		return provider.ForkResult{AgentSessionID: "forked"}, nil
	}
	return s
}

func TestForkRejectsDeferGoalContinuation(t *testing.T) {
	s := forkable(&session{agentID: "s", cwd: "/tmp"})
	_, err := s.Fork(context.Background(), provider.ForkOptions{DeferGoalContinuation: true})
	if err == nil || !strings.Contains(err.Error(), "defer goal continuation") {
		t.Fatalf("err=%v", err)
	}
}

func TestForkClosedSession(t *testing.T) {
	s := forkable(&session{agentID: "s", cwd: "/tmp", closed: true})
	_, err := s.Fork(context.Background(), provider.ForkOptions{})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("err=%v", err)
	}
}
