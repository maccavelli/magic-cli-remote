package session_test

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// TestForkInheritsTheNameSource is PLAN 0187 T2: a fork's derived name keeps
// its parent's source, so the fork of a user-named session stays the user's,
// and the fork of an unnamed or agent-titled one stays open to agent titles
// (MADR 0187 D2).
func TestForkInheritsTheNameSource(t *testing.T) {
	p := &forkProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := session.NewManager(reg, nil, nil, nil)

	for _, tc := range []struct {
		parentName, wantName, wantSource string
	}{
		{"work", "work (fork)", session.NameSourceUser},
		{"", "fork", session.NameSourceGenerated},
	} {
		parent, err := mgr.Create(context.Background(), "forker",
			provider.StartOptions{Name: tc.parentName}, "dev-a")
		if err != nil {
			t.Fatal(err)
		}
		child, err := mgr.Fork(context.Background(), parent.ID, "", "dev-a")
		if err != nil {
			t.Fatalf("fork of %q: %v", tc.parentName, err)
		}
		if child.Name != tc.wantName || child.NameSource != tc.wantSource {
			t.Errorf("fork of %q: %q/%q, want %q/%q",
				tc.parentName, child.Name, child.NameSource, tc.wantName, tc.wantSource)
		}
	}
}
