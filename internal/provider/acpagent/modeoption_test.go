package acpagent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// modeOptionJSON is an agent's config options with a mode-category select and
// no ACP modes, the shape of 0214 PLAN P4 Deviation 2.
func modeOptionJSON(mode string) string {
	return `[{"type":"select","id":"mode","name":"Mode","category":"mode","currentValue":"` + mode + `",` +
		`"options":[{"value":"build","name":"Build"},{"value":"plan","name":"Plan"}]}]`
}

// modeAgent starts a session on an agent whose mode is a config option and
// whose session/set_mode changes the mode and confirms nothing. It records the
// methods the client calls after session/new.
func modeAgent(t *testing.T) (*session, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	mode := "build"
	s, err := startOnScript(t, New(Spec{
		ID:                 "vendor",
		StaticModes:        []event.SessionMode{{ID: "build", Name: "Build"}, {ID: "plan", Name: "Plan"}},
		DefaultModeID:      "build",
		SynthesizeAutoMode: true,
	}, Config{}), provider.StartOptions{}, func(r scriptRequest, _ func(string)) {
		switch r.Method {
		case "initialize":
			r.Answer(`{"protocolVersion":1,"agentCapabilities":{}}`)
			return
		case "session/new":
			r.Answer(`{"sessionId":"agent-1","configOptions":` + modeOptionJSON(mode) + `}`)
			return
		}
		var p struct {
			ConfigID string `json:"configId"`
			Value    string `json:"value"`
			ModeID   string `json:"modeId"`
		}
		_ = json.Unmarshal(r.Params, &p)
		mu.Lock()
		calls = append(calls, r.Method+" "+p.ConfigID+p.ModeID+"="+p.Value)
		mu.Unlock()
		switch r.Method {
		case "session/set_config_option":
			mode = p.Value
			r.Answer(`{"configOptions":` + modeOptionJSON(mode) + `}`)
		case "session/set_mode":
			mode = p.ModeID
			r.Answer(`{}`)
		}
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	startEvents(s)
	return s.(*session), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), calls...)
	}
}

// reportedModes returns the current mode ids s reported, in order.
func reportedModes(s provider.Session) []string {
	var out []string
	for _, ev := range startEvents(s) {
		if ev.Type == event.TypeMode && ev.CurrentModeID != "" {
			out = append(out, ev.CurrentModeID)
		}
	}
	return out
}

// TestModeIsSetThroughTheModeOption pins 0214 PLAN P4 Deviation 2: an agent
// that publishes its mode as a mode-category config option is switched through
// session/set_config_option, and the answer is what Magic reports, where
// session/set_mode would have confirmed nothing.
func TestModeIsSetThroughTheModeOption(t *testing.T) {
	s, calls := modeAgent(t)
	if err := s.SetMode(context.Background(), "plan"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if got := strings.Join(calls(), ","); got != "session/set_config_option mode=plan" {
		t.Errorf("the client called %q, want the mode option set to plan", got)
	}
	if got := reportedModes(s); len(got) == 0 || got[len(got)-1] != "plan" {
		t.Errorf("reported modes %q, want plan last", got)
	}
}

// TestArmingAutoUsesTheModeOption: auto puts the agent in its normal mode
// through the same option.
func TestArmingAutoUsesTheModeOption(t *testing.T) {
	s, calls := modeAgent(t)
	if err := s.SetMode(context.Background(), "plan"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMode(context.Background(), "auto"); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 2 || got[1] != "session/set_config_option mode=build" {
		t.Errorf("the client called %q, want the mode option set to build to arm auto", got)
	}
	if modes := reportedModes(s); len(modes) == 0 || modes[len(modes)-1] != "auto" {
		t.Errorf("reported modes %q, want auto last", modes)
	}
}

// TestModeOptionUpdateReportsTheMode: the agent moving its mode option on its
// own is a mode change.
func TestModeOptionUpdateReportsTheMode(t *testing.T) {
	s := loadingSession()
	s.loading = false
	replay(t, s, `{"sessionUpdate":"config_option_update","configOptions":`+modeOptionJSON("build")+`}`)
	replay(t, s, `{"sessionUpdate":"config_option_update","configOptions":`+modeOptionJSON("plan")+`}`)
	var modes []string
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeMode {
			modes = append(modes, ev.CurrentModeID)
		}
	}
	if strings.Join(modes, ",") != "build,plan" {
		t.Errorf("reported modes %q, want build then plan", modes)
	}
}
