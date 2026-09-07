package httpagent

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// defaultingDialect knows an engine default, the way kilo's and opencode's
// dialects do once they have talked to their gateway.
type defaultingDialect struct {
	fakeDialect
	mp, mid string
}

func (d *defaultingDialect) DefaultModel() (string, string) { return d.mp, d.mid }

// TestCurrentModelReadsTheProviderDialectNotTheSessionView is the regression
// for a wiring defect the session-level unit tests could not see, found on a
// live host (MADR 0137 Phase 2).
//
// The first implementation asked `s.ds` — the per-session DialectSession —
// which for kilo is `kilo.httpSession` and implements no such method. It
// compiled, every manager test passed because they use their own fake, and a
// live `hi` produced a record with no model at all. The engine default belongs
// to the provider-wide dialect, and this pins that.
func TestCurrentModelReadsTheProviderDialectNotTheSessionView(t *testing.T) {
	p := NewWithLogger(&defaultingDialect{
		fakeDialect: fakeDialect{id: "test"},
		mp:          "kilo",
		mid:         "kilo-auto/balanced",
	}, Config{Bin: "false"}, nil)
	s := &session{p: p}

	var _ provider.ModelReporter = s

	if got := s.CurrentModel(); got != "kilo/kilo-auto/balanced" {
		t.Fatalf("CurrentModel() = %q, want kilo/kilo-auto/balanced", got)
	}
}

// TestCurrentModelPrefersTheSessionsOwnModel proves a mid-session switch wins
// over the engine default: the recorded model is a fact about this session,
// the default is only what it would have been.
func TestCurrentModelPrefersTheSessionsOwnModel(t *testing.T) {
	p := NewWithLogger(&defaultingDialect{
		fakeDialect: fakeDialect{id: "test"},
		mp:          "kilo",
		mid:         "kilo-auto/balanced",
	}, Config{Bin: "false"}, nil)
	s := &session{p: p, model: "anthropic/claude-opus-5"}

	if got := s.CurrentModel(); got != "anthropic/claude-opus-5" {
		t.Fatalf("CurrentModel() = %q, want the session's own model", got)
	}
}

// TestCurrentModelIsEmptyWhenNothingKnowsOne covers grok, goose and codex, and
// a dialect that has not resolved its default yet. Empty is the honest answer;
// a caller must not be handed a guess it cannot distinguish from a fact.
func TestCurrentModelIsEmptyWhenNothingKnowsOne(t *testing.T) {
	t.Run("dialect implements no default", func(t *testing.T) {
		p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
		s := &session{p: p}
		if got := s.CurrentModel(); got != "" {
			t.Fatalf("CurrentModel() = %q, want empty", got)
		}
	})
	t.Run("dialect has not resolved one yet", func(t *testing.T) {
		p := NewWithLogger(&defaultingDialect{fakeDialect: fakeDialect{id: "test"}},
			Config{Bin: "false"}, nil)
		s := &session{p: p}
		if got := s.CurrentModel(); got != "" {
			t.Fatalf("CurrentModel() = %q, want empty before a default is known", got)
		}
	})
}
