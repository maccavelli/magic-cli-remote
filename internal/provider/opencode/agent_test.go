package opencode

import (
	"context"
	"encoding/json"
	"errors"
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
	if len(cat.Options) != 2 {
		t.Fatalf("opts=%d", len(cat.Options))
	}
	// Startable primary agents only: subagents cannot receive top-level prompts.
	if cat.Options[0].Group != "primary" {
		t.Fatalf("first group=%s want primary", cat.Options[0].Group)
	}
	for _, option := range cat.Options {
		if option.ID == "explore" {
			t.Fatalf("subagent leaked into top-level picker: %+v", cat.Options)
		}
	}
	if cat.AllowCustom {
		t.Fatal("agent catalog must reject arbitrary agent text")
	}
	if len(cat.DefaultIDs) == 0 || cat.DefaultIDs[0] != "build" {
		t.Fatalf("default=%v", cat.DefaultIDs)
	}
}

func TestValidateStartAgent(t *testing.T) {
	body := `[
		{"name":"build","mode":"primary"},
		{"name":"all-purpose","mode":"all"},
		{"name":"explore","mode":"subagent"},
		{"name":"title","mode":"primary","hidden":true}
	]`
	var gotPath string
	api := func(_ context.Context, method, path string, _ any, out any) error {
		gotPath = path
		if method != "GET" {
			t.Fatalf("method=%s", method)
		}
		return json.Unmarshal([]byte(body), out)
	}
	d := &httpDialect{}
	got, err := d.ValidateStartAgent(context.Background(), api, "/repo with space", "BUILD")
	if err != nil {
		t.Fatal(err)
	}
	if got != "build" {
		t.Fatalf("canonical=%q want build", got)
	}
	if gotPath != "/agent?directory=%2Frepo+with+space" {
		t.Fatalf("path=%q", gotPath)
	}
	for _, agent := range []string{"explore", "title", "missing"} {
		_, err := d.ValidateStartAgent(context.Background(), api, "/repo", agent)
		if !errors.Is(err, provider.ErrInvalidAgent) {
			t.Fatalf("agent %q error=%v, want ErrInvalidAgent", agent, err)
		}
	}
}

func TestPromptAsyncIncludesAgent(t *testing.T) {
	var gotBody map[string]any
	h := &captureHost{
		model: "opencode/test-model",
		agent: "plan",
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
