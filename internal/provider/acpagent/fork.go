package acpagent

import (
	"context"
	"fmt"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

var _ provider.ForkSession = (*session)(nil)

// Fork implements [provider.ForkSession] through the Spec's Ops.Fork.
// Manager.Create session/loads the returned id on a new process.
func (s *session) Fork(ctx context.Context, opts provider.ForkOptions) (provider.ForkResult, error) {
	if s.ops.Fork == nil {
		return provider.ForkResult{}, errNotOffered("fork")
	}
	if opts.DeferGoalContinuation {
		return provider.ForkResult{}, fmt.Errorf("defer goal continuation not supported")
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return provider.ForkResult{}, fmt.Errorf("session closed")
	}
	return s.ops.Fork(ctx, s, opts)
}
