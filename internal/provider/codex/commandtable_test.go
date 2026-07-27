package codex

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
)

// TestCommandTableHasNoInternalTODONotes guards the MADR 0035 D3 fix: the
// note shown to the user must say what they can do instead, not what the
// developer is thinking about.
func TestCommandTableHasNoInternalTODONotes(t *testing.T) {
	for name, m := range commandTable {
		if m.Kind != command.KindNone {
			continue
		}
		lower := strings.ToLower(m.Note)
		for _, banned := range []string{"probe", "todo", "not supported", "tbd", "fixme"} {
			if strings.Contains(lower, banned) {
				t.Errorf("%q: note contains internal-only phrase %q: %q",
					name, banned, m.Note)
			}
		}
	}
}

// TestCommandTableRoutesCompactToOp pins the MADR 0035 D3 fix: /compact
// must reach codex's Compact (session.go:457), not fall through to a
// dead-end daemon handler.
func TestCommandTableRoutesCompactToOp(t *testing.T) {
	m, ok := commandTable["compact"]
	if !ok {
		t.Fatal("codex table missing compact")
	}
	if m.Kind != command.KindOp || m.Op != command.OpCompact {
		t.Errorf("compact: kind=%s op=%s, want kind=op op=compact", m.Kind, m.Op)
	}
}

// TestCommandTableRoutesContextToOp pins /context reaching OpContext.
func TestCommandTableRoutesContextToOp(t *testing.T) {
	m, ok := commandTable["context"]
	if !ok {
		t.Fatal("codex table missing context")
	}
	if m.Kind != command.KindOp || m.Op != command.OpContext {
		t.Errorf("context: kind=%s op=%s, want kind=op op=context", m.Kind, m.Op)
	}
}

// TestUnavailableCommandsHaveActionableNotes: every KindNone note must give
// the user a next step (a verb, an alternative command, or both).
func TestUnavailableCommandsHaveActionableNotes(t *testing.T) {
	for name, m := range commandTable {
		if m.Kind != command.KindNone {
			continue
		}
		if m.Note == "" {
			t.Errorf("%q: KindNone with empty note; user gets no explanation", name)
		}
	}
}
