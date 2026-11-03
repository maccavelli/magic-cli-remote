package httpagent

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// allOpsDialectSession implements every optional dialect operation. Its shell
// is permitted unless shellForbidden says the operator forbids it.
type allOpsDialectSession struct {
	fakeDialectSession
	shellForbidden bool
}

func (*allOpsDialectSession) Compact(context.Context) error                { return nil }
func (*allOpsDialectSession) Fork(context.Context, string) (string, error) { return "", nil }
func (*allOpsDialectSession) Revert(context.Context, string, string) error { return nil }
func (*allOpsDialectSession) Unrevert(context.Context) error               { return nil }
func (*allOpsDialectSession) Diff(context.Context, string) (string, error) { return "", nil }
func (*allOpsDialectSession) SetModel(context.Context, string) error       { return nil }
func (*allOpsDialectSession) SetThinkingLevel(context.Context, string) error {
	return nil
}
func (*allOpsDialectSession) ThinkingLevel() string                    { return "" }
func (*allOpsDialectSession) UndoLast(context.Context) (string, error) { return "", nil }
func (*allOpsDialectSession) Rename(context.Context, string) error     { return nil }
func (*allOpsDialectSession) Shell(context.Context, string) error      { return nil }
func (d *allOpsDialectSession) ShellAllowed() bool                     { return !d.shellForbidden }
func (*allOpsDialectSession) DisposeInstance(context.Context) error    { return nil }
func (*allOpsDialectSession) ReloadSkillCatalogs(context.Context) error {
	return nil
}
func (*allOpsDialectSession) ListWorkspace(context.Context, string) ([]provider.WorkspaceEntry, error) {
	return nil, nil
}
func (*allOpsDialectSession) ReadWorkspace(context.Context, string) (provider.WorkspaceContent, error) {
	return provider.WorkspaceContent{}, nil
}
func (*allOpsDialectSession) SearchWorkspace(context.Context, string, string) (provider.WorkspaceSearch, error) {
	return provider.WorkspaceSearch{}, nil
}

// TestCapabilitiesFollowTheDialect pins MADR 0208 D1 for the HTTP adapter: a
// session declares what its dialect's session implements, and /status and
// /usage, which every dialect answers. Each declared capability has the
// interface that performs it.
func TestCapabilitiesFollowTheDialect(t *testing.T) {
	always := []provider.Capability{provider.CapStatus, provider.CapUsage}
	for _, tc := range []struct {
		name string
		ds   DialectSession
		want []provider.Capability
	}{
		{"no optional operation", &fakeDialectSession{}, always},
		{"every optional operation", &allOpsDialectSession{}, append([]provider.Capability{
			provider.CapCompact, provider.CapFork, provider.CapRevert, provider.CapDiff,
			provider.CapSetModel, provider.CapSetThinkingLevel, provider.CapUndo, provider.CapRename,
			provider.CapWorkspace, provider.CapSkillRefresh, provider.CapShell,
		}, always...)},
		// The operator's shell policy is part of the declaration (0214 PLAN P6a).
		{"shell forbidden by the operator", &allOpsDialectSession{shellForbidden: true}, append([]provider.Capability{
			provider.CapCompact, provider.CapFork, provider.CapRevert, provider.CapDiff,
			provider.CapSetModel, provider.CapSetThinkingLevel, provider.CapUndo, provider.CapRename,
			provider.CapWorkspace, provider.CapSkillRefresh,
		}, always...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newBareSession()
			s.ds = tc.ds
			got := map[provider.Capability]bool{}
			for c, ok := range s.Capabilities() {
				if !ok {
					continue
				}
				if !provider.Implements(s, c) {
					t.Errorf("declares %q without the interface that performs it", c)
				}
				got[c] = true
			}
			if len(got) != len(tc.want) {
				t.Errorf("declared %v, want exactly %v", got, tc.want)
			}
			for _, c := range tc.want {
				if !got[c] {
					t.Errorf("declared %v, want %q among them", got, c)
				}
			}
		})
	}
}
