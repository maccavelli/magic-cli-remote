package acpagent

import (
	"context"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

var _ provider.RuntimeSession = (*session)(nil)

// RuntimeUsage implements [provider.RuntimeSession] through the Spec's
// Ops.RuntimeUsage.
func (s *session) RuntimeUsage(ctx context.Context) (provider.Result, error) {
	if s.ops.RuntimeUsage == nil {
		return provider.Result{Text: "Usage is unavailable: this agent offers no usage report."}, nil
	}
	return s.ops.RuntimeUsage(ctx, s)
}

// RuntimeStatus implements [provider.RuntimeSession] through the Spec's
// Ops.RuntimeStatus.
func (s *session) RuntimeStatus(ctx context.Context) (provider.Result, error) {
	if s.ops.RuntimeStatus == nil {
		return provider.Result{Text: "Status is unavailable: this agent offers no status report."}, nil
	}
	return s.ops.RuntimeStatus(ctx, s)
}
