package command

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func modes(ids ...string) []event.SessionMode {
	out := make([]event.SessionMode, 0, len(ids))
	for _, id := range ids {
		out = append(out, event.SessionMode{ID: id, Name: id})
	}
	return out
}

func TestLookupNameAliasAndSlash(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{in: "compact", want: "compact", ok: true},
		{in: "/compact", want: "compact", ok: true},
		{in: " PLAN ", want: "plan", ok: true},
		{in: "reset", want: "clear", ok: true}, // alias
		{in: "", ok: false},
		{in: "/", ok: false},
		{in: "nonesuch", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			spec, ok := Lookup(tt.in)
			if ok != tt.ok {
				t.Fatalf("Lookup(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			}
			if ok && spec.Name != tt.want {
				t.Fatalf("Lookup(%q) = %q, want %q", tt.in, spec.Name, tt.want)
			}
		})
	}
}

func TestResolveUnknownCommandIsNotCanonical(t *testing.T) {
	if _, ok := Resolve("nonesuch", Table{}, SessionState{}); ok {
		t.Fatal("an agent's own command must not resolve as canonical")
	}
}

func TestResolveDeepResearchAndWorkflow(t *testing.T) {
	stAdvertised := SessionState{AgentCommands: []string{"deep-research", "workflow"}}
	rDeep, ok := Resolve("deep-research", Table{}, stAdvertised)
	if !ok || !rDeep.Available {
		t.Fatalf("deep-research must be available when advertised: ok=%v, avail=%v", ok, rDeep.Available)
	}
	rWork, ok := Resolve("workflow", Table{}, stAdvertised)
	if !ok || !rWork.Available {
		t.Fatalf("workflow must be available when advertised: ok=%v, avail=%v", ok, rWork.Available)
	}

	stNotAdvertised := SessionState{AgentCommands: []string{}}
	rDeepNot, _ := Resolve("deep-research", Table{}, stNotAdvertised)
	if rDeepNot.Available {
		t.Fatal("deep-research must be unavailable when agent doesn't advertise it")
	}
	rWorkNot, _ := Resolve("workflow", Table{}, stNotAdvertised)
	if rWorkNot.Available {
		t.Fatal("workflow must be unavailable when agent doesn't advertise it")
	}
}

// The rule the framework exists for: a provider that says a command cannot work
// wins over the agent advertising it. Grok advertises /compact over ACP and
// executes nothing.
func TestExplicitNoneBeatsAdvertisement(t *testing.T) {
	tbl := Table{"compact": {Kind: KindNone, Note: "grok compacts only in its own terminal UI"}}
	st := SessionState{AgentCommands: []string{"compact"}, Ops: map[Op]bool{OpCompact: true}}

	r, ok := Resolve("compact", tbl, st)
	if !ok {
		t.Fatal("compact must be canonical")
	}
	if r.Available {
		t.Fatal("an explicit KindNone must stay unavailable")
	}
	if !strings.Contains(r.Reason(), "terminal UI") {
		t.Fatalf("reason = %q, want the provider's note", r.Reason())
	}
}

func TestResolvePicksDeclaredMechanism(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		table    Table
		state    SessionState
		wantKind Kind
		wantOn   string // Native or ModeID or Op, whichever the kind uses
	}{
		{
			name:     "native under another name",
			cmd:      "context",
			table:    Table{"context": {Kind: KindNative, Native: "session-info"}},
			state:    SessionState{AgentCommands: []string{"session-info"}},
			wantKind: KindNative,
			wantOn:   "session-info",
		},
		{
			name:     "op when the session has the capability",
			cmd:      "compact",
			table:    Table{"compact": {Kind: KindOp, Op: OpCompact}},
			state:    SessionState{Ops: map[Op]bool{OpCompact: true}},
			wantKind: KindOp,
			wantOn:   string(OpCompact),
		},
		{
			name:     "mode when the mode exists",
			cmd:      "plan",
			table:    Table{"plan": {Kind: KindMode, ModeID: "plan"}},
			state:    SessionState{Modes: modes("default", "plan")},
			wantKind: KindMode,
			wantOn:   "plan",
		},
		{
			name:     "daemon needs nothing",
			cmd:      "sessions",
			table:    Table{},
			state:    SessionState{},
			wantKind: KindDaemon,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, ok := Resolve(tt.cmd, tt.table, tt.state)
			if !ok || !r.Available {
				t.Fatalf("Resolve(%q) ok=%v available=%v, want both true", tt.cmd, ok, r.Available)
			}
			if r.Mapping.Kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", r.Mapping.Kind, tt.wantKind)
			}
			got := r.Mapping.Native + r.Mapping.ModeID + string(r.Mapping.Op)
			if tt.wantOn != "" && got != tt.wantOn {
				t.Fatalf("mechanism target = %q, want %q", got, tt.wantOn)
			}
		})
	}
}

