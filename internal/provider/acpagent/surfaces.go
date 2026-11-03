package acpagent

import (
	"context"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// The operations a Spec serves outside ACP (Ops, 0214 PLAN P6a). Each answers
// "not offered" when the Spec does not set it, and the session declares it only
// when set (Capabilities), so the manager never calls one that is not there.

var (
	_ provider.WorkspaceSession    = (*session)(nil)
	_ provider.ShellSession        = (*session)(nil)
	_ provider.RevertSession       = (*session)(nil)
	_ provider.DiffSession         = (*session)(nil)
	_ provider.SkillRefreshSession = (*session)(nil)
)

// ListWorkspace implements [provider.WorkspaceSession] through Ops.
func (s *session) ListWorkspace(ctx context.Context, path string) ([]provider.WorkspaceEntry, error) {
	if !s.ops.workspaceOffered() {
		return nil, errNotOffered("workspace")
	}
	return s.ops.ListWorkspace(ctx, s, path)
}

// ReadWorkspace implements [provider.WorkspaceSession] through Ops.
func (s *session) ReadWorkspace(ctx context.Context, path string) (provider.WorkspaceContent, error) {
	if !s.ops.workspaceOffered() {
		return provider.WorkspaceContent{}, errNotOffered("workspace")
	}
	return s.ops.ReadWorkspace(ctx, s, path)
}

// SearchWorkspace implements [provider.WorkspaceSession] through Ops.
func (s *session) SearchWorkspace(ctx context.Context, kind, query string) (provider.WorkspaceSearch, error) {
	if !s.ops.workspaceOffered() {
		return provider.WorkspaceSearch{}, errNotOffered("workspace")
	}
	return s.ops.SearchWorkspace(ctx, s, kind, query)
}

// Shell implements [provider.ShellSession] through Ops.Shell.
func (s *session) Shell(ctx context.Context, command string) error {
	if s.ops.Shell == nil {
		return errNotOffered("shell")
	}
	return s.ops.Shell(ctx, s, command)
}

// Revert implements [provider.RevertSession] through Ops.Revert.
func (s *session) Revert(ctx context.Context, messageID, partID string) error {
	if !s.ops.revertOffered() {
		return errNotOffered("revert")
	}
	return s.ops.Revert(ctx, s, messageID, partID)
}

// Unrevert implements [provider.RevertSession] through Ops.Unrevert.
func (s *session) Unrevert(ctx context.Context) error {
	if !s.ops.revertOffered() {
		return errNotOffered("revert")
	}
	return s.ops.Unrevert(ctx, s)
}

// Diff implements [provider.DiffSession] through Ops.Diff.
func (s *session) Diff(ctx context.Context, messageID string) (provider.DiffResult, error) {
	if s.ops.Diff == nil {
		return provider.DiffResult{}, errNotOffered("diff")
	}
	return s.ops.Diff(ctx, s, messageID)
}

// RefreshSkills implements [provider.SkillRefreshSession] through
// Ops.RefreshSkills.
func (s *session) RefreshSkills(ctx context.Context) error {
	if s.ops.RefreshSkills == nil {
		return errNotOffered("skill refresh")
	}
	return s.ops.RefreshSkills(ctx, s)
}
