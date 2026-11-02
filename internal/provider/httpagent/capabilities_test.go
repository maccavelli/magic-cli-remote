package httpagent

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// allOpsDialectSession implements every optional dialect operation.
type allOpsDialectSession struct{ fakeDialectSession }

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
