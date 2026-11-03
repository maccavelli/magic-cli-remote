package fake_test

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
)

// TestCapabilitiesAreWhatTheFakeImplements pins MADR 0208 D1 for the fake: it
// declares the operations it implements, and each has the interface that
// performs it.
func TestCapabilitiesAreWhatTheFakeImplements(t *testing.T) {
	s, err := fake.New().Start(context.Background(), provider.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close(context.Background()) }()
	want := []provider.Capability{provider.CapCompact, provider.CapSetModel,
		provider.CapSetThinkingLevel, provider.CapUndo, provider.CapRevert, provider.CapDiff,
		provider.CapWorkspace, provider.CapShell, provider.CapSkillRefresh}
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
	if len(got) != len(want) {
		t.Errorf("declared %v, want exactly %v", got, want)
	}
	for _, c := range want {
		if !got[c] {
			t.Errorf("declared %v, want %q among them", got, c)
		}
	}
}
