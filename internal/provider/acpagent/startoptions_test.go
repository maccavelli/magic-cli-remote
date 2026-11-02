package acpagent

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// startEvents collects what s emitted during its start.
func startEvents(s provider.Session) []event.Event {
	var out []event.Event
	for {
		select {
		case ev := <-s.Events():
			out = append(out, ev)
		case <-time.After(100 * time.Millisecond):
			return out
		}
	}
}

// TestStartAppliesTheRequestedModelAndLevel pins 0214 PLAN P4 Deviation 1: an
// agent whose model and thinking level are standard config options is sent the
// session's requested values after session/new, when they differ from its
// current ones, and Magic records and shows what the agent answers. A refusal
// keeps the agent's value, says so, and the session still starts.
func TestStartAppliesTheRequestedModelAndLevel(t *testing.T) {
	for _, c := range []struct {
		name         string
		model, level string
		refuse       string // a configId the agent refuses to set
		wantSets     []string
		wantModel    string
		wantLevel    string
		wantNotice   string
	}{
		{name: "both differ", model: "m2", level: "high",
			wantSets: []string{"model=m2", "effort=high"}, wantModel: "m2", wantLevel: "high"},
		{name: "already current", model: "m1", level: "low", wantModel: "m1", wantLevel: "low"},
		{name: "nothing requested", wantModel: "m1", wantLevel: "low"},
		{name: "the model refused", model: "m2", level: "low", refuse: "model",
			wantSets: []string{"model=m2"}, wantModel: "m1", wantLevel: "low", wantNotice: "m2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var sets []string
			model, level := "m1", "low"
			s, err := startOnScript(t, New(Spec{ID: "vendor"}, Config{}),
				provider.StartOptions{Model: c.model, ThinkingLevel: c.level},
				func(r scriptRequest, _ func(string)) {
					switch r.Method {
					case "initialize":
						r.Answer(`{"protocolVersion":1,"agentCapabilities":{}}`)
					case "session/new":
						r.Answer(`{"sessionId":"agent-1","configOptions":` + configOptionsJSON(model, level) + `}`)
					case "session/set_config_option":
						var p struct {
							ConfigID string `json:"configId"`
							Value    string `json:"value"`
						}
						_ = json.Unmarshal(r.Params, &p)
						mu.Lock()
						sets = append(sets, p.ConfigID+"="+p.Value)
						mu.Unlock()
						if p.ConfigID == c.refuse {
							r.Fail(-32602, "unknown model")
							return
						}
						if p.ConfigID == "model" {
							model = p.Value
						} else {
							level = p.Value
						}
						r.Answer(`{"configOptions":` + configOptionsJSON(model, level) + `}`)
					}
				})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			evs := startEvents(s)
			mu.Lock()
			got := strings.Join(sets, ",")
			mu.Unlock()
			if got != strings.Join(c.wantSets, ",") {
				t.Errorf("set_config_option calls = %q, want %q", got, strings.Join(c.wantSets, ","))
			}
			ss := s.(*session)
			if ss.CurrentModel() != c.wantModel || ss.ThinkingLevel() != c.wantLevel {
				t.Errorf("session model %q level %q, want %q %q", ss.CurrentModel(), ss.ThinkingLevel(), c.wantModel, c.wantLevel)
			}
			var shown string
			var notice string
			for _, ev := range evs {
				switch ev.Type {
				case event.TypeSessionConfig:
					for _, o := range ev.ConfigOptions {
						if o.ID == "model" {
							shown = o.CurrentValue
						}
					}
				case event.TypeNotice:
					notice += ev.Text
				}
			}
			if shown != c.wantModel {
				t.Errorf("the start's session_config shows model %q, want %q", shown, c.wantModel)
			}
			if c.wantNotice == "" && notice != "" {
				t.Errorf("unexpected notice %q", notice)
			}
			if c.wantNotice != "" && !strings.Contains(notice, c.wantNotice) {
				t.Errorf("notice %q does not name %q", notice, c.wantNotice)
			}
		})
	}
}