// A declared mechanism that is not usable right now (the mode is gone, the
// capability is missing, the agent stopped advertising the command) must fall
// back to the spec default rather than reporting a mechanism that would fail.
func TestUnusableMappingFallsBackToDefault(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		table    Table
		state    SessionState
		wantKind Kind
		wantOK   bool
	}{
		{
			name:     "in-place model switch missing falls back to daemon relaunch",
			cmd:      "model",
			table:    Table{"model": {Kind: KindOp, Op: OpSetModel}},
			state:    SessionState{},
			wantKind: KindDaemon,
			wantOK:   true,
		},
		{
			name:     "native command no longer advertised",
			cmd:      "goal",
			table:    Table{"goal": {Kind: KindNative, Native: "goal"}},
			state:    SessionState{AgentCommands: []string{"review"}},
			wantKind: KindNone,
		},
		{
			name:     "op default without the capability",
			cmd:      "diff",
			table:    Table{},
			state:    SessionState{},
			wantKind: KindNone,
		},
		{
			name:     "plan without a plan mode",
			cmd:      "plan",
			table:    Table{},
			state:    SessionState{Modes: modes("auto", "chat")},
			wantKind: KindNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, ok := Resolve(tt.cmd, tt.table, tt.state)
			if !ok {
				t.Fatalf("Resolve(%q) must be canonical", tt.cmd)
			}
			if r.Available != tt.wantOK {
				t.Fatalf("available = %v, want %v", r.Available, tt.wantOK)
			}
			if r.Mapping.Kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", r.Mapping.Kind, tt.wantKind)
			}
			if !r.Available && r.Reason() == "" {
				t.Fatal("an unavailable command must explain itself")
			}
		})
	}
}

// /plan and /mode differ: /mode only needs modes to exist, /plan needs a mode
// called plan.
func TestModeCommandNeedsOnlyAnyMode(t *testing.T) {
	st := SessionState{Modes: modes("auto", "chat")}
	r, _ := Resolve("mode", Table{}, st)
	if !r.Available {
		t.Fatal("/mode must be available whenever the session has modes")
	}
	r, _ = Resolve("mode", Table{}, SessionState{})
	if r.Available {
		t.Fatal("/mode must be unavailable without modes")
	}
	if !strings.Contains(r.Reason(), "modes") {
		t.Fatalf("reason = %q, want it to mention modes", r.Reason())
	}
}

func TestResolveAllCoversVocabularyInOrder(t *testing.T) {
	got := ResolveAll(Table{}, SessionState{})
	if len(got) != len(Specs) {
		t.Fatalf("ResolveAll returned %d, want %d", len(got), len(Specs))
	}
	for i, r := range got {
		if r.Spec.Name != Specs[i].Name {
			t.Fatalf("position %d = %q, want %q", i, r.Spec.Name, Specs[i].Name)
		}
	}
}

func TestUsageRendersHint(t *testing.T) {
	plan, _ := Resolve("plan", Table{}, SessionState{Modes: modes("plan")})
	if got := plan.Usage(); got != "/plan [off]" {
		t.Fatalf("usage = %q", got)
	}
	help, _ := Resolve("help", Table{}, SessionState{})
	if got := help.Usage(); got != "/help" {
		t.Fatalf("usage = %q", got)
	}
}

// Names and aliases must be unique across the vocabulary, or Lookup silently
// resolves one spelling to two commands.
func TestVocabularySpellingsAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, s := range Specs {
		for _, name := range append([]string{s.Name}, s.Aliases...) {
			if prev, dup := seen[name]; dup {
				t.Fatalf("%q is claimed by both %s and %s", name, prev, s.Name)
			}
			seen[name] = s.Name
			if name != strings.ToLower(name) {
				t.Fatalf("%q must be lower-case to be found by Lookup", name)
			}
		}
		if s.Description == "" {
			t.Fatalf("%s needs a description: clients show it", s.Name)
		}
	}
}
