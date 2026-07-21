package opencode

import (
	"log/slog"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestNewDefaults(t *testing.T) {
	p := New(Config{})
	if p.ID() != provider.IDOpencode {
		t.Fatalf("id = %q", p.ID())
	}
}

func TestDefaultArgs(t *testing.T) {
	got := spec.DefaultArgs(Config{})
	if len(got) != 1 || got[0] != "acp" {
		t.Fatalf("args = %v, want [acp]", got)
	}
}

// No model requested → no config-option call, no error.
func TestConfigureSessionNoModelIsNoop(t *testing.T) {
	err := configureSession(t.Context(), nil, acp.NewSessionResponse{},
		provider.StartOptions{}, Config{}, slog.Default())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

// Requested model already current → no config-option call, no error.
func TestConfigureSessionModelAlreadyCurrent(t *testing.T) {
	resp := acp.NewSessionResponse{
		SessionId: "s1",
		ConfigOptions: []acp.SessionConfigOption{
			{Select: &acp.SessionConfigOptionSelect{
				Id:           "model",
				Name:         "Model",
				CurrentValue: "opencode/big-pickle",
			}},
		},
	}
	err := configureSession(t.Context(), nil, resp,
		provider.StartOptions{Model: "opencode/big-pickle"}, Config{}, slog.Default())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

// Model requested but the agent advertises no "model" option → warn-and-continue.
func TestConfigureSessionMissingModelOptionIsSoft(t *testing.T) {
	err := configureSession(t.Context(), nil, acp.NewSessionResponse{SessionId: "s1"},
		provider.StartOptions{Model: "anthropic/claude-sonnet-4-5"}, Config{}, slog.Default())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

// Config-level default model is used when the per-session override is empty.
// (CurrentValue matching keeps this connection-free.)
func TestConfigureSessionUsesConfigModel(t *testing.T) {
	resp := acp.NewSessionResponse{
		SessionId: "s1",
		ConfigOptions: []acp.SessionConfigOption{
			{Select: &acp.SessionConfigOptionSelect{
				Id:           "model",
				CurrentValue: "anthropic/claude-sonnet-4-5",
			}},
		},
	}
	err := configureSession(t.Context(), nil, resp,
		provider.StartOptions{}, Config{Model: "anthropic/claude-sonnet-4-5"}, slog.Default())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}
