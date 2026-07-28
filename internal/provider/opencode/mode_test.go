package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// The engine reports its internal compaction/summary/title agents as primary
// with hidden:true, and subagents cannot run a user turn — neither belongs in
// the mode list.
const agentCatalogBody = `[
	{"name":"build","description":"Default","mode":"primary","native":true},
	{"name":"compaction","mode":"primary","native":true,"hidden":true},
	{"name":"explore","description":"Explore","mode":"subagent","native":true},
	{"name":"plan","description":"Plan mode. Disallows all edit tools.","mode":"primary","native":true},
	{"name":"summary","mode":"primary","native":true,"hidden":true},
	{"name":"title","mode":"primary","native":true,"hidden":true}
]`

func agentCatalogAPI(t *testing.T) func(context.Context, string, string, any, any) error {
	t.Helper()
	return func(_ context.Context, method, path string, _ any, out any) error {
		if method != "GET" || !strings.HasPrefix(path, "/agent") {
			t.Fatalf("unexpected %s %s", method, path)
		}
		return json.Unmarshal([]byte(agentCatalogBody), out)
	}
}

func TestModesFromAgentsKeepsRunnablePrimaries(t *testing.T) {
	var agents []agentInfo
	if err := json.Unmarshal([]byte(agentCatalogBody), &agents); err != nil {
		t.Fatal(err)
	}
	modes := modesFromAgents(agents)
	want := []string{"build", "plan"}
	if len(modes) != len(want) {
		t.Fatalf("modes = %+v, want %v", modes, want)
	}
	for i, id := range want {
		if modes[i].ID != id {
			t.Fatalf("mode %d = %q, want %q", i, modes[i].ID, id)
		}
	}
	if modes[1].Description == "" {
		t.Error("plan mode should carry the engine's description")
	}
}

func TestAdvertiseModesEmitsSessionMode(t *testing.T) {
	h := &captureHost{api: agentCatalogAPI(t)}
	d := &httpDialect{}
	s := d.NewSession(h).(*httpSession)

	s.advertiseModes(context.Background())

	var got *event.Event
	for i := range h.events {
		if h.events[i].Type == event.TypeMode {
			got = &h.events[i]
		}
	}
	if got == nil {
		t.Fatalf("no mode event; got %+v", h.events)
	}
	// The two catalog agents plus the synthetic auto mode (MADR 0044).
	if len(got.Modes) != 3 {
		t.Fatalf("modes = %+v", got.Modes)
	}
	// No agent was requested at create, so the engine default is in effect —
	// and auto-approve is off, so the current mode is the agent, not "auto".
	if got.CurrentModeID != "build" {
		t.Fatalf("current = %q, want build", got.CurrentModeID)
	}
}

// With the engine unreachable the switcher and /plan must still work.
func TestAdvertiseModesFallsBackToStatic(t *testing.T) {
	h := &captureHost{
		api: func(context.Context, string, string, any, any) error {
			return fmt.Errorf("engine down")
		},
	}
	d := &httpDialect{}
	s := d.NewSession(h).(*httpSession)

	s.advertiseModes(context.Background())

	if len(h.events) != 1 || h.events[0].Type != event.TypeMode {
		t.Fatalf("events = %+v", h.events)
	}
	ids := make([]string, 0, 3)
	for _, m := range h.events[0].Modes {
		ids = append(ids, m.ID)
	}
	// auto last: session.defaultMode resolves "return to normal" as build, else
	// the first non-plan mode, so ordering keeps /plan off out of auto.
	if strings.Join(ids, ",") != "build,plan,auto" {
		t.Fatalf("static modes = %v", ids)
	}
}

// A mode switch is only real if the next turn actually runs as that agent.
func TestSetModeRedirectsNextPrompt(t *testing.T) {
	var promptBody map[string]any
	h := &captureHost{
		model: "opencode/test-model",
		api: func(ctx context.Context, method, path string, body, out any) error {
			if method == "GET" && strings.HasPrefix(path, "/agent") {
				return agentCatalogAPI(t)(ctx, method, path, body, out)
			}
			if method != "POST" || !strings.Contains(path, "prompt_async") {
				t.Fatalf("unexpected %s %s", method, path)
			}
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			return json.Unmarshal(b, &promptBody)
		},
	}
	d := &httpDialect{}
	s := d.NewSession(h).(*httpSession)

	current, err := s.SetMode(context.Background(), "plan")
	if err != nil {
		t.Fatal(err)
	}
	if current != "plan" {
		t.Fatalf("current = %q, want plan", current)
	}
	if err := s.Prompt(context.Background(),
		[]provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if promptBody["agent"] != "plan" {
		t.Fatalf("prompt agent = %v, want plan; body=%v", promptBody["agent"], promptBody)
	}
}

func TestSetModeRejectsUnknownMode(t *testing.T) {
	h := &captureHost{api: agentCatalogAPI(t)}
	d := &httpDialect{}
	s := d.NewSession(h).(*httpSession)

	if _, err := s.SetMode(context.Background(), "explore"); err == nil {
		t.Error("a subagent is not a switchable mode")
	}
	if _, err := s.SetMode(context.Background(), "nope"); err == nil {
		t.Error("unknown mode accepted")
	}
	if got := h.Agent(); got != "" {
		t.Fatalf("agent = %q, want unchanged after a rejected switch", got)
	}
}

// The daemon lower-cases command arguments; the engine's ids are the source of
// truth for casing.
func TestSetModeMatchesIDCaseInsensitively(t *testing.T) {
	h := &captureHost{api: agentCatalogAPI(t)}
	d := &httpDialect{}
	s := d.NewSession(h).(*httpSession)

	current, err := s.SetMode(context.Background(), "PLAN")
	if err != nil {
		t.Fatal(err)
	}
	if current != "plan" || h.Agent() != "plan" {
		t.Fatalf("current=%q agent=%q, want plan", current, h.Agent())
	}
}

// Hidden internals and delegated subagents must not reach the top-level picker.
func TestListAgentsLiveDropsNonStartableAgents(t *testing.T) {
	d := &httpDialect{}
	cat, err := d.ListAgentsLive(context.Background(), agentCatalogAPI(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range cat.Options {
		switch o.ID {
		case "compaction", "summary", "title", "explore":
			t.Fatalf("non-startable agent %q offered in the picker", o.ID)
		}
	}
	if len(cat.Options) != 2 {
		t.Fatalf("opts = %d, want build/plan", len(cat.Options))
	}
}
