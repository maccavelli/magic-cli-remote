package opencode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

func TestStaticAgentsCatalog(t *testing.T) {
	d := &httpDialect{}
	cat := d.StaticAgents(httpagent.Config{})
	if len(cat.Options) < 2 {
		t.Fatalf("expected static agents, got %+v", cat.Options)
	}
	if len(cat.DefaultIDs) == 0 || cat.DefaultIDs[0] != "build" {
		t.Fatalf("default=%v want build", cat.DefaultIDs)
	}
}

func TestListAgentsLiveParsesAndGroups(t *testing.T) {
	d := &httpDialect{}
	body := `[
		{"name":"explore","description":"Explore","mode":"subagent","native":true},
		{"name":"build","description":"Default","mode":"primary","native":true},
		{"name":"plan","description":"Plan","mode":"primary","native":true}
	]`
	api := func(_ context.Context, method, path string, _ any, out any) error {
		if method != "GET" || path != "/agent" {
			t.Fatalf("unexpected %s %s", method, path)
		}
		return json.Unmarshal([]byte(body), out)
	}
	cat, err := d.ListAgentsLive(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Options) != 3 {
		t.Fatalf("opts=%d", len(cat.Options))
	}
	// primary before subagent
	if cat.Options[0].Group != "primary" {
		t.Fatalf("first group=%s want primary", cat.Options[0].Group)
	}
	if cat.Options[len(cat.Options)-1].Group != "subagent" {
		t.Fatalf("last group=%s want subagent", cat.Options[len(cat.Options)-1].Group)
	}
	if len(cat.DefaultIDs) == 0 || cat.DefaultIDs[0] != "build" {
		t.Fatalf("default=%v", cat.DefaultIDs)
	}
}

func TestPromptAsyncIncludesAgent(t *testing.T) {
	var gotBody map[string]any
	h := &agentHost{
		captureHost: captureHost{
			model: "opencode/test-model",
			api: func(_ context.Context, method, path string, body, _ any) error {
				if method != "POST" || !strings.Contains(path, "prompt_async") {
					t.Fatalf("unexpected %s %s", method, path)
				}
				b, err := json.Marshal(body)
				if err != nil {
					return err
				}
				return json.Unmarshal(b, &gotBody)
			},
		},
		agent: "plan",
	}
	d := &httpDialect{}
	s := d.NewSession(h).(*httpSession)
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if gotBody["agent"] != "plan" {
		t.Fatalf("body agent=%v want plan; full=%v", gotBody["agent"], gotBody)
	}
}

func TestPromptAsyncOmitsEmptyAgent(t *testing.T) {
	var gotBody map[string]any
	h := &captureHost{
		model: "opencode/test-model",
		api: func(_ context.Context, _ string, _ string, body, _ any) error {
			b, _ := json.Marshal(body)
			return json.Unmarshal(b, &gotBody)
		},
	}
	d := &httpDialect{}
	s := d.NewSession(h).(*httpSession)
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := gotBody["agent"]; ok {
		t.Fatalf("empty agent must be omitted, got %v", gotBody["agent"])
	}
}

// agentHost wraps captureHost with a non-empty Agent().
type agentHost struct {
	captureHost
	agent string
}

func (h *agentHost) Agent() string { return h.agent }
