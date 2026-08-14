package acpagent

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestSetCredentialFillsModelFromHook(t *testing.T) {
	var gotModel string
	p := New(Spec{
		ID:          provider.IDGrok,
		DefaultBin:  "true",
		DefaultArgs: func(Config) []string { return nil },
		CredentialModel: func(_ context.Context, _ func(context.Context) (picker.Catalog, error), _ string) (string, error) {
			return "grok-4.6", nil
		},
		SetCredential: func(_ context.Context, _, _, _ string, inputs map[string]string) error {
			gotModel = inputs["model"]
			return nil
		},
	}, Config{})
	in := map[string]string{"other": "x"}
	if err := p.SetCredential(context.Background(), "xai", "xai:api", "sk-x", in); err != nil {
		t.Fatal(err)
	}
	if gotModel != "grok-4.6" {
		t.Fatalf("model = %q, want grok-4.6", gotModel)
	}
	if _, ok := in["model"]; ok {
		t.Fatal("must not mutate caller inputs")
	}
}

func TestClearCredentialPassesResolvedModel(t *testing.T) {
	var gotModel string
	p := New(Spec{
		ID:          provider.IDGrok,
		DefaultBin:  "true",
		DefaultArgs: func(Config) []string { return nil },
		CredentialModel: func(_ context.Context, _ func(context.Context) (picker.Catalog, error), cfg string) (string, error) {
			if cfg != "pinned" {
				t.Fatalf("cfgModel = %q, want pinned", cfg)
			}
			return cfg, nil
		},
		ClearCredential: func(_ context.Context, _ string, modelID string) error {
			gotModel = modelID
			return nil
		},
	}, Config{Model: "pinned"})
	if err := p.ClearCredential(context.Background(), "xai"); err != nil {
		t.Fatal(err)
	}
	if gotModel != "pinned" {
		t.Fatalf("model = %q, want pinned", gotModel)
	}
}
