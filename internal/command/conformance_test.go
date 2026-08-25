// Package command_test holds the cross-provider conformance test: every
// provider the daemon can register must say, for every canonical command, how
// it satisfies it — or why it cannot. It lives in an external test package so
// it can import the providers that import this one.
package command_test

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/provider/goose"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// knownOps is every capability a KindOp mapping may name. A table naming
// anything else would resolve to "unavailable" forever, silently.
var knownOps = map[command.Op]bool{
	command.OpCompact:          true,
	command.OpContext:          true,
	command.OpStatus:           true,
	command.OpUsage:            true,
	command.OpSetModel:         true,
	command.OpSetThinkingLevel: true,
	command.OpDiff:             true,
	command.OpUndo:             true,
	command.OpRedo:             true,
	command.OpGoal:             true,
	command.OpServiceTier:      true,
	command.OpPersonality:      true,
	command.OpReview:           true,
	command.OpFork:             true,
}

// TestProvidersDeclareEveryCanonicalCommand is the contract for adding a CLI:
// declare each canonical command's mechanism, with a reason for the ones the
// agent cannot do. Falling through to a default is allowed by the resolver but
// not by this test — a default is a guess, and guesses about a specific CLI have
// been wrong every time we checked (MADR 0023).
func TestProvidersDeclareEveryCanonicalCommand(t *testing.T) {
	providers := []provider.Provider{
		fake.New(),
		grok.New(grok.Config{}),
		goose.New(goose.Config{}),
		opencode.NewHTTP(opencode.Config{}),
		kilo.NewHTTP(kilo.Config{}),
		codex.New(codex.Config{}),
	}
	for _, p := range providers {
		t.Run(string(p.ID()), func(t *testing.T) {
			tabler, ok := p.(command.Tabler)
			if !ok {
				t.Fatalf("%s must implement command.Tabler", p.ID())
			}
			tbl := tabler.CommandTable()
			for _, spec := range command.Specs {
				m, declared := tbl[spec.Name]
				if !declared {
					t.Errorf("%s declares no mapping for /%s — say how it works "+
						"or KindNone with a reason", p.ID(), spec.Name)
					continue
				}
				switch m.Kind {
				case command.KindDaemon:
				case command.KindMode:
				case command.KindCollaborationMode:
				case command.KindOp:
					if !knownOps[m.Op] {
						t.Errorf("/%s on %s names unknown op %q", spec.Name, p.ID(), m.Op)
					}
				case command.KindNative:
					if m.Native == "" {
						t.Errorf("/%s on %s is native but names no agent command",
							spec.Name, p.ID())
					}
				case command.KindNone:
					if m.Note == "" {
						t.Errorf("/%s on %s is unavailable without a reason — the note "+
							"is what the user is told", spec.Name, p.ID())
					}
				default:
					t.Errorf("/%s on %s has unknown kind %q", spec.Name, p.ID(), m.Kind)
				}
			}
			for name := range tbl {
				if _, ok := command.Lookup(name); !ok {
					t.Errorf("%s declares %q, which is not a canonical command", p.ID(), name)
				}
			}
		})
	}
}

// A table keyed by an alias would never be found: Resolve looks entries up by
// canonical name.
func TestTablesAreKeyedByCanonicalName(t *testing.T) {
	providers := []command.Tabler{
		fake.New(),
		grok.New(grok.Config{}),
		goose.New(goose.Config{}),
		opencode.NewHTTP(opencode.Config{}),
		kilo.NewHTTP(kilo.Config{}),
		codex.New(codex.Config{}),
	}
	for _, p := range providers {
		for name := range p.CommandTable() {
			spec, ok := command.Lookup(name)
			if ok && spec.Name != name {
				t.Errorf("%q is an alias of /%s; key the table by the canonical name",
					name, spec.Name)
			}
		}
	}
}

// TestEveryKindDaemonMappingIsDispatched guards MADR 0035 D6: any provider
// that declares a command as KindDaemon must have a handler in the
// daemon's dispatch table. This is the test that would have caught
// `/compact`, `/context`, and `/model` being advertised but dead-ending
// against codex. Without this guard, an empty switch arm in
// runCanonical silently turns into "not wired up in this build".
func TestEveryKindDaemonMappingIsDispatched(t *testing.T) {
	daemon := map[string]bool{}
	for _, n := range session.DaemonCommandNames() {
		daemon[n] = true
	}
	providers := []provider.Provider{
		fake.New(),
		grok.New(grok.Config{}),
		goose.New(goose.Config{}),
		opencode.NewHTTP(opencode.Config{}),
		kilo.NewHTTP(kilo.Config{}),
		codex.New(codex.Config{}),
	}
	for _, p := range providers {
		tbl := p.(command.Tabler).CommandTable()
		for name, m := range tbl {
			if m.Kind != command.KindDaemon {
				continue
			}
			if !daemon[name] {
				t.Errorf("%s declares /%s as KindDaemon but the daemon has no handler for it",
					p.ID(), name)
			}
		}
	}
}

// TestKindOpNamesKnownOp would be a more comprehensive check
// (the live session must implement the Op-named interface), but the
// existing TestProvidersDeclareEveryCanonicalCommand catches the easy case
// of unknown op names. The full capability check is per-session and runs
// in the manager when Resolve consults session state. The dispatch-side
// guarantee here is just that any KindOp names a known op.
func Test0080ScopeLeavesFollowOnsUnimplemented(t *testing.T) {
	// MADR 0080 D13–D14: follow-on and host-global commands must not have
	// landed as working Codex operations in this decision.
	tbl := codex.New(codex.Config{}).CommandTable()
	for _, name := range []string{"rename", "skills"} {
		if _, ok := tbl[name]; ok {
			t.Errorf("/%s is a ranked follow-on and must not be in the Codex table yet", name)
		}
	}
	for _, name := range []string{"logout", "theme", "plugins", "hooks"} {
		if _, ok := tbl[name]; ok {
			t.Errorf("/%s is host-global/TUI-only and must not be a provider command", name)
		}
	}
}

func TestCodexRequiredCommandsAreNotNative(t *testing.T) {
	// MADR 0080 D13/D21: required Codex commands must never fall through to
	// literal native slash forwarding.
	required := []string{"plan", "mode", "permissions", "diff", "goal", "fast", "personality", "review", "fork"}
	tbl := codex.New(codex.Config{}).CommandTable()
	for _, name := range required {
		m, ok := tbl[name]
		if !ok {
			t.Errorf("codex missing /%s", name)
			continue
		}
		if m.Kind == command.KindNative {
			t.Errorf("/%s must not be KindNative", name)
		}
		if m.Kind == command.KindNone && m.Note == command.ReasonIntegrationNotWired {
			t.Errorf("/%s still unwired", name)
		}
	}
}

func TestKindOpNamesKnownOp(t *testing.T) {
	providers := []provider.Provider{
		fake.New(),
		grok.New(grok.Config{}),
		goose.New(goose.Config{}),
		opencode.NewHTTP(opencode.Config{}),
		kilo.NewHTTP(kilo.Config{}),
		codex.New(codex.Config{}),
	}
	for _, p := range providers {
		tbl := p.(command.Tabler).CommandTable()
		for name, m := range tbl {
			if m.Kind != command.KindOp {
				continue
			}
			if !knownOps[m.Op] {
				t.Errorf("%s declares /%s as KindOp(%s) but the op is not in the dispatch table",
					p.ID(), name, m.Op)
			}
		}
	}
}
