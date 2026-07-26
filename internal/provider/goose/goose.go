package goose

import (
	"log/slog"
	"strconv"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acphttp"
)

// Config is an alias for acphttp.Config.
type Config = acphttp.Config

// McpServer is an alias for acphttp.McpServer.
type McpServer = acphttp.McpServer

var staticModels = []picker.Option{
	{ID: "poolside/laguna-s-2.1:free", Label: "Laguna S 2.1", Group: "poolside"},
	{ID: "x-ai/grok-code-fast-1", Label: "Grok Code Fast 1", Group: "xai"},
	{ID: "anthropic/claude-sonnet-4.5", Label: "Claude Sonnet 4.5", Group: "anthropic"},
	{ID: "google/gemini-2.5-pro", Label: "Gemini 2.5 Pro", Group: "google"},
	{ID: "google/gemini-2.5-flash", Label: "Gemini 2.5 Flash", Group: "google"},
	{ID: "deepseek/deepseek-r1-0528", Label: "DeepSeek R1", Group: "deepseek"},
}

var staticModes = []event.SessionMode{
	{ID: "auto", Name: "Auto", Description: "Automatically approve tool calls"},
	{ID: "approve", Name: "Approve", Description: "Ask before every tool call"},
	{ID: "smart_approve", Name: "Smart Approve", Description: "Ask only for sensitive tool calls"},
	{ID: "chat", Name: "Chat", Description: "Chat only, no tool calls"},
}

var spec = acphttp.Spec{
	ID:         provider.IDGoose,
	DefaultBin: "goose",
	ServeArgs: func(port int) []string {
		return []string{
			"serve", "--host", "127.0.0.1",
			"--port", strconv.Itoa(port),
			"--dangerously-unauthenticated",
		}
	},
	HealthPath:    "/health",
	StaticModels:  staticModels,
	StaticModes:   staticModes,
	DefaultModeID: "auto",
	Commands:      commandTable,
}

// Provider is an alias for acphttp.Provider.
type Provider = acphttp.Provider

// New creates a goose provider from config.
func New(cfg Config) *Provider {
	return acphttp.New(spec, cfg)
}

// NewWithLogger creates a goose provider with a custom logger.
func NewWithLogger(cfg Config, log *slog.Logger) *Provider {
	return acphttp.NewWithLogger(spec, cfg, log)
}
