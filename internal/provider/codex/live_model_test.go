//go:build live_codex

package codex_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
)

// TestLiveCodexListModelsWithoutThread pins the contract MADR 0043 D5 depends
// on: codex answers model/list with no thread open, so the create-session model
// picker works before any session exists.
//
// This is the regression guard for the failure it replaces. `model/list`
// previously went out with no `params` and was decoded from a `models` field
// that codex does not send; both errors fell into the same empty static
// catalog as "no engine", so an empty picker was indistinguishable from a
// working one. If a codex upgrade renames either, this test fails loudly
// instead of the picker quietly going blank.
//
// Run: go test -tags live_codex ./internal/provider/codex/ -run TestLiveCodexListModels -count=1 -v
func TestLiveCodexListModelsWithoutThread(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex not in PATH")
	}
	p := codex.New(codex.Config{})
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cat, err := p.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	t.Logf("source=%s options=%d defaults=%v", cat.Source, len(cat.Options), cat.DefaultIDs)
	for _, o := range cat.Options {
		t.Logf("  %s | %s | %v", o.ID, o.Label, o.Meta)
	}
	if cat.Source == picker.SourceStatic {
		t.Fatal("catalog is the static fallback: model/list failed (see Warn logs) — " +
			"check the params shape and the `data` field name against the installed codex")
	}
	if len(cat.Options) == 0 {
		t.Fatal("live catalog is empty")
	}
	if len(cat.DefaultIDs) == 0 {
		t.Error("no default model: isDefault was not honoured")
	}
	for _, o := range cat.Options {
		if o.Label == "" {
			t.Errorf("model %q has no display label", o.ID)
		}
	}
}
