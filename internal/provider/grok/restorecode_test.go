package grok

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestSessionMetaAlwaysSendsRestoreCodeFalse is Q1 and Q2.
//
// Asserted on the meta map — the request body — and not on behaviour, because
// the behaviour it guards is grok checking a commit out into the operator's
// repository. That is not something to reproduce in order to test for it.
//
// x.ai/restore_code is a session/load flag, not a method. When a client omits
// it, grok resolves it from `[cli] restore_code` in the operator's own config
// or from xAI's remote settings (util/config/worktree.rs, resolve_restore_code)
// — neither of which mcremote controls, and one of which the vendor serves. So
// omitting it is not "leave it off"; it is "accept whatever is configured".
func TestSessionMetaAlwaysSendsRestoreCodeFalse(t *testing.T) {
	cases := []struct {
		name string
		opts provider.StartOptions
		cfg  Config
	}{
		{"bare", provider.StartOptions{}, Config{}},
		{"with a model", provider.StartOptions{Model: "grok-4.6"}, Config{}},
		{"with effort", provider.StartOptions{ThinkingLevel: "high"}, Config{}},
		{"from config", provider.StartOptions{}, Config{Model: "grok-4.5", ReasoningEffort: "low"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := grokSessionMeta(tc.opts, tc.cfg)

			got, ok := meta["x.ai/restore_code"]
			if !ok {
				t.Fatal("session meta omits x.ai/restore_code. grok then takes the value from " +
					"the operator's own grok config or from xAI's remote settings, and a " +
					"session resumed from the phone can check a commit out into their repo")
			}
			if got != false {
				t.Fatalf("x.ai/restore_code = %v, want false: resuming a session must not "+
					"modify the working tree", got)
			}
		})
	}
}

// TestSessionMetaStillCarriesModelAndEffort keeps the addition from displacing
// what the map is actually for.
func TestSessionMetaStillCarriesModelAndEffort(t *testing.T) {
	meta := grokSessionMeta(
		provider.StartOptions{Model: "grok-4.6", ThinkingLevel: "high"}, Config{})
	if meta["modelId"] != "grok-4.6" {
		t.Errorf("modelId = %v", meta["modelId"])
	}
	if meta["reasoningEffort"] != "high" {
		t.Errorf("reasoningEffort = %v", meta["reasoningEffort"])
	}
	// An unset model must stay unset rather than becoming an empty string: grok
	// reads presence, and "" is a value.
	bare := grokSessionMeta(provider.StartOptions{}, Config{})
	if _, ok := bare["modelId"]; ok {
		t.Error("an unset model was sent as an empty modelId")
	}
}
