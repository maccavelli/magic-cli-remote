package command_test

import (
	"reflect"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// providerTables is every registrable provider's declared command table.
func providerTables(t *testing.T) map[provider.ID]command.Table {
	t.Helper()
	out := map[provider.ID]command.Table{}
	for _, p := range []provider.Provider{
		fake.New(),
		grok.New(grok.Config{}),
		opencode.NewHTTP(opencode.Config{}),
		kilo.NewHTTP(kilo.Config{}),
		codex.New(codex.Config{}),
	} {
		tabler, ok := p.(command.Tabler)
		if !ok {
			t.Fatalf("%s declares no command table", p.ID())
		}
		out[p.ID()] = tabler.CommandTable()
	}
	return out
}

// TestAgentCommandComesBeforeATableOp is MADR 0208 D9 over all five tables:
// where a row is an operation Magic drives, an agent that advertises the
// canonical name gets the command, unless the command is marked silent here.
func TestAgentCommandComesBeforeATableOp(t *testing.T) {
	for id, table := range providerTables(t) {
		for name, row := range table {
			if row.Kind != command.KindOp {
				continue
			}
			t.Run(string(id)+"/"+name, func(t *testing.T) {
				base := command.SessionState{Ops: knownOps}

				advertised := base
				advertised.AgentCommands = []string{name}
				r, _ := command.Resolve(name, table, advertised)
				want := command.Mapping{Kind: command.KindNative, Native: name}
				if r.Mapping != want || !r.Available {
					t.Fatalf("advertised: got %+v (available %v), want %+v", r.Mapping, r.Available, want)
				}

				silent := advertised
				silent.Silent = []string{name}
				if r, _ := command.Resolve(name, table, silent); r.Mapping != row {
					t.Fatalf("advertised but silent: got %+v, want the row %+v", r.Mapping, row)
				}

				if r, _ := command.Resolve(name, table, base); r.Mapping != row {
					t.Fatalf("not advertised: got %+v, want the row %+v", r.Mapping, row)
				}
			})
		}
	}
}

// TestDaemonCommandsNeverForward is D9's last step: help, new, sessions and
// clear act on Magic's sessions, so an agent advertising the same names never
// receives them, whatever its table says, including a provider with none.
func TestDaemonCommandsNeverForward(t *testing.T) {
	tables := providerTables(t)
	tables["no-table"] = nil
	daemon := []string{"help", "new", "sessions", "clear"}
	state := command.SessionState{Ops: knownOps, AgentCommands: daemon}
	for id, table := range tables {
		for _, name := range daemon {
			r, _ := command.Resolve(name, table, state)
			if r.Mapping.Kind != command.KindDaemon || !r.Available {
				t.Errorf("%s /%s: got %+v (available %v), want the daemon's", id, name, r.Mapping, r.Available)
			}
		}
	}
}

// TestNonCanonicalAgentCommandsChangeNothing is MADR 0208 N8's consequence:
// opencode, kilo and codex advertise no canonical name, so what they do
// advertise leaves every canonical command resolved as before.
func TestNonCanonicalAgentCommandsChangeNothing(t *testing.T) {
	for id, table := range providerTables(t) {
		bare := command.SessionState{Ops: knownOps}
		withOwn := bare
		withOwn.AgentCommands = []string{"init", "a-project-skill"}
		if got, want := command.ResolveAll(table, withOwn), command.ResolveAll(table, bare); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: non-canonical agent commands changed resolution:\n got %+v\nwant %+v", id, got, want)
		}
	}
}

// TestGrokCompactIsGroksOwn pins MADR 0208's P4b deviation 1: grok's /compact
// compacts over ACP, so the command forwards to grok while grok advertises it.
func TestGrokCompactIsGroksOwn(t *testing.T) {
	table := providerTables(t)[provider.IDGrok]
	r, _ := command.Resolve("compact", table, command.SessionState{AgentCommands: []string{"compact"}})
	want := command.Mapping{Kind: command.KindNative, Native: "compact"}
	if r.Mapping != want || !r.Available {
		t.Fatalf("grok /compact: got %+v (available %v), want %+v", r.Mapping, r.Available, want)
	}
}
