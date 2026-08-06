package session

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// Leaving plan mode must never land the user in a mode that answers permissions
// on their behalf. These cases pin that for the mode lists the providers
// actually advertise (MADR 0044).
func TestDefaultModeNeverResolvesToADangerousMode(t *testing.T) {
	auto := event.SessionMode{ID: "auto", Name: "auto", Dangerous: true}

	tests := []struct {
		name  string
		modes []event.SessionMode
		want  string
		ok    bool
	}{
		{
			name:  "opencode",
			modes: []event.SessionMode{{ID: "build"}, {ID: "plan"}, auto},
			want:  "build",
			ok:    true,
		},
		{
			name:  "grok",
			modes: []event.SessionMode{{ID: "default"}, {ID: "plan"}},
			want:  "default",
			ok:    true,
		},
		{
			// The edge ordering alone does not cover: no build agent, so the
			// "first non-plan" fallback would otherwise pick auto.
			name:  "no_build_agent",
			modes: []event.SessionMode{{ID: "plan"}, auto},
			want:  "",
			ok:    false,
		},
		{
			name:  "custom_primary_agent",
			modes: []event.SessionMode{{ID: "plan"}, {ID: "reviewer"}, auto},
			want:  "reviewer",
			ok:    true,
		},
		{
			// goose ships auto as its *default* and does not flag it, so the
			// existing behaviour must be untouched.
			name: "goose_unflagged_auto_is_still_eligible",
			modes: []event.SessionMode{
				{ID: "auto"}, {ID: "approve"}, {ID: "smart_approve"}, {ID: "chat"},
			},
			want: "auto",
			ok:   true,
		},
		{
			// codex advertises default first (MADR 0047); /plan off and any
			// "return to normal" path must land there, never on dangerous auto.
			name: "codex",
			modes: []event.SessionMode{
				{ID: "default"}, {ID: "read-only"}, auto,
			},
			want: "default",
			ok:   true,
		},
		{
			// kilo's default agent is `code`, not opencode's `build` (MADR
			// 0075 §2.8); /plan off must land there, not on whatever agent
			// happens to sort first (MADR 0076 M1).
			name: "kilo",
			modes: []event.SessionMode{
				{ID: "code"}, {ID: "plan"}, auto,
			},
			want: "code",
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := defaultMode(tt.modes)
			if ok != tt.ok {
				t.Fatalf("defaultMode ok = %v, want %v", ok, tt.ok)
			}
			if got.ID != tt.want {
				t.Fatalf("defaultMode = %q, want %q", got.ID, tt.want)
			}
			if got.Dangerous {
				t.Errorf("defaultMode resolved to a dangerous mode (%q)", got.ID)
			}
		})
	}
}
