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
	{ID: "auto", Name: "Auto", Description: "Automatically approve tool calls"},
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
		ID:            provider.IDGoose,
		DefaultBin:    "goose",
		ServeArgs:     args,
		HealthPath:    "/health",
		StaticModes:   staticModes,
		DefaultModeID: "auto",
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
