package acpagent

import (
	"encoding/json"
	"slices"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// groupedOptions is a session/new answer's config options in the shape a
// categorized agent sends (a `recent` group, then one per model provider; 0179
// PLAN P1 step 3): a grouped `model` select and an ungrouped `thought_level`
// select, each with its category.
const groupedOptions = `[
	{"type":"select","id":"model","name":"Model","category":"model","currentValue":"anthropic/claude-x",
	 "options":[
		{"group":"recent","name":"Recent","options":[{"value":"anthropic/claude-x","name":"Claude X"}]},
		{"group":"anthropic","name":"Anthropic","options":[
			{"value":"anthropic/claude-x","name":"Claude X"},{"value":"anthropic/claude-y","name":"Claude Y"}]},
		{"group":"openai","name":"","options":[{"value":"openai/gpt-z","name":"GPT Z"}]}]},
	{"type":"select","id":"effort","name":"Effort","category":"thought_level","currentValue":"high",
	 "options":[{"value":"low","name":"Low"},{"value":"high","name":"High"}]},
	{"type":"boolean","id":"web","name":"Web","category":"_tools","currentValue":true}
]`

// TestGroupedConfigOptionReachesThePhone is 0208-PLAN P7 step 1: a grouped
// select reaches the phone with every value and its group, and every option
// with its category. Before P7 the grouped model option arrived with no values
// at all, and no option carried a category.
func TestGroupedConfigOptionReachesThePhone(t *testing.T) {
	var opts []acp.SessionConfigOption
	if err := json.Unmarshal([]byte(groupedOptions), &opts); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	s := newBareSession(4)
	s.emitConfigOptions(opts)
	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeSessionConfig || len(ev.ConfigOptions) != 3 {
		t.Fatalf("event = %+v, want session_config with 3 options", ev)
	}

	model := ev.ConfigOptions[0]
	want := []event.ConfigOptionValue{
		{ID: "anthropic/claude-x", Name: "Claude X", Group: "Recent"},
		{ID: "anthropic/claude-x", Name: "Claude X", Group: "Anthropic"},
		{ID: "anthropic/claude-y", Name: "Claude Y", Group: "Anthropic"},
		// A group the agent named nothing is shown by its id.
		{ID: "openai/gpt-z", Name: "GPT Z", Group: "openai"},
	}
	if !slices.Equal(model.Values, want) {
		t.Errorf("model values = %+v\nwant %+v", model.Values, want)
	}
	if model.Category != "model" || model.CurrentValue != "anthropic/claude-x" {
		t.Errorf("model = category %q, current %q; want model, anthropic/claude-x", model.Category, model.CurrentValue)
	}

	effort := ev.ConfigOptions[1]
	if effort.Category != "thought_level" || len(effort.Values) != 2 || effort.Values[0].Group != "" {
		t.Errorf("effort = %+v, want category thought_level and two ungrouped values", effort)
	}
	if web := ev.ConfigOptions[2]; web.Category != "_tools" || !web.BoolValue {
		t.Errorf("web = %+v, want category _tools and true", web)
	}
}

// TestDangerousModeIDsMarkTheAgentsModes is P7 step 3: a Spec's dangerous mode
// ids are advertised dangerous, so the phone confirms before arming one, on
// the agent's list and on the Spec's static one, without writing through to
// the static list every session shares.
func TestDangerousModeIDsMarkTheAgentsModes(t *testing.T) {
	static := []event.SessionMode{{ID: "default", Name: "Default"}, {ID: "Bypass", Name: "Bypass"}}
	s := newBareSession(4)
	s.provider = &Provider{spec: Spec{DangerousModeIDs: []string{"bypass"}}}
	s.staticModes = static

	s.emitModes(&acp.SessionModeState{
		CurrentModeId:  "default",
		AvailableModes: []acp.SessionMode{{Id: "default", Name: "Default"}, {Id: "bypass", Name: "Bypass"}},
	})
	agent := recvEvent(t, s.events)
	if len(agent.Modes) != 2 || agent.Modes[0].Dangerous || !agent.Modes[1].Dangerous {
		t.Errorf("agent's modes = %+v, want bypass alone dangerous", agent.Modes)
	}

	s.emitModesOrStatic(nil)
	fallback := recvEvent(t, s.events)
	if len(fallback.Modes) != 2 || fallback.Modes[0].Dangerous || !fallback.Modes[1].Dangerous {
		t.Errorf("static modes = %+v, want Bypass alone dangerous, matched without case", fallback.Modes)
	}
	if static[1].Dangerous {
		t.Error("marking wrote through to the Spec's shared static list")
	}
}

// A Spec with no dangerous ids advertises the agent's modes as they came.
func TestNoDangerousModeIDsLeavesModesAlone(t *testing.T) {
	s := newBareSession(4)
	s.provider = &Provider{spec: Spec{}}
	s.emitModes(&acp.SessionModeState{
		CurrentModeId:  "default",
		AvailableModes: []acp.SessionMode{{Id: "default", Name: "Default"}, {Id: "bypass", Name: "Bypass"}},
	})
	for _, m := range recvEvent(t, s.events).Modes {
		if m.Dangerous {
			t.Errorf("mode %q advertised dangerous with no DangerousModeIDs", m.ID)
		}
	}
}
