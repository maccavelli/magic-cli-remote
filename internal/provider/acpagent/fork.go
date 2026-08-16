package acpagent

import (
	"context"
	"fmt"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// forkRequest is the pinned grok 1.0.4 _x.ai/session/fork body
// (MADR 0092 P1.1). newCwd == sourceCwd == the daemon session cwd.
func forkRequest(agentID, cwd string) map[string]any {
	return map[string]any{
		"sourceSessionId": agentID,
		"sourceCwd":       cwd,
		"newCwd":          cwd,
	}
}

var _ provider.ForkSession = (*session)(nil)

// Fork implements [provider.ForkSession] via _x.ai/session/fork.
// LastTurnID is ignored: grok has no turn-boundary field. Manager.Create
// session/loads the returned id on a new process.
func (s *session) Fork(ctx context.Context, opts provider.ForkOptions) (provider.ForkResult, error) {
	if opts.DeferGoalContinuation {
		return provider.ForkResult{}, fmt.Errorf("defer goal continuation not supported")
	}
	s.mu.Lock()
	agentID := s.agentID
	cwd := s.cwd
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return provider.ForkResult{}, fmt.Errorf("session closed")
	}
	if agentID == "" || cwd == "" {
		return provider.ForkResult{}, fmt.Errorf("fork: missing session id or cwd")
	}
	var resp struct {
		NewSessionID    string `json:"newSessionId"`
		ParentSessionID string `json:"parentSessionId"`
	}
	if err := s.rawRequest(ctx, "_x.ai/session/fork", forkRequest(agentID, cwd), &resp); err != nil {
		return provider.ForkResult{}, err
	}
	if resp.NewSessionID == "" {
		return provider.ForkResult{}, fmt.Errorf("fork: empty newSessionId")
	}
	parent := resp.ParentSessionID
	if parent == "" {
		parent = agentID
	}
	return provider.ForkResult{AgentSessionID: resp.NewSessionID, ForkedFromID: parent}, nil
}
