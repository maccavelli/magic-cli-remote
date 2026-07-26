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
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// knownOps is every capability a KindOp mapping may name. A table naming
// anything else would resolve to "unavailable" forever, silently.
var knownOps = map[command.Op]bool{
	command.OpCompact:  true,
	command.OpContext:  true,
	command.OpSetModel: true,
	command.OpDiff:     true,
	command.OpUndo:     true,
	command.OpRedo:     true,
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
