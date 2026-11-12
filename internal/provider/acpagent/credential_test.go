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
		ID:          provider.ID("vendor"),
		DefaultBin:  "true",
		DefaultArgs: func(Config) []string { return nil },
		CredentialModel: func(_ context.Context, _ func(context.Context) (picker.Catalog, error), _ string) (string, error) {
			return "model-a", nil
		},
		SetCredential: func(_ context.Context, _, _, _ string, inputs map[string]string) error {
			gotModel = inputs["model"]
			return nil
		},
	}, Config{})
	in := map[string]string{"other": "x"}
	if err := p.SetCredential(context.Background(), "vendor", "vendor:api", "sk-x", in); err != nil {
		t.Fatal(err)
	}
	if gotModel != "model-a" {
		t.Fatalf("model = %q, want model-a", gotModel)
	}
	if _, ok := in["model"]; ok {
		t.Fatal("must not mutate caller inputs")
	}
}

func TestClearCredentialPassesResolvedModel(t *testing.T) {
	var gotModel string
	p := New(Spec{
		ID:          provider.ID("vendor"),
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
	if err := p.ClearCredential(context.Background(), "vendor"); err != nil {
		t.Fatal(err)
	}
	if gotModel != "pinned" {
		t.Fatalf("model = %q, want pinned", gotModel)
	}
}

// TestStartDeviceAuthPassesTheMethodAndInputs pins 0214 PLAN P6b Deviation 1:
// the Spec sees the phone's method id and inputs, as a copy.
func TestStartDeviceAuthPassesTheMethodAndInputs(t *testing.T) {
	var gotBin, gotUpstream, gotMethod string
	var gotInputs map[string]string
	p := New(Spec{
		ID:          provider.ID("vendor"),
		DefaultBin:  "vendor-bin",
		DefaultArgs: func(Config) []string { return nil },
		StartDeviceAuth: func(_ context.Context, bin, upstreamID, methodID string, inputs map[string]string) (provider.DeviceFlow, func(context.Context) error, error) {
			gotBin, gotUpstream, gotMethod, gotInputs = bin, upstreamID, methodID, inputs
			inputs["added"] = "by the spec"
			return provider.DeviceFlow{UserCode: "CODE"}, nil, nil
		},
	}, Config{})
	in := map[string]string{"deploymentType": "enterprise"}
	if _, _, err := p.StartDeviceAuth(context.Background(), "up", "up:oauth:device", in); err != nil {
		t.Fatal(err)
	}
	if gotBin != "vendor-bin" || gotUpstream != "up" || gotMethod != "up:oauth:device" ||
		gotInputs["deploymentType"] != "enterprise" {
		t.Errorf("the Spec got bin %q, upstream %q, method %q, inputs %v",
			gotBin, gotUpstream, gotMethod, gotInputs)
	}
	if _, ok := in["added"]; ok {
		t.Error("the Spec's change reached the caller's inputs")
	}
}
