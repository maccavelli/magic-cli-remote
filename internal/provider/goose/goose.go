package goose

import (
	"log/slog"
	"strconv"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acphttp"
)

// Config configures Goose's shared ACP-over-HTTP engine. WithBuiltins maps
// directly to Goose's repeatable --with-builtin flag; it deliberately does
// not expose arbitrary engine arguments.
type Config struct {
	acphttp.Config
	WithBuiltins []string
}

// McpServer is an alias for acphttp.McpServer.
type McpServer = acphttp.McpServer

var staticModes = []event.SessionMode{
	// Dangerous + not the default (MADR 0069 D3, deciding 0044's deferred
	// goose item): auto bypasses every approval AND runs unconfined — the
	// HTTP transport has no sandbox or workspace roots — so it gets the
	// same informed-consent tap as codex's bypass modes (0049) instead of
	// being the silent starting state.
	{ID: "auto", Name: "Auto", Description: "Automatically approve every tool call — no confirmation, no sandbox", Dangerous: true},
	{ID: "approve", Name: "Approve", Description: "Ask before every tool call"},
	{ID: "smart_approve", Name: "Smart Approve", Description: "Ask only for sensitive tool calls"},
	{ID: "chat", Name: "Chat", Description: "Chat only, no tool calls"},
}

func newSpec(withBuiltins []string) acphttp.Spec {
	args := func(port int) []string {
		out := []string{
			"serve", "--host", "127.0.0.1",
			"--port", strconv.Itoa(port),
			"--dangerously-unauthenticated",
		}
		for _, builtin := range withBuiltins {
			out = append(out, "--with-builtin", builtin)
		}
		return out
	}
	return acphttp.Spec{
		ID:          provider.IDGoose,
		DefaultBin:  "goose",
		ServeArgs:   args,
		HealthPath:  "/health",
		StaticModes: staticModes,
		// approve, not auto (MADR 0069 D3): a goose session must opt into
		// the dangerous mode per session, like every other provider.
		DefaultModeID: "approve",
		Commands:      commandTable,
	}
}

var spec = newSpec(nil)

// Provider is an alias for acphttp.Provider.
type Provider = acphttp.Provider

// New creates a goose provider from config.
func New(cfg Config) *Provider {
	return acphttp.New(newSpec(cfg.WithBuiltins), cfg.Config)
}

// NewWithLogger creates a goose provider with a custom logger.
func NewWithLogger(cfg Config, log *slog.Logger) *Provider {
	return acphttp.NewWithLogger(newSpec(cfg.WithBuiltins), cfg.Config, log)
}
