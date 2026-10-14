package grok

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// TestGrokAdvertisesOnlyWhatMagicImplements holds grok's Spec to MADR 0184
// D4's capability-honesty invariant: initialize advertises to grok nothing
// that acpagent lacks the behavior for. grok keeps fs and terminal (0184,
// amendment, owner answer Q3).
func TestGrokAdvertisesOnlyWhatMagicImplements(t *testing.T) {
	caps := New(Config{}).ClientCapabilities()
	if problems := acpagent.CheckClientCapabilities(caps); len(problems) != 0 {
		t.Fatalf("grok's advertisement is dishonest: %v", problems)
	}
	if !caps.Fs.ReadTextFile || !caps.Fs.WriteTextFile || !caps.Terminal {
		t.Fatalf("grok must keep fs and terminal, got %+v", caps)
	}
}
